package consume

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/psyduck-etl/sdk"
)

type httpPostConfig struct {
	URL         string            `psy:"url"`
	Method      string            `psy:"method"`
	Headers     map[string]string `psy:"headers"`
	ContentType string            `psy:"content-type"`
}

func HttpPost(parse sdk.Parser) (sdk.Consumer, error) {
	config := new(httpPostConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Method == "" {
		config.Method = "POST"
	}
	if config.ContentType == "" {
		config.ContentType = "application/octet-stream"
	}

	client := &http.Client{}

	return func(recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
		defer close(done)
		defer close(errs)

		for msg := range recv {
			req, err := http.NewRequest(config.Method, config.URL, bytes.NewReader(msg))
			if err != nil {
				errs <- fmt.Errorf("http-post build request: %w", err)
				return
			}

			req.Header.Set("Content-Type", config.ContentType)
			for k, v := range config.Headers {
				req.Header.Set(k, v)
			}

			resp, err := client.Do(req)
			if err != nil {
				errs <- fmt.Errorf("http-post request: %w", err)
				return
			}
			resp.Body.Close()
		}
	}, nil
}
