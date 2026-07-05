package integration

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/produce"
)

// TestHTTPWebhookReceiver exercises http-listen as an inbound webhook endpoint:
// N concurrent POST requests must all be received exactly once, the configured
// reply status must be returned to the caller, and no request may hang.
func TestHTTPWebhookReceiver(t *testing.T) {
	const N = 12
	addr := freePort(t)

	p, err := produce.HTTPListen(parser(map[string]any{
		"address": addr,
		"path":    "/hook",
		"method":  "POST",
		"status":  202,
		"reply":   "accepted",
	}))
	if err != nil {
		t.Fatalf("HTTPListen: %v", err)
	}

	// Buffered so handlers don't block on the send channel while concurrent
	// requests are in flight.
	send := make(chan []byte, N)
	errs := make(chan error, N)
	go p(send, errs)
	drainErrs(errs)

	waitForHTTP(t, "http://"+addr+"/hook")

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		i := i
		go func() {
			defer wg.Done()
			body := fmt.Sprintf(`{"event":%d}`, i)
			resp, err := http.Post("http://"+addr+"/hook", "application/json", strings.NewReader(body))
			if err != nil {
				t.Errorf("POST %d: %v", i, err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 202 {
				t.Errorf("POST %d: want status 202, got %d", i, resp.StatusCode)
			}
		}()
	}
	wg.Wait()

	msgs := readN(t, send, N, 5*time.Second)
	assertNoDups(t, msgs)
	if len(msgs) != N {
		t.Errorf("got %d messages, want %d", len(msgs), N)
	}
}

// TestHTTPWebhookMethodGating verifies that the http-listen method filter
// rejects requests with the wrong verb and does not forward their bodies.
func TestHTTPWebhookMethodGating(t *testing.T) {
	addr := freePort(t)

	p, err := produce.HTTPListen(parser(map[string]any{
		"address": addr,
		"path":    "/ingest",
		"method":  "POST",
		"status":  200,
		"reply":   "",
	}))
	if err != nil {
		t.Fatalf("HTTPListen: %v", err)
	}
	send := make(chan []byte, 4)
	errs := make(chan error, 1)
	go p(send, errs)
	drainErrs(errs)

	waitForHTTP(t, "http://"+addr+"/ingest")

	// A GET to the POST-only endpoint must be rejected.
	resp, err := http.Get("http://" + addr + "/ingest")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET want 405, got %d", resp.StatusCode)
	}

	// A POST must be accepted; verify the message lands.
	resp, err = http.Post("http://"+addr+"/ingest", "text/plain", strings.NewReader("ok"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("POST want 200, got %d", resp.StatusCode)
	}

	msgs := readN(t, send, 1, 2*time.Second)
	if string(msgs[0]) != "ok" {
		t.Errorf("body = %q, want %q", msgs[0], "ok")
	}
	// send should still have exactly 1 message (the GET was rejected).
	select {
	case extra := <-send:
		t.Errorf("unexpected extra message %q (GET body should have been dropped)", extra)
	default:
	}
}
