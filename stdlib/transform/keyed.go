package transform

import (
	"context"
	"fmt"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"
)

// keyer computes a string key from a message using the domain-matched selector:
// `by = "<jq>"` for continuous/linear data or `path = [..]` for discrete/object
// data. Exactly one must be set. An unresolved key yields ("", false).
type keyer struct {
	by   string
	path data.Path
}

func newKeyer(by string, path []string, who string) (keyer, error) {
	if (by == "") == (len(path) == 0) {
		return keyer{}, fmt.Errorf("%s: set exactly one of `by` or `path`", who)
	}
	return keyer{by: by, path: data.Path(path)}, nil
}

func (k keyer) key(in []byte) (string, bool, error) {
	v, err := data.Decode(in, "json")
	if err != nil {
		// non-JSON input: key on the raw bytes for `by`-less selection
		if k.by == "" {
			return string(in), true, nil
		}
		return "", false, err
	}

	var selected data.Value
	var ok bool
	if k.by != "" {
		selected, ok, err = data.ByJQ(v, k.by)
	} else {
		selected, ok, err = data.Walk(v, k.path)
	}
	if err != nil || !ok {
		return "", false, err
	}
	return string(selected.Bytes()), true, nil
}

// ── dedupe ─────────────────────────────────────────────────────────────────

type dedupeConfig struct {
	By     string   `psy:"by"`
	Path   []string `psy:"path"`
	Window int      `psy:"window"`
}

// Dedupe drops messages whose key has been seen within the last `window`
// messages. The original message passes through unchanged; the selector only
// computes the dedup key.
func Dedupe(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(dedupeConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	k, err := newKeyer(config.By, config.Path, "dedupe")
	if err != nil {
		return nil, err
	}
	window := config.Window
	if window <= 0 {
		window = 10000
	}

	seen := make(map[string]struct{}, window)
	ring := make([]string, 0, window)

	return mapTransform(func(in []byte) ([]byte, error) {
		key, ok, err := k.key(in)
		if err != nil {
			return nil, err
		}
		if !ok {
			return in, nil
		}
		if _, dup := seen[key]; dup {
			return nil, nil
		}
		if len(ring) >= window {
			delete(seen, ring[0])
			ring = ring[1:]
		}
		seen[key] = struct{}{}
		ring = append(ring, key)
		return in, nil
	}), nil
}

// ── uniq ───────────────────────────────────────────────────────────────────

type uniqConfig struct {
	By   string   `psy:"by"`
	Path []string `psy:"path"`
}

// Uniq drops consecutive duplicates — like the Unix tool — comparing each
// message's key to only the previous one. Lighter than dedupe for sorted or
// already-grouped streams.
func Uniq(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(uniqConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	k, err := newKeyer(config.By, config.Path, "uniq")
	if err != nil {
		return nil, err
	}

	var last string
	var have bool

	return mapTransform(func(in []byte) ([]byte, error) {
		key, ok, err := k.key(in)
		if err != nil {
			return nil, err
		}
		if !ok {
			return in, nil
		}
		if have && key == last {
			return nil, nil
		}
		last, have = key, true
		return in, nil
	}), nil
}

// ── batch ──────────────────────────────────────────────────────────────────

type batchConfig struct {
	Size int `psy:"size"`
}

// Batch collects `size` messages and emits them as a single JSON array. A
// final partial batch — shorter than size — is flushed when the stream ends,
// so no trailing messages are lost. Use size to bound memory.
func Batch(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(batchConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	size := config.Size
	if size <= 0 {
		size = 1
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		buf := make(data.List, 0, size)

		emit := func() bool {
			b, err := data.Encode(buf, "json")
			buf = make(data.List, 0, size)
			if err != nil {
				select {
				case errs <- err:
				case <-ctx.Done():
					return false
				}
				return true
			}
			select {
			case out <- b:
			case <-ctx.Done():
				return false
			}
			return true
		}

		for {
			select {
			case msg, ok := <-in:
				if !ok {
					if len(buf) > 0 {
						emit()
					}
					return
				}
				v, err := data.Decode(msg, "json")
				if err != nil {
					v = data.Bytes(msg)
				}
				buf = append(buf, v)
				if len(buf) >= size {
					if !emit() {
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}
