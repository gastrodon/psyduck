package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// maxDispatchBody caps the size of a dispatch payload we'll read, so a
// runaway or hostile body can't exhaust memory. A .psy document is small;
// 1 MiB is generous.
const maxDispatchBody = 1 << 20

// Server is the HTTP API for one psyduck instance. It is a thin marshaling
// layer over a Supervisor — construct it with New, then either mount
// Handler() into your own http.Server or call ListenAndServe.
type Server struct {
	sup Supervisor
	mux *http.ServeMux

	// authUser/authPass gate the plugin routes when set (see auth.go and
	// WithBasicAuth). Empty means auth is disabled.
	authUser string
	authPass string
}

// New builds a Server backed by sup, applies any options, and wires every
// route.
func New(sup Supervisor, opts ...Option) *Server {
	s := &Server{sup: sup, mux: http.NewServeMux()}
	for _, o := range opts {
		o(s)
	}
	s.routes()
	return s
}

// Handler exposes the routed mux, for tests (httptest) or for mounting the
// API under a larger server.
func (s *Server) Handler() http.Handler { return s.mux }

// routes registers every endpoint. Patterns use net/http's method+wildcard
// syntax (Go 1.22+): the method is matched, {id} is a path variable read
// back with r.PathValue("id"). More specific patterns win, so the /stats
// sub-route coexists with the bare {id} route.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)

	s.mux.HandleFunc("GET /api/v1/instance", s.handleInstance)
	s.mux.HandleFunc("GET /api/v1/graph", s.handleGraph)

	s.mux.HandleFunc("GET /api/v1/pipelines", s.handleListPipelines)
	s.mux.HandleFunc("POST /api/v1/pipelines", s.handleDispatch)
	s.mux.HandleFunc("GET /api/v1/pipelines/{id}", s.handleGetPipeline)
	s.mux.HandleFunc("DELETE /api/v1/pipelines/{id}", s.handleCancelPipeline)
	s.mux.HandleFunc("GET /api/v1/pipelines/{id}/stats", s.handlePipelineStats)

	// Plugin routes are gated by Basic auth when a credential is configured
	// (see auth.go). Registration clones + compiles operator-supplied
	// sources, so the whole subtree is guarded, reads included.
	s.mux.HandleFunc("GET /api/v1/plugins", s.guard(s.handleListPlugins))
	s.mux.HandleFunc("POST /api/v1/plugins", s.guard(s.handleAddPlugin))
	s.mux.HandleFunc("GET /api/v1/plugins/{name}", s.guard(s.handleGetPlugin))
	s.mux.HandleFunc("PUT /api/v1/plugins/{name}", s.guard(s.handleUpdatePlugin))
	s.mux.HandleFunc("DELETE /api/v1/plugins/{name}", s.guard(s.handleRemovePlugin))

	// Stage 2 (peer-to-peer) is reserved but not implemented.
	s.mux.HandleFunc("GET /api/v1/peers", s.handlePeers)
}

// ListenAndServe serves the API on addr until ctx is canceled, then shuts
// down gracefully (draining in-flight requests up to a short grace period).
// It returns nil on a clean ctx-driven shutdown and the underlying error
// otherwise.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutCtx)
	}
}

// --- handlers ---------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleInstance(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.sup.Instance())
}

func (s *Server) handleListPipelines(w http.ResponseWriter, _ *http.Request) {
	// Wrap in an object so the top-level response is extensible (paging,
	// filters) without breaking clients that read a bare array.
	writeJSON(w, http.StatusOK, map[string]any{"pipelines": s.sup.List()})
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	p, ok := s.sup.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such pipeline")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handlePipelineStats(w http.ResponseWriter, r *http.Request) {
	p, ok := s.sup.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such pipeline")
		return
	}
	writeJSON(w, http.StatusOK, p.Stats)
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request) {
	var req DispatchRequest
	if err := decodeJSON(r, &req, maxDispatchBody); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	p, err := s.sup.Dispatch(req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidSource):
			writeError(w, http.StatusBadRequest, "source is required")
		default:
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	// 202: accepted, will run asynchronously. Location points at the record.
	w.Header().Set("Location", "/api/v1/pipelines/"+p.ID)
	writeJSON(w, http.StatusAccepted, p)
}

func (s *Server) handleCancelPipeline(w http.ResponseWriter, r *http.Request) {
	err := s.sup.Cancel(r.PathValue("id"))
	switch {
	case err == nil:
		p, _ := s.sup.Get(r.PathValue("id"))
		writeJSON(w, http.StatusOK, p)
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "no such pipeline")
	case errors.Is(err, ErrNotCancelable):
		writeError(w, http.StatusConflict, "pipeline already finished")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) handleGraph(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.sup.Graph())
}

// handlePeers is the reserved stage-2 surface. It answers 501 with a note so
// a client can discover that peers are planned but unavailable, rather than
// 404 (which would read as "no such route").
func (s *Server) handlePeers(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "peer-to-peer is not implemented yet (stage 2); see docs/http-api.md")
}

// --- json helpers -----------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads a JSON body into v, bounded by limit bytes and rejecting
// unknown fields so a typo in a dispatch payload is an error, not a silent
// no-op.
func decodeJSON(r *http.Request, v any, limit int64) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}
