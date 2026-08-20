package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/transform"
)

// TestHTTPFetchComposesFromMessage verifies the core round trip: each
// message is its own JSON request descriptor, dispatched, and the response
// body comes back as the transformed message.
func TestHTTPFetchComposesFromMessage(t *testing.T) {
	var mu sync.Mutex
	var gotPaths []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPaths = append(gotPaths, r.URL.Path)
		mu.Unlock()
		fmt.Fprintf(w, "hello %s", r.URL.Path[len("/items/"):])
	}))
	t.Cleanup(srv.Close)

	fn, err := transform.Fetch(context.Background(), parser(map[string]any{
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
			in <- []byte(fmt.Sprintf(`{"url":%q}`, srv.URL+"/items/"+w))
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

// TestHTTPFetchMessageFallsBackToBlock verifies Cp -> Ct fallback: a
// message setting only headers still gets url/method from the block, and
// its header merges in rather than replacing the block's.
func TestHTTPFetchMessageFallsBackToBlock(t *testing.T) {
	var gotAuth, gotExtra string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotExtra = r.Header.Get("X-Extra")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fn, err := transform.Fetch(context.Background(), parser(map[string]any{
		"url":        srv.URL,
		"headers":    map[string]string{"Authorization": "Bearer block-token"},
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
		in <- []byte(`{"headers":{"X-Extra":"per-message"}}`)
		close(in)
	}()

	readN(t, out, 1, 5*time.Second)

	if gotAuth != "Bearer block-token" {
		t.Errorf("Authorization = %q, want block's default to survive", gotAuth)
	}
	if gotExtra != "per-message" {
		t.Errorf("X-Extra = %q, want the message's own header", gotExtra)
	}
}

// TestHTTPFetchFollowRedirects verifies follow-redirects: true (the
// default) follows a 3xx to completion, false stops at the redirect
// response itself.
func TestHTTPFetchFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		fmt.Fprint(w, "landed")
	}))
	t.Cleanup(srv.Close)

	for _, tc := range []struct {
		name   string
		follow any
		codes  []int
	}{
		{name: "follows by default", follow: nil, codes: []int{200}},
		{name: "stops when disabled", follow: false, codes: []int{302}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vals := map[string]any{
				"url":           srv.URL + "/redirect",
				"success-codes": tc.codes,
				"timeout-ms":    5000,
			}
			if tc.follow != nil {
				vals["follow-redirects"] = tc.follow
			}
			fn, err := transform.Fetch(context.Background(), parser(vals))
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}

			in := make(chan []byte)
			out := make(chan []byte)
			errs := make(chan error, 1)
			go fn(t.Context(), in, out, errs)
			go func() {
				in <- []byte("{}")
				close(in)
			}()

			select {
			case got := <-out:
				followed := string(got) == "landed"
				wantFollowed := tc.follow == nil
				if followed != wantFollowed {
					t.Errorf("followed redirect = %v (body %q), want %v", followed, got, wantFollowed)
				}
			case err := <-errs:
				t.Fatalf("unexpected error: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for result")
			}
		})
	}
}

// TestHTTPFetchMissingURLErrors verifies that a message composing to no url
// at all (neither the message nor the block sets one) surfaces a clear
// error rather than an obscure transport failure.
func TestHTTPFetchMissingURLErrors(t *testing.T) {
	fn, err := transform.Fetch(context.Background(), parser(map[string]any{
		"timeout-ms": 2000,
	}))
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, 1)
	go fn(t.Context(), in, out, errs)
	go func() {
		in <- []byte("{}")
		close(in)
	}()

	select {
	case err := <-errs:
		if err == nil {
			t.Error("expected an error for a request with no url")
		}
	case <-out:
		t.Fatal("expected an error, got a result")
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for error")
	}
}
