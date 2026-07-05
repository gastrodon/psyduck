package integration

import (
	"net"
	"net/http"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
)

// parser builds an sdk.Parser that populates a struct's psy-tagged fields from
// vals using reflection. It is the integration-test stand-in for the HCL config
// layer so tests stay independent of the parser package.
func parser(vals map[string]any) sdk.Parser {
	return func(dst any) error {
		rv := reflect.ValueOf(dst).Elem()
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			if tag := rt.Field(i).Tag.Get("psy"); tag != "" {
				if v, ok := vals[tag]; ok {
					rv.Field(i).Set(reflect.ValueOf(v))
				}
			}
		}
		return nil
	}
}

// delimitCfg returns a standard config map for stream transports using
// newline framing (sep="\n"). sep-byte=-1 signals "unset" to Delimit.Validate.
func delimitCfg(loc string, create bool) map[string]any {
	return map[string]any{
		"location": loc, "create": create,
		"sep": "\n", "sep-byte": -1, "sep-byte-index": 0, "group": 0,
	}
}

// fileCfg extends delimitCfg with the file-specific follow and append flags.
func fileCfg(loc string, follow bool) map[string]any {
	m := delimitCfg(loc, false)
	m["follow"] = follow
	m["append"] = false
	return m
}

// freePort binds to an ephemeral TCP port, captures its address, then closes
// the listener so the caller can re-bind to the same port. There is a narrow
// race window between close and re-bind; acceptable in tests on loopback.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// freeUDPAddr finds a free UDP port the same way.
func freeUDPAddr(t *testing.T) string {
	t.Helper()
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.LocalAddr().String()
	l.Close()
	return addr
}

// waitForSocket polls until the unix socket file appears on disk.
func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("unix socket never appeared")
}

// waitForTCP polls until a TCP connection can be established.
func waitForTCP(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("TCP server at %s never became ready", addr)
}

// waitForHTTP polls until the URL yields a non-error HTTP response (any status
// code is accepted — a 405 is still proof the server is listening).
func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("HTTP server at %s never became ready", url)
}

// readN collects exactly n messages from ch, failing if they don't arrive
// within timeout. It does not require ch to close.
func readN(t *testing.T, ch <-chan []byte, n int, timeout time.Duration) []string {
	t.Helper()
	out := make([]string, 0, n)
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(out) < n {
		select {
		case msg, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed early: got %d/%d messages: %v", len(out), n, out)
			}
			out = append(out, string(msg))
		case <-timer.C:
			t.Fatalf("timed out: got %d/%d messages: %v", len(out), n, out)
		}
	}
	return out
}

// drainChan collects all messages until ch closes, failing if it does not
// close within timeout. Use this to verify graceful producer exit.
func drainChan(t *testing.T, ch <-chan []byte, timeout time.Duration) []string {
	t.Helper()
	var out []string
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, string(msg))
		case <-timer.C:
			t.Fatalf("channel did not close within %v; collected %d messages so far", timeout, len(out))
			return nil
		}
	}
}

// assertNoDups fails if any message appears more than once.
func assertNoDups(t *testing.T, msgs []string) {
	t.Helper()
	seen := make(map[string]int, len(msgs))
	for _, m := range msgs {
		seen[m]++
	}
	for m, n := range seen {
		if n > 1 {
			t.Errorf("duplicate message %q appeared %d times", m, n)
		}
	}
}

// assertSameSet fails if got and want don't contain the same elements
// regardless of order.
func assertSameSet(t *testing.T, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if !reflect.DeepEqual(g, w) {
		t.Errorf("message set mismatch:\n got  %v\n want %v", g, w)
	}
}

// drainErrs discards all errors from ch so producer/consumer goroutines don't
// block on a full error channel during a test.
func drainErrs(ch <-chan error) { go func() { for range ch {} }() }
