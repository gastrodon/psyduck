package consume

import (
	"context"
	"fmt"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// Request sends each message as the body of an HTTP request (default POST). It
// is the consumer half of the dual-role `request` resource — you PUT/POST/PATCH
// the way you GET. It decodes into the same transport.RequestConfig the producer
// uses; body and interval-ms are producer-only and rejected here.
func Request(ctx context.Context, parse sdk.Parser) (sdk.Consumer, error) {
	config := new(transport.RequestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	if config.Body != "" {
		return nil, fmt.Errorf("request consumer: body is a producer-only attribute")
	}
	if config.IntervalMs != 0 {
		return nil, fmt.Errorf("request consumer: interval-ms is a producer-only attribute")
	}
	h := config.HTTP()
	if h.Method == "" {
		h.Method = "POST"
	}

	return func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		client := h.Client()
		for msg := range recv {
			if _, err := h.Do(ctx, client, msg); err != nil && ctx.Err() == nil {
				errs <- err
			}
		}
	}, nil
}
