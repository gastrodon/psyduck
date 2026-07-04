package transform

import (
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
)

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
