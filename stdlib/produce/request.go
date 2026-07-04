package produce

import (
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

type requestConfig struct {
	URL          string            `psy:"url"`
	Method       string            `psy:"method"`
	Headers      map[string]string `psy:"headers"`
	Body         string            `psy:"body"`
	QueryParams  map[string]string `psy:"query-params"`
	BasicAuth    string            `psy:"basic-auth"`
	TimeoutMs    int               `psy:"timeout-ms"`
	SuccessCodes []int             `psy:"success-codes"`
	IntervalMs   int               `psy:"interval-ms"`
}

// Request polls an HTTP endpoint, emitting each response body as a message. It
// is the producer half of the dual-role `request` resource; the consumer half
// sends messages to the endpoint. Use interval-ms to pace polling and the
// host-owned stop-after to bound it.
func Request(parse sdk.Parser) (sdk.Producer, error) {
	config := new(requestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	h := transport.HTTP{
		URL:          config.URL,
		Method:       config.Method,
		Headers:      config.Headers,
		QueryParams:  config.QueryParams,
		BasicAuth:    config.BasicAuth,
		TimeoutMs:    config.TimeoutMs,
		SuccessCodes: config.SuccessCodes,
	}

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
