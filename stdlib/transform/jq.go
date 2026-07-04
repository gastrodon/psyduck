package transform

import (
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

	return func(in []byte) ([]byte, error) {
		v, err := runJQ(query, in)
		if err != nil {
			return nil, err
		}

		return marshalJQ(v)
	}, nil
}
