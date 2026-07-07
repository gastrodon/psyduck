package produce

import (
	"io"
	"net/http"

	"github.com/psyduck-etl/sdk"
)

type httpListenConfig struct {
	Address string `psy:"address"`
	Path    string `psy:"path"`
	Method  string `psy:"method"`
	Status  int    `psy:"status"`
	Reply   string `psy:"reply"`
}

// HTTPListen runs an HTTP server and emits each matching request body as a
// message. An empty method matches any verb.
func HTTPListen(parse sdk.Parser) (sdk.Producer, error) {
	config := new(httpListenConfig)
	if err := parse(config); err != nil {
		return nil, err
	}

	return func(send chan<- []byte, errs chan<- error) {
		defer close(send)
		defer close(errs)

		mux := http.NewServeMux()
		mux.HandleFunc(config.Path, func(w http.ResponseWriter, r *http.Request) {
			if config.Method != "" && r.Method != config.Method {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			send <- body
			w.WriteHeader(config.Status)
			if config.Reply != "" {
				_, _ = io.WriteString(w, config.Reply)
			}
		})

		if err := http.ListenAndServe(config.Address, mux); err != nil {
			errs <- err
		}
	}, nil
}
