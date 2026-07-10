package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func authServer() *Server {
	return New(newStubSupervisor(fixedClock()), WithBasicAuth("ops", "s3cret"))
}

// doWithAuth issues a request carrying optional Basic credentials.
func doWithAuth(t *testing.T, srv *Server, method, path, user, pass string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if user != "" || pass != "" {
		r.SetBasicAuth(user, pass)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	return w
}

func TestPluginRoutesRequireAuth(t *testing.T) {
	srv := authServer()

	// No credentials -> 401 with a challenge.
	w := doWithAuth(t, srv, http.MethodGet, "/api/v1/plugins", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("no creds: got %d, want 401", w.Code)
	}
	if ch := w.Header().Get("WWW-Authenticate"); ch == "" {
		t.Error("missing WWW-Authenticate challenge")
	}

	// Wrong credentials -> 401.
	if w := doWithAuth(t, srv, http.MethodGet, "/api/v1/plugins", "ops", "nope"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong creds: got %d, want 401", w.Code)
	}

	// Correct credentials -> 200.
	if w := doWithAuth(t, srv, http.MethodGet, "/api/v1/plugins", "ops", "s3cret"); w.Code != http.StatusOK {
		t.Fatalf("good creds: got %d, want 200", w.Code)
	}
}

func TestWriteRoutesRequireAuth(t *testing.T) {
	srv := authServer()
	// A mutating plugin route is guarded too.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/plugins", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated add: got %d, want 401", w.Code)
	}
}

func TestNonPluginRoutesStayOpen(t *testing.T) {
	srv := authServer()
	// Observability/health remain reachable without credentials, so a UI or
	// a Prometheus scrape isn't blocked by the plugin gate.
	for _, path := range []string{"/healthz", "/api/v1/instance", "/api/v1/pipelines", "/metrics"} {
		if w := doWithAuth(t, srv, http.MethodGet, path, "", ""); w.Code != http.StatusOK {
			t.Errorf("%s without creds: got %d, want 200", path, w.Code)
		}
	}
}

func TestAuthDisabledLeavesPluginsOpen(t *testing.T) {
	// The default stub server has no credentials configured.
	srv := newTestServer()
	if w := doWithAuth(t, srv, http.MethodGet, "/api/v1/plugins", "", ""); w.Code != http.StatusOK {
		t.Fatalf("auth-disabled plugins: got %d, want 200", w.Code)
	}
}
