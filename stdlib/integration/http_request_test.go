package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/transform"
)

// TestHTTPRequestConsumer verifies consume.Request as an outbound HTTP poster:
// every message on the recv channel must arrive at the server exactly once with
// the configured method and headers, and the consumer must close done cleanly
// when recv is closed (graceful exit, no hang).
func TestHTTPRequestConsumer(t *testing.T) {
	const N = 10

	var mu sync.Mutex
	var received []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("want POST, got %s", r.Method)
		}
		if r.Header.Get("X-Source") != "integration-test" {
			t.Errorf("want X-Source=integration-test, got %q", r.Header.Get("X-Source"))
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(body))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := consume.Request(context.Background(), parser(map[string]any{
		"url":           srv.URL,
		"method":        "POST",
		"headers":       map[string]string{"X-Source": "integration-test"},
		"success-codes": []int{200},
		"timeout-ms":    5000,
	}))
	if err != nil {
		t.Fatalf("Request consumer: %v", err)
	}

	recv := make(chan []byte)
	cerrs := make(chan error, 1)
	cdone := make(chan struct{})
	go c(t.Context(), recv, cerrs, cdone)
	drainErrs(cerrs)

	want := make([]string, N)
	for i := 0; i < N; i++ {
		want[i] = fmt.Sprintf("payload-%d", i)
		recv <- []byte(want[i])
	}
	close(recv)

	// Consumer must exit cleanly after recv closes.
	select {
	case <-cdone:
	case <-time.After(5 * time.Second):
		t.Fatal("consumer did not close done after recv closed")
	}

	mu.Lock()
	got := append([]string(nil), received...)
	mu.Unlock()

	if len(got) != N {
		t.Errorf("server got %d requests, want %d", len(got), N)
	}
	assertSameSet(t, got, want)
	assertNoDups(t, got)
}

// TestHTTPRequestTransform verifies transform.Request as a per-message HTTP
// pull: the URL is templated from the decoded input message, and the
// response body is emitted downstream unchanged for the next stage to parse.
func TestHTTPRequestTransform(t *testing.T) {
	var mu sync.Mutex
	var gotPaths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPaths = append(gotPaths, r.URL.Path)
		mu.Unlock()
		fmt.Fprintf(w, `{"tenant":%q}`, r.URL.Path[len("/boards/"):])
	}))
	t.Cleanup(srv.Close)

	fn, err := transform.Request(context.Background(), parser(map[string]any{
		"url":        srv.URL + "/boards/{{.}}",
		"timeout-ms": 5000,
	}))
	if err != nil {
		t.Fatalf("Request transform: %v", err)
	}

	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(t.Context(), in, out, errs)
	}()
	drainErrs(errs)

	tenants := []string{"stripe", "airbnb", "coinbase"}
	go func() {
		defer close(in)
		for _, tenant := range tenants {
			in <- []byte(tenant)
		}
	}()

	got := readN(t, out, len(tenants), 5*time.Second)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transform did not close out after in closed")
	}

	want := make([]string, len(tenants))
	for i, tenant := range tenants {
		want[i] = fmt.Sprintf(`{"tenant":%q}`, tenant)
	}
	assertSameSet(t, got, want)

	mu.Lock()
	paths := append([]string(nil), gotPaths...)
	mu.Unlock()
	wantPaths := make([]string, len(tenants))
	for i, tenant := range tenants {
		wantPaths[i] = "/boards/" + tenant
	}
	assertSameSet(t, paths, wantPaths)
}

// TestHTTPRequestTransformOnErrorDrop verifies that on-error="drop" swallows
// a request failure (here, a 500 outside the default success-codes) instead
// of raising it, and keeps the stage running for later messages.
func TestHTTPRequestTransformOnErrorDrop(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/bad" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, "ok")
	}))
	t.Cleanup(srv.Close)

	fn, err := transform.Request(context.Background(), parser(map[string]any{
		"url":        srv.URL + "/{{.}}",
		"on-error":   "drop",
		"timeout-ms": 5000,
	}))
	if err != nil {
		t.Fatalf("Request transform: %v", err)
	}

	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(t.Context(), in, out, errs)
	}()
	drainErrs(errs)

	go func() {
		defer close(in)
		in <- []byte("bad")
		in <- []byte("good")
	}()

	got := readN(t, out, 1, 5*time.Second)
	if got[0] != "ok" {
		t.Errorf("want %q, got %q", "ok", got[0])
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transform did not close out after in closed")
	}
}

// TestHTTPRequestConsumerErrorOnBadStatus verifies that a non-success response
// code surfaces on the error channel rather than silently succeeding.
func TestHTTPRequestConsumerErrorOnBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c, err := consume.Request(context.Background(), parser(map[string]any{
		"url":           srv.URL,
		"method":        "POST",
		"success-codes": []int{200},
		"timeout-ms":    2000,
	}))
	if err != nil {
		t.Fatalf("Request consumer: %v", err)
	}

	recv := make(chan []byte, 1)
	cerrs := make(chan error, 1)
	cdone := make(chan struct{})
	go c(t.Context(), recv, cerrs, cdone)

	recv <- []byte("trigger-error")
	close(recv)

	var gotErr error
	select {
	case e := <-cerrs:
		gotErr = e
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for error")
	}
	if gotErr == nil {
		t.Error("expected an error for 500 response, got nil")
	}
}
