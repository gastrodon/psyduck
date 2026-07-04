package consume

import (
	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

type requestConfig struct {
	URL          string            `psy:"url"`
	Method       string            `psy:"method"`
	Headers      map[string]string `psy:"headers"`
	Body         string            `psy:"body"` // producer-side; ignored when posting
	QueryParams  map[string]string `psy:"query-params"`
	BasicAuth    string            `psy:"basic-auth"`
	TimeoutMs    int               `psy:"timeout-ms"`
	SuccessCodes []int             `psy:"success-codes"`
	IntervalMs   int               `psy:"interval-ms"` // producer-side; ignored when posting
}

// Request sends each message as the body of an HTTP request (default POST). It
// is the consumer half of the dual-role `request` resource — you PUT/POST/PATCH
// the way you GET.
func Request(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(requestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	method := config.Method
	if method == "" {
		method = "POST"
	}
	h := transport.HTTP{
		URL:          config.URL,
		Method:       method,
		Headers:      config.Headers,
		QueryParams:  config.QueryParams,
		BasicAuth:    config.BasicAuth,
		TimeoutMs:    config.TimeoutMs,
		SuccessCodes: config.SuccessCodes,
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
