package transform

import (
	"encoding/json"
	"strings"

	"github.com/psyduck-etl/sdk"
)

type zoomConfig struct {
	Field string `psy:"field"`
}

type zoomTarget []byte

func (me *zoomTarget) UnmarshalJSON(data []byte) error {
	*me = zoomTarget(strings.Trim(string(data), "\""))
	return nil
}

func Zoom(parse sdk.Parser) (sdk.Transformer, error) {
	config := new(zoomConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(data []byte) ([]byte, error) {
		source := make(map[string]zoomTarget)
		if err := json.Unmarshal(data, &source); err != nil {
			return nil, err
		}

		return source[config.Field], nil
	}, nil
}
