package transform

import (
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
	"github.com/psyduck-etl/sdk"
)

type jqConfig struct {
	Expression string `psy:"expression"`
}

// Jq applies a jq expression to the message and emits the result.
// If the expression produces no output, the message is filtered out
// (returns keep=false). String outputs are emitted as plain bytes; all
// other types are JSON-encoded.
func Jq(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(jqConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	query, err := gojq.Parse(config.Expression)
	if err != nil {
		return nil, fmt.Errorf("jq: parse expression %q: %w", config.Expression, err)
	}

	return func(in []byte) ([]byte, bool, error) {
		v, err := runJQ(query, in)
		if err != nil {
			return nil, false, err
		}

		return marshalJQ(v)
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
func marshalJQ(v interface{}) ([]byte, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	// Strings are returned as plain bytes (no JSON quoting).
	if s, ok := v.(string); ok {
		return []byte(s), true, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}
