package produce

import (
	"context"
	"fmt"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transport"
)

// Request polls an HTTP endpoint, emitting each response body as a
// message. url must be set on the block. Use interval-ms to pace polling
// and the host-owned stop-after to bound it.
func Request(ctx context.Context, parse sdk.Parser) (sdk.Producer, error) {
	config := new(transport.RequestConfig)
	if err := parse(config); err != nil {
		return nil, err
	}
	h, err := transport.Compose(nil, config.RequestSpec)
	if err != nil {
		return nil, fmt.Errorf("request producer: build request: %w", err)
	}
	interval := time.Duration(config.IntervalMs) * time.Millisecond

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		client := h.Client()
		for {
			data, err := h.Do(ctx, client)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				errs <- fmt.Errorf("request producer: poll: %w", err)
				return
			}
			select {
			case send <- data:
			case <-ctx.Done():
				return
			}
			if interval > 0 {
				select {
				case <-time.After(interval):
					continue
				case <-ctx.Done():
					return
				}
			}
		}
	}, nil
}
