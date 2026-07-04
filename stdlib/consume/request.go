package consume

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// Request sends each message as the body of an HTTP request (default POST). It
// is the consumer half of the dual-role `request` resource — you PUT/POST/PATCH
// the way you GET. It decodes into the same transport.RequestConfig the producer
// uses; the producer-only body/interval-ms fields are simply unused here.
func Request(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(transport.RequestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	h := config.HTTP()
	if h.Method == "" {
		h.Method = "POST"
	}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		client := h.Client()
		for msg := range recv {
			if _, err := h.Do(client, msg); err != nil {
				errs <- err
			}
		}
	}, nil
}
