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
	"github.com/gastrodon/psyduck/stdlib/produce"
)

// TestHTTPRequestConsumer verifies consume.Request as an outbound HTTP
// poster: each message is its own JSON request descriptor (Cp), composed
// over the consumer's block (Ct) — method and X-Source here come from the
// block, body from each message — and every one must arrive at the server
// exactly once. The consumer must also close done cleanly when recv is
// closed (graceful exit, no hang).
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
		recv <- []byte(fmt.Sprintf(`{"body":%q}`, want[i]))
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

// TestHTTPRequestConsumerDefaultsToPOST verifies that a block with no
// method set on either layer posts by default.
func TestHTTPRequestConsumerDefaultsToPOST(t *testing.T) {
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := consume.Request(context.Background(), parser(map[string]any{
		"url":        srv.URL,
		"timeout-ms": 2000,
	}))
	if err != nil {
		t.Fatalf("Request consumer: %v", err)
	}

	recv := make(chan []byte, 1)
	cerrs := make(chan error, 1)
	cdone := make(chan struct{})
	go c(t.Context(), recv, cerrs, cdone)
	drainErrs(cerrs)

	recv <- []byte("{}")
	close(recv)

	select {
	case <-cdone:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not close done after recv closed")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
}

// TestHTTPRequestConsumerMessageOverridesBlock verifies a message's own
// request descriptor (Cp) can override the block's url entirely, not just
// supply a body.
func TestHTTPRequestConsumerMessageOverridesBlock(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c, err := consume.Request(context.Background(), parser(map[string]any{
		"url":        srv.URL + "/default",
		"timeout-ms": 2000,
	}))
	if err != nil {
		t.Fatalf("Request consumer: %v", err)
	}

	recv := make(chan []byte, 1)
	cerrs := make(chan error, 1)
	cdone := make(chan struct{})
	go c(t.Context(), recv, cerrs, cdone)
	drainErrs(cerrs)

	recv <- []byte(fmt.Sprintf(`{"url":%q}`, srv.URL+"/override"))
	close(recv)

	select {
	case <-cdone:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not close done after recv closed")
	}
	if gotPath != "/override" {
		t.Errorf("path = %s, want /override (message url should win over block url)", gotPath)
	}
}

// TestHTTPRequestProducerRequiresURL verifies the producer rejects a block
// with no url at bind time, before it ever starts polling.
func TestHTTPRequestProducerRequiresURL(t *testing.T) {
	_, err := produce.Request(context.Background(), parser(map[string]any{
		"timeout-ms": 2000,
	}))
	if err == nil {
		t.Error("expected an error for a block with no url")
	}
}

// TestHTTPRequestConsumerMissingURLErrors verifies a message composing to
// no url at all (neither the message nor the block sets one) surfaces a
// clear error on the error channel.
func TestHTTPRequestConsumerMissingURLErrors(t *testing.T) {
	c, err := consume.Request(context.Background(), parser(map[string]any{
		"timeout-ms": 2000,
	}))
	if err != nil {
		t.Fatalf("Request consumer: %v", err)
	}

	recv := make(chan []byte, 1)
	cerrs := make(chan error, 1)
	cdone := make(chan struct{})
	go c(t.Context(), recv, cerrs, cdone)

	recv <- []byte("{}")
	close(recv)

	select {
	case err := <-cerrs:
		if err == nil {
			t.Error("expected an error for a request with no url")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for error")
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

	recv <- []byte(`{"body":"trigger-error"}`)
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
