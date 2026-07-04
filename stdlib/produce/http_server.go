package produce

import (
	"fmt"
	"io"
	"net/http"

	"github.com/psyduck-etl/sdk"
)

type httpServerConfig struct {
	Address string `psy:"address"`
	Path    string `psy:"path"`
}

func HttpServer(parse sdk.Parser) (sdk.Producer, error) {
	config := new(httpServerConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	if config.Address == "" {
		config.Address = ":8080"
	}
	if config.Path == "" {
		config.Path = "/"
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		mux := http.NewServeMux()
		mux.HandleFunc(config.Path, func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				errs <- fmt.Errorf("http-server read body: %w", err)
				http.Error(w, "read error", http.StatusInternalServerError)
				return
			}

			send <- body
			w.WriteHeader(http.StatusOK)
		})

		srv := &http.Server{
			Addr:    config.Address,
			Handler: mux,
		}

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errs <- fmt.Errorf("http-server: %w", err)
		}
	}, nil
}
