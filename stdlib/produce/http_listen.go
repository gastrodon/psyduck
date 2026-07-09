package produce

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/psyduck-etl/sdk"
)

type httpListenConfig struct {
	Address        string `psy:"address"`
	Path           string `psy:"path"`
	Method         string `psy:"method"`
	Status         int    `psy:"status"`
	Reply          string `psy:"reply"`
	MaxBodyBytes   int    `psy:"max-body-bytes"`
	ReadTimeoutMs  int    `psy:"read-timeout-ms"`
	WriteTimeoutMs int    `psy:"write-timeout-ms"`
	IdleTimeoutMs  int    `psy:"idle-timeout-ms"`
}

// HTTPListen runs an HTTP server and emits each matching request body as a
// message. An empty method matches any verb. Body reads are capped at
// max-body-bytes (0 opts out of the cap) and connection lifetimes are bounded
// by the read/write/idle timeouts.
func HTTPListen(parse sdk.Parser) (sdk.Producer, error) {
	config := new(httpListenConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		mux := http.NewServeMux()
		mux.HandleFunc(config.Path, func(w http.ResponseWriter, r *http.Request) {
			if config.Method != "" && r.Method != config.Method {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			reader := r.Body
			if config.MaxBodyBytes > 0 {
				reader = http.MaxBytesReader(w, r.Body, int64(config.MaxBodyBytes))
			}
			body, err := io.ReadAll(reader)
			if err != nil {
				var mbe *http.MaxBytesError
				if errors.As(err, &mbe) {
					http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			select {
			case send <- body:
			case <-ctx.Done():
				http.Error(w, "shutting down", http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(config.Status)
			if config.Reply != "" {
				_, _ = io.WriteString(w, config.Reply)
			}
		})

		srv := &http.Server{
			Addr:         config.Address,
			Handler:      mux,
			ReadTimeout:  time.Duration(config.ReadTimeoutMs) * time.Millisecond,
			WriteTimeout: time.Duration(config.WriteTimeoutMs) * time.Millisecond,
			IdleTimeout:  time.Duration(config.IdleTimeoutMs) * time.Millisecond,
		}
		stop := closeOnDone(ctx, srv)
		defer stop()

		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}, nil
}
