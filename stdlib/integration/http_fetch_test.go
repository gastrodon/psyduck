package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/transform"
)

// fetchParser builds an sdk.Parser over vals via the real JSON-tagged decode
// path (sdk.DecodeJSONTagged), rather than the reflection-based parser used
// elsewhere in this package: fetchConfig embeds transport.RequestConfig, and
// only the JSON path promotes an anonymous embedded struct's fields the way
// production decode (hclBlock.Decode, jsonBlock.Decode) does.
func fetchParser(vals map[string]any) sdk.Parser {
	return func(dst any) error {
		raw, err := json.Marshal(vals)
		if err != nil {
			return err
		}
		return sdk.DecodeJSONTagged(raw, dst)
	}
}

// TestHTTPFetchTemplatesPerMessage verifies the core round trip: each input
// message templates its own URL, the request actually goes out, and the
// response body comes back as the transformed message — one call per input,
// not a poll.
func TestHTTPFetchTemplatesPerMessage(t *testing.T) {
	var mu sync.Mutex
	var gotPaths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPaths = append(gotPaths, r.URL.Path)
		mu.Unlock()
		fmt.Fprintf(w, "hello %s", r.URL.Path[len("/items/"):])
	}))
	t.Cleanup(srv.Close)

	fn, err := transform.Fetch(context.Background(), fetchParser(map[string]any{
		"url":        srv.URL + "/items/{{.}}",
		"decode":     "bytes",
		"timeout-ms": 5000,
	}))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, 1)
	drainErrs(errs)

	go fn(t.Context(), in, out, errs)

	want := []string{"a", "b", "c"}
	go func() {
		for _, w := range want {
			in <- []byte(w)
		}
		close(in)
	}()

	got := readN(t, out, len(want), 5*time.Second)

	sortedWant := make([]string, len(want))
	for i, w := range want {
		sortedWant[i] = "hello " + w
	}
	assertSameSet(t, got, sortedWant)

	mu.Lock()
	paths := append([]string(nil), gotPaths...)
	mu.Unlock()
	if len(paths) != len(want) {
		t.Errorf("server saw %d requests, want %d", len(paths), len(want))
	}
}

// TestHTTPFetchParallelBoundsConcurrency verifies parallel controls how many
// requests are in flight at once: with parallel = 1, the server never sees a
// second request start before the first (held open) one is released.
func TestHTTPFetchParallelBoundsConcurrency(t *testing.T) {
	var inFlight int32
	var maxInFlight int32
	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if n <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, n) {
				break
			}
		}
		<-release
		atomic.AddInt32(&inFlight, -1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fn, err := transform.Fetch(context.Background(), fetchParser(map[string]any{
		"url":        srv.URL + "/",
		"decode":     "bytes",
		"parallel":   1,
		"timeout-ms": 5000,
	}))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, 1)
	drainErrs(errs)

	go fn(t.Context(), in, out, errs)

	go func() {
		in <- []byte("1")
		in <- []byte("2")
		close(in)
	}()

	// Give the first request time to land and hold, then release both.
	time.Sleep(200 * time.Millisecond)
	close(release)

	readN(t, out, 2, 5*time.Second)

	if got := atomic.LoadInt32(&maxInFlight); got != 1 {
		t.Errorf("max concurrent requests = %d, want 1 (parallel = 1)", got)
	}
}
