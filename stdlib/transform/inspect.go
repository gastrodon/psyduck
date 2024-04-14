package transform

import (
	"fmt"

	"github.com/psyduck-etl/sdk"
)

type inspectConfig struct {
	BeString bool `psy:"be-string"`
}

func Inspect(parse sdk.Parser, _ sdk.SpecParser) (sdk.Transformer, error) {
	config := new(inspectConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	formatter := func(data []byte) interface{} { return data }
	if config.BeString {
		formatter = func(data []byte) interface{} { return string(data) }
	}

	return func(data []byte) ([]byte, error) {
		fmt.Println(formatter(data))
		return data, nil
	}, nil
}
