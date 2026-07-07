package produce

import (
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// Request polls an HTTP endpoint, emitting each response body as a message. It
// is the producer half of the dual-role `request` resource; the consumer half
// sends messages to the endpoint. Use interval-ms to pace polling and the
// host-owned stop-after to bound it.
func Request(parse sdk.Parser) (sdk.Producer, error) {
	config := new(transport.RequestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	h := config.HTTP()

	var body []byte
	if config.Body != "" {
		body = []byte(config.Body)
	}
	interval := time.Duration(config.IntervalMs) * time.Millisecond

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		client := h.Client()
		for {
			data, err := h.Do(client, body)
			if err != nil {
				errs <- err
				return
			}
			send <- data
			if interval > 0 {
				time.Sleep(interval)
			}
		}
	}, nil
}
