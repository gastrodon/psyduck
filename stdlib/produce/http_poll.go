package produce

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/psyduck-etl/sdk"
)

type httpPollConfig struct {
	URL        string            `psy:"url"`
	Method     string            `psy:"method"`
	Headers    map[string]string `psy:"headers"`
	Body       string            `psy:"body"`
	IntervalMs int               `psy:"interval-ms"`
}

func HttpPoll(parse sdk.Parser) (sdk.Producer, error) {
	config := new(httpPollConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Method == "" {
		config.Method = "GET"
	}

	var interval time.Duration
	if config.IntervalMs > 0 {
		interval = time.Duration(config.IntervalMs) * time.Millisecond
	}

	client := &http.Client{}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		for {
			var bodyReader io.Reader
			if config.Body != "" {
				bodyReader = newStringReader(config.Body)
			}

			req, err := http.NewRequest(config.Method, config.URL, bodyReader)
			if err != nil {
				errs <- fmt.Errorf("http-poll build request: %w", err)
				return
			}

			for k, v := range config.Headers {
				req.Header.Set(k, v)
			}

			resp, err := client.Do(req)
			if err != nil {
				errs <- fmt.Errorf("http-poll request: %w", err)
				return
			}

			data, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				errs <- fmt.Errorf("http-poll read body: %w", err)
				return
			}

			send <- data

			if interval > 0 {
				time.Sleep(interval)
			}
		}
	}, nil
}
