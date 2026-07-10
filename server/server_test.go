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

func TestPluginLifecycleOverHTTP(t *testing.T) {
	srv := newTestServer()

	// Empty to start.
	w := do(t, srv, http.MethodGet, "/api/v1/plugins", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d", w.Code)
	}

	// Add (202).
	body, _ := json.Marshal(PluginRequest{Name: "amqp", Source: "https://github.com/psyduck-etl/amqp", Tag: "v0.1.0"})
	wa := do(t, srv, http.MethodPost, "/api/v1/plugins", body)
	if wa.Code != http.StatusAccepted {
		t.Fatalf("add: got %d, want 202", wa.Code)
	}
	if loc := wa.Header().Get("Location"); loc != "/api/v1/plugins/amqp" {
		t.Errorf("location: %q", loc)
	}

	// Duplicate add is a conflict.
	wd := do(t, srv, http.MethodPost, "/api/v1/plugins", body)
	if wd.Code != http.StatusConflict {
		t.Errorf("duplicate add: got %d, want 409", wd.Code)
	}

	// Manifest.
	wm := do(t, srv, http.MethodGet, "/api/v1/plugins/amqp", nil)
	if wm.Code != http.StatusOK {
		t.Fatalf("manifest: got %d", wm.Code)
	}
	var man PluginManifest
	if err := json.Unmarshal(wm.Body.Bytes(), &man); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if man.Name != "amqp" || len(man.Resources) == 0 {
		t.Errorf("manifest: %+v", man)
	}

	// Update (202).
	upd, _ := json.Marshal(PluginRequest{Source: "https://github.com/psyduck-etl/amqp", Tag: "v0.2.0"})
	wu := do(t, srv, http.MethodPut, "/api/v1/plugins/amqp", upd)
	if wu.Code != http.StatusAccepted {
		t.Errorf("update: got %d, want 202", wu.Code)
	}

	// Delete.
	wdel := do(t, srv, http.MethodDelete, "/api/v1/plugins/amqp", nil)
	if wdel.Code != http.StatusOK {
		t.Errorf("delete: got %d, want 200", wdel.Code)
	}
	// Gone now.
	if w := do(t, srv, http.MethodGet, "/api/v1/plugins/amqp", nil); w.Code != http.StatusNotFound {
		t.Errorf("get after delete: got %d, want 404", w.Code)
	}
}

func TestAddPluginRejectsMissingSource(t *testing.T) {
	w := do(t, newTestServer(), http.MethodPost, "/api/v1/plugins", []byte(`{"name":"x"}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing source: got %d, want 400", w.Code)
	}
}

func TestUpdateUnknownPlugin(t *testing.T) {
	w := do(t, newTestServer(), http.MethodPut, "/api/v1/plugins/nope", []byte(`{"source":"x"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("update unknown: got %d, want 404", w.Code)
	}
}
