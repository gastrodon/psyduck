package transform

import (
	"encoding/json"

	"github.com/psyduck-etl/sdk"
)

type snippetConfig struct {
	Fields []string `psy:"fields"`
}

func Snippet(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(snippetConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(data []byte) ([]byte, error) {
		source := make(map[string]interface{})
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}

		items := make(map[string]interface{}, len(config.Fields))
		for _, field := range config.Fields {
			items[field] = source[field]
		}

		dataBytes, err := json.Marshal(items)
		if err != nil {
			return nil, err
		}

		return dataBytes, nil
	}, nil
}
