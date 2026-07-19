package produce

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

type constant struct {
	Value     string `psy:"value"`
	StopAfter int    `psy:"stop-after"`
}

func Constant(ctx context.Context, parse sdk.Parser) (sdk.Producer, error) {
	config := new(constant)
	if err := parse(config); err != nil {
		return nil, err
	}

	value := []byte(config.Value)
	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		for i := 0; config.StopAfter == 0 || i < config.StopAfter; i++ {
			select {
			case send <- value:
				continue
			case <-ctx.Done():
				return
			}
		}
	}, nil
}
