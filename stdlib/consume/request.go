package consume

import (
	"context"
	"fmt"
	"net/http"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// Request performs one HTTP request per input message. Each message is a
// JSON request descriptor composed over this resource's spec via
// transport.Dispatch; the response is discarded, and only a transport
// failure or non-success status surfaces as an error.
//
// A bare block defaults to method POST. interval-ms is rejected: it's a
// polling knob and has no meaning for a per-message send.
func Request(ctx context.Context, parse sdk.Parser) (sdk.Consumer, error) {
	config := new(transport.RequestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.IntervalMs != 0 {
		return nil, fmt.Errorf("request consumer: interval-ms is a producer-only attribute")
	}
	if config.Method == "" {
		config.Method = http.MethodPost
	}
	ct := config.RequestSpec

	return func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		for msg := range recv {
			if _, err := transport.Dispatch(ctx, msg, ct); err != nil && ctx.Err() == nil {
				errs <- fmt.Errorf("request: %w", err)
			}
		}
	}, nil
}
