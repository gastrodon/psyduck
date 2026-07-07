package transform

import (
	"fmt"

	"github.com/itchyny/gojq"
	"github.com/psyduck-etl/sdk"
)

type filterConfig struct {
	Expression string `psy:"expression"`
}

// Filter passes a message through only when the jq expression returns a truthy
// value (anything other than false or null). A nil result also drops the message.
func Filter(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(filterConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	query, err := gojq.Parse(config.Expression)
	if err != nil {
		return nil, fmt.Errorf("filter: parse expression %q: %w", config.Expression, err)
	}

	return func(in []byte) ([]byte, error) {
		v, err := runJQ(query, in)
		if err != nil {
			return nil, err
		}

		switch val := v.(type) {
		case nil:
			return nil, nil // drop
		case bool:
			if !val {
				return nil, nil // drop
			}
		}

		return in, nil // pass through original message
	}, nil
}
