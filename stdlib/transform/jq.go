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

// Jq applies a jq expression to the message and emits the result.
// If the expression produces no output, the message is dropped (nil returned).
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

	jqOnce := func(msg []byte) (b []byte, drop bool, err error) {
		v, err := runJQ(query, msg)
		if err != nil {
			return nil, false, err
		}
		b, err = marshalJQ(v)
		if err != nil {
			return nil, false, err
		}
		return b, b == nil, nil // no output: drop
	}

	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		defer close(out)
		for {
			select {
			case msg, ok := <-in:
				if !ok {
					return
				}
				b, drop, err := jqOnce(msg)
				if err != nil {
					select {
					case errs <- err:
					case <-ctx.Done():
						return
					}
					continue
				}
				if drop {
					continue
				}
				select {
				case out <- b:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}, nil
}

// runJQ compiles and executes a jq expression against JSON-decoded input.
// It returns the first output value or an error. If the expression produces
// no output, it returns (nil, nil).
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
