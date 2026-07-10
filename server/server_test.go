package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixedClock returns a deterministic time so uptime/timestamps are testable.
func fixedClock() func() time.Time {
	t := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func newTestServer() *Server {
	return New(newStubSupervisor(fixedClock()))
}

func do(t *testing.T, srv *Server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	return w
}

func TestHealth(t *testing.T) {
	w := do(t, newTestServer(), http.MethodGet, "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("health: got %d, want 200", w.Code)
	}
}

func TestInstance(t *testing.T) {
	w := do(t, newTestServer(), http.MethodGet, "/api/v1/instance", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("instance: got %d, want 200", w.Code)
	}
	var got InstanceInfo
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Version != Version {
		t.Errorf("version: got %q, want %q", got.Version, Version)
	}
	// Seeded with one running + one succeeded pipeline.
	if got.Pipelines.Total != 2 || got.Pipelines.Running != 1 || got.Pipelines.Succeeded != 1 {
		t.Errorf("counts: %+v", got.Pipelines)
	}
}

func TestListPipelines(t *testing.T) {
	w := do(t, newTestServer(), http.MethodGet, "/api/v1/pipelines", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d, want 200", w.Code)
	}
	var got struct {
		Pipelines []PipelineInfo `json:"pipelines"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Pipelines) != 2 {
		t.Fatalf("pipelines: got %d, want 2", len(got.Pipelines))
	}
}

func TestGetPipelineAndStats(t *testing.T) {
	srv := newTestServer()
	// pipe-000001 is the seeded running pipeline.
	w := do(t, srv, http.MethodGet, "/api/v1/pipelines/pipe-000001", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: got %d, want 200", w.Code)
	}
	var p PipelineInfo
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Name != "ingest" || p.Status != StatusRunning {
		t.Errorf("pipeline: got name=%q status=%q", p.Name, p.Status)
	}

	ws := do(t, srv, http.MethodGet, "/api/v1/pipelines/pipe-000001/stats", nil)
	if ws.Code != http.StatusOK {
		t.Fatalf("stats: got %d, want 200", ws.Code)
	}
	var st Stats
	if err := json.Unmarshal(ws.Body.Bytes(), &st); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if st.Produced == 0 {
		t.Errorf("stats: expected non-zero produced, got %+v", st)
	}
}

func TestGetPipelineNotFound(t *testing.T) {
	w := do(t, newTestServer(), http.MethodGet, "/api/v1/pipelines/nope", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing: got %d, want 404", w.Code)
	}
}

func TestDispatchLifecycle(t *testing.T) {
	srv := newTestServer()

	body, _ := json.Marshal(DispatchRequest{
		Name:   "adhoc",
		Source: `pipeline "adhoc" {}`,
		Labels: map[string]string{"who": "test"},
	})
	w := do(t, srv, http.MethodPost, "/api/v1/pipelines", body)
	if w.Code != http.StatusAccepted {
		t.Fatalf("dispatch: got %d, want 202", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "/api/v1/pipelines/") {
		t.Errorf("location header: %q", loc)
	}
	var created PipelineInfo
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Status != StatusPending {
		t.Errorf("dispatched status: got %q, want pending", created.Status)
	}

	// Now cancel it.
	wc := do(t, srv, http.MethodDelete, "/api/v1/pipelines/"+created.ID, nil)
	if wc.Code != http.StatusOK {
		t.Fatalf("cancel: got %d, want 200", wc.Code)
	}
	var canceled PipelineInfo
	if err := json.Unmarshal(wc.Body.Bytes(), &canceled); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if canceled.Status != StatusCanceled {
		t.Errorf("canceled status: got %q", canceled.Status)
	}

	// Canceling again is a conflict (already terminal).
	wc2 := do(t, srv, http.MethodDelete, "/api/v1/pipelines/"+created.ID, nil)
	if wc2.Code != http.StatusConflict {
		t.Errorf("re-cancel: got %d, want 409", wc2.Code)
	}
}

func TestDispatchRejectsEmptySource(t *testing.T) {
	srv := newTestServer()
	body, _ := json.Marshal(DispatchRequest{Name: "x"})
	w := do(t, srv, http.MethodPost, "/api/v1/pipelines", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty source: got %d, want 400", w.Code)
	}
}

func TestDispatchRejectsUnknownField(t *testing.T) {
	srv := newTestServer()
	w := do(t, srv, http.MethodPost, "/api/v1/pipelines", []byte(`{"nope": 1}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: got %d, want 400", w.Code)
	}
}

func TestGraph(t *testing.T) {
	w := do(t, newTestServer(), http.MethodGet, "/api/v1/graph", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("graph: got %d, want 200", w.Code)
	}
	var g Graph
	if err := json.Unmarshal(w.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Two pipeline nodes plus their stage nodes; edges wire the chains.
	if len(g.Nodes) == 0 || len(g.Edges) == 0 {
		t.Errorf("graph: nodes=%d edges=%d, want both non-empty", len(g.Nodes), len(g.Edges))
	}
}

func TestMetrics(t *testing.T) {
	w := do(t, newTestServer(), http.MethodGet, "/metrics", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("metrics: got %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		"# TYPE psyduck_pipelines gauge",
		"psyduck_pipelines{status=\"running\"} 1",
		"# TYPE psyduck_pipeline_messages_produced_total counter",
		"psyduck_pipeline_messages_produced_total{pipeline=\"pipe-000001\",name=\"ingest\"} 1280",
		"psyduck_instance_uptime_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %q\n---\n%s", want, body)
		}
	}
}

func TestPeersNotImplemented(t *testing.T) {
	w := do(t, newTestServer(), http.MethodGet, "/api/v1/peers", nil)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("peers: got %d, want 501", w.Code)
	}
}
