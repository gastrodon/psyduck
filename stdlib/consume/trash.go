package consume

import (
	"context"

	"github.com/psyduck-etl/sdk"
)

func Trash(ctx context.Context, parse sdk.Parser) (sdk.Consumer, error) {
	return func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		for range recv {
		}
	}, nil
}
