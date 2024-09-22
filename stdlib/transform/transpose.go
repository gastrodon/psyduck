package transform

import (
	"encoding/json"
	"fmt"

	"github.com/psyduck-etl/sdk"
)

type transposeConfig struct {
	Fields map[string][]string `cty:"fields"`
}

func readField(data map[string]zoomTarget, field []string) ([]byte, error) {
	if len(field) == 0 {
		return nil, fmt.Errorf("no fields passed")
	}

	head := data[field[0]]
	if len(field) == 1 || head == nil {
		return head, nil
	}

	b := make(map[string]zoomTarget)
	if err := json.Unmarshal(head, &b); err != nil {
		return nil, fmt.Errorf("failed to unmarshap head %v: %s", head, err)
	}

	return readField(b, field[1:])
}

func Transpose(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(transposeConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(in []byte) ([]byte, error) {
		b := make(map[string]zoomTarget)
		if err := json.Unmarshal(in, &b); err != nil {
			return nil, fmt.Errorf("failed to unmarshal in %s: %s", in, err)
		}

		transposed := make(map[string]string, len(config.Fields))
		for dest, src := range config.Fields {
			v, err := readField(b, src)
			if err != nil {
				return nil, fmt.Errorf("failed to read field %v -> %s: %s", src, dest, err)
			}

			transposed[dest] = string(v)
		}

		out, err := json.Marshal(transposed)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal transposed %v: %s", transposed, err)
		}

		return out, nil
	}, nil
}
