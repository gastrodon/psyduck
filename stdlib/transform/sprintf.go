package transform

import (
	"fmt"

	"github.com/psyduck-etl/sdk"
)

type sprintfConfig struct {
	Format   string `psy:"format"`
	Encoding string `psy:"encoding"`
}

func Sprintf(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(sprintfConfig)

	if err := parse(config); err != nil {
		return nil, err
	}

	var decode func([]byte) []any
	switch config.Encoding {
	case "bytes":
		decode = func(in []byte) []any {
			v := make([]any, len(in))
			for i := range in {
				v[i] = in[i]
			}
			return v
		}
	case "string":
		decode = func(in []byte) []any {
			return []any{string(in)}
		}
	default:
		return nil, fmt.Errorf("unable to handle encoding %s", config.Encoding)
	}

	return func(in []byte) ([]byte, error) {
		return []byte(fmt.Sprintf(config.Format, decode(in)...)), nil
	}, nil
}
