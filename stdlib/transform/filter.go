package transform

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
	"github.com/psyduck-etl/sdk"
)

type filterConfig struct {
	Expression string `psy:"expression"`
}

// Filter passes a message through only when the jq expression returns a truthy
// value (anything other than false or null). A nil result also drops the message.
func Filter(ctx context.Context, parse sdk.Parser) (sdk.Transformer, error) {
	config := new(filterConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	query, err := gojq.Parse(config.Expression)
	if err != nil {
		return nil, fmt.Errorf("filter: parse expression %q: %w", config.Expression, err)
	}

	// A gate emits the original message unchanged when dropJQ says keep, and
	// a nil return tells sdk.Map to drop it.
	return sdk.Map(func(msg []byte) ([]byte, error) {
		drop, err := dropJQ(query, msg)
		if err != nil {
			return nil, err
		}
		if drop {
			return nil, nil
		}
		return msg, nil
	}), nil
}

// dropJQ runs a jq expression against in and reports whether the message
// should be dropped: true if the expression yields nothing, null, or false;
// false for any other result (standard jq/select truthiness).
func dropJQ(query *gojq.Query, in []byte) (bool, error) {
	var input interface{}
	if err := json.Unmarshal(in, &input); err != nil {
		return false, fmt.Errorf("jq: parse input JSON: %w", err)
	}

	iter := query.Run(input)
	v, ok := iter.Next()
	if !ok {
		return true, nil
	}
	if err, ok := v.(error); ok {
		return false, fmt.Errorf("jq: %w", err)
	}
	switch val := v.(type) {
	case nil:
		return true, nil
	case bool:
		return !val, nil
	}
	return false, nil
}
