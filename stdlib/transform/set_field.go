package transform

import (
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
	"github.com/psyduck-etl/sdk"
)

type setFieldConfig struct {
	Field      string `psy:"field"`
	Expression string `psy:"expression"`
}

// SetField evaluates a jq expression against the message and sets the result
// as the value of the named JSON field. The message must be a JSON object.
// Useful for adding computed fields:
//   - expression = "now | todate"  → ISO timestamp
//   - expression = "now"           → Unix timestamp (float)
//   - expression = "\"static\""    → static string
//   - expression = ".x | . * 2"   → transform an existing field
func SetField(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(setFieldConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	query, err := gojq.Parse(config.Expression)
	if err != nil {
		return nil, fmt.Errorf("set-field: parse expression %q: %w", config.Expression, err)
	}

	return func(in []byte) ([]byte, error) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(in, &obj); err != nil {
			return nil, fmt.Errorf("set-field: input is not a JSON object: %w", err)
		}

		v, err := runJQ(query, in)
		if err != nil {
			return nil, err
		}

		fieldVal, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("set-field: marshal value: %w", err)
		}

		obj[config.Field] = json.RawMessage(fieldVal)

		out, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("set-field: marshal output: %w", err)
		}

		return out, nil
	}, nil
}
