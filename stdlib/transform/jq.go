package transform

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
	"github.com/psyduck-etl/sdk"
)

type jqConfig struct {
	Expression string `psy:"expression"`
}

// Jq applies a jq expression to each message. A jq expression yields a stream
// of 0, 1, or many values per input, and each value becomes its own output
// message — so Jq is explosive and cannot use sdk.Map (which is 1-to-0/1).
// String outputs are emitted as plain bytes; all other types are JSON-encoded.
func Jq(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(jqConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	query, err := gojq.Parse(config.Expression)
	if err != nil {
		return nil, fmt.Errorf("jq: parse expression %q: %w", config.Expression, err)
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		emitErr := func(err error) (stop bool) {
			select {
			case errs <- err:
				return false
			case <-ctx.Done():
				return true
			}
		}
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				var input interface{}
				if err := json.Unmarshal(msg, &input); err != nil {
					if emitErr(fmt.Errorf("jq: parse input JSON: %w", err)) {
						return
					}
					continue
				}
				iter := query.Run(input)
				for {
					v, ok := iter.Next()
					if !ok {
						break
					}
					if err, ok := v.(error); ok {
						if emitErr(fmt.Errorf("jq: %w", err)) {
							return
						}
						continue
					}
					b, err := marshalJQ(v)
					if err != nil {
						if emitErr(err) {
							return
						}
						continue
					}
					if b == nil { // null / no output: drop
						continue
					}
					select {
					case out <- b:
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// runJQ runs a jq expression and returns its first output value (or nil if the
// expression yields nothing). Used by predicate callers like Filter that only
// care about a single result; Jq itself drains the whole iterator inline.
func runJQ(query *gojq.Query, in []byte) (interface{}, error) {
	var input interface{}
	if err := json.Unmarshal(in, &input); err != nil {
		return nil, fmt.Errorf("jq: parse input JSON: %w", err)
	}

	iter := query.Run(input)
	v, ok := iter.Next()
	if !ok {
		return nil, nil
	}
	if err, ok := v.(error); ok {
		return nil, fmt.Errorf("jq: %w", err)
	}
	return v, nil
}

// marshalJQ converts a jq output value back to bytes.
func marshalJQ(v interface{}) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	// Strings are returned as plain bytes (no JSON quoting).
	if s, ok := v.(string); ok {
		return []byte(s), nil
	}
	return json.Marshal(v)
}
