package produce

import "github.com/psyduck-etl/sdk"

func Increment(parse sdk.Parser, _ sdk.SpecParser) (sdk.Producer, error) {
	config := new(struct {
		StopAfter byte `psy:"stop-after"`
	})

	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)
		for i := byte(0); config.StopAfter == 0 || i < config.StopAfter; i++ {
			send <- []byte{i}
		}
	}, nil
}
