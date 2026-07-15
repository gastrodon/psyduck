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

	keep := func(msg []byte) (bool, error) {
		v, err := runJQ(query, msg)
		if err != nil {
			return false, err
		}
		switch val := v.(type) {
		case nil:
			return false, nil
		case bool:
			return val, nil
		}
		return true, nil
	}

	// A gate emits the original message unchanged when keep passes, and a
	// nil return tells sdk.Map to drop it.
	return sdk.Map(func(msg []byte) ([]byte, error) {
		pass, err := keep(msg)
		if err != nil {
			return nil, err
		}
		if !pass {
			return nil, nil
		}
		return msg, nil
	}), nil
}
