package consume

import (
	"github.com/psyduck-etl/sdk"
)

func Trash(sdk.Parser, sdk.SpecParser) (sdk.Consumer, error) {
	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		for range recv {
		}
	}, nil
}
