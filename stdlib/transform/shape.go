package transform

import (
	"fmt"

	"github.com/psyduck-etl/sdk"
	"github.com/psyduck-etl/sdk/data"
)

// ── recode ─────────────────────────────────────────────────────────────────

type recodeConfig struct {
	Decode  string `psy:"decode"`
	Encode  string `psy:"encode"`
	OnError string `psy:"on-error"`
}

// Recode is the universal format converter: it decodes the message with one
// codec chain and re-encodes it with another. "gzip|json" → "json-pretty"
// decompresses and pretty-prints; "base64" → "bytes" decodes base64; and so on.
// It replaces the dozen single-purpose encode/decode transformers.
func Recode(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(recodeConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Decode == "" {
		config.Decode = "bytes"
	}
	if config.Encode == "" {
		config.Encode = "bytes"
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}
	return codecTransformer(config.Decode, config.Encode, onError,
		func(v data.Value) (data.Value, error) { return v, nil }), nil
}

// ── pick ─────────────────────────────────────────────────────────────────

type pickConfig struct {
	Path    []string `psy:"path"`
	By      string   `psy:"by"`
	Decode  string   `psy:"decode"`
	Encode  string   `psy:"encode"`
	OnError string   `psy:"on-error"`
}

// Pick extracts a single value from the message. Selection mirrors the data
// domain: `path = ["a","b"]` walks discrete/object data by key, while
// `by = "<jq>"` selects continuous/linear data by jq. Exactly one must be set.
func Pick(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(pickConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if (len(config.Path) == 0) == (config.By == "") {
		return nil, fmt.Errorf("pick: set exactly one of `path` or `by`")
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	op := func(v data.Value) (data.Value, error) {
		if config.By != "" {
			out, ok, err := data.ByJQ(v, config.By)
			if err != nil || !ok {
				return nil, err
			}
			return out, nil
		}
		out, ok, err := data.Walk(v, data.Path(config.Path))
		if err != nil || !ok {
			return nil, err
		}
		return out, nil
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}

// ── pick-map ───────────────────────────────────────────────────────────────

type pickMapConfig struct {
	Fields  map[string][]string `psy:"fields"`
	Decode  string              `psy:"decode"`
	Encode  string              `psy:"encode"`
	OnError string              `psy:"on-error"`
}

// PickMap reshapes the message into a new object: each entry maps a destination
// key to a source path walked in the input. It backs the transpose/remap use
// case. Missing source paths are simply absent from the result.
func PickMap(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(pickMapConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	op := func(v data.Value) (data.Value, error) {
		dsts := make([]string, 0, len(config.Fields))
		srcs := make([]data.Path, 0, len(config.Fields))
		for dst, src := range config.Fields {
			dsts = append(dsts, dst)
			srcs = append(srcs, data.Path(src))
		}
		out, _ := data.Transpose(v, srcs, dsts)
		return out, nil
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}

// ── set ────────────────────────────────────────────────────────────────────

type setConfig struct {
	Values  map[string]string `psy:"values"`
	Decode  string            `psy:"decode"`
	Encode  string            `psy:"encode"`
	OnError string            `psy:"on-error"`
}

// Set adds or overwrites object fields with static string values. It backs the
// annotate/tag use case (set a source name, a constant flag, …).
func Set(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(setConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	op := func(v data.Value) (data.Value, error) {
		obj, ok := v.(data.Object)
		if !ok {
			return nil, fmt.Errorf("set: want object, got %s", v.Kind())
		}
		next := make(data.Object, len(obj)+len(config.Values))
		for k, val := range obj {
			next[k] = val
		}
		for k, val := range config.Values {
			next[k] = data.Str(val)
		}
		return next, nil
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}

// ── drop ───────────────────────────────────────────────────────────────────

type dropConfig struct {
	Fields  []string `psy:"fields"`
	Decode  string   `psy:"decode"`
	Encode  string   `psy:"encode"`
	OnError string   `psy:"on-error"`
}

// Drop removes the named top-level fields from an object.
func Drop(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(dropConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	onError, err := data.ParseOnError(config.OnError)
	if err != nil {
		return nil, err
	}

	op := func(v data.Value) (data.Value, error) {
		obj, ok := v.(data.Object)
		if !ok {
			return nil, fmt.Errorf("drop: want object, got %s", v.Kind())
		}
		next := make(data.Object, len(obj))
		for k, val := range obj {
			next[k] = val
		}
		for _, f := range config.Fields {
			delete(next, f)
		}
		return next, nil
	}
	return codecTransformer(config.Decode, config.Encode, onError, op), nil
}
