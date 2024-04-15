package produce

import (
	"github.com/psyduck-etl/sdk"
)

type constant struct {
	Value     string `psy:"value"`
	StopAfter int    `psy:"stop-after"`
}

func Constant(parse sdk.Parser) (sdk.Producer, error) {
	config := new(constant)
	if err := parse(config); err != nil {
		return nil, err
	}

	value := []byte(config.Value)
	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		for i := 0; config.StopAfter == 0 || i < config.StopAfter; i++ {
			send <- value
		}
	}, nil
}
