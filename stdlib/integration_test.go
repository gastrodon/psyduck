package stdlib

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
)

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

func delimit(loc string, create bool) map[string]any {
	return map[string]any{
		"location": loc, "create": create,
		"sep": "\n", "sep-byte": -1, "sep-byte-index": 0, "group": 0,
	}
}

// TestSocketMetaRoundTrip exercises the transport path behind the
// socket→meta-producer use case: a listen producer reads newline-framed
// messages that a socket consumer writes into the same unix socket. This is
// the fan-in a produce-from meta-producer sits on top of.
func TestSocketMetaRoundTrip(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "meta.sock")
	sock := "unix://" + sockPath

	lp, err := produce.Listen(parser(delimit(sock, true)))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	send, lerrs := make(chan []byte), make(chan error)
	go lp(send, lerrs)
	go func() {
		for range lerrs {
		}
	}()

	// Wait for the listener to bind before dialing.
	waitForSocket(t, sockPath)

	cw, err := consume.Socket(parser(delimit(sock, false)))
	if err != nil {
		t.Fatalf("consume socket: %v", err)
	}
	recv, cerrs, cdone := make(chan []byte), make(chan error), make(chan struct{})
	go cw(recv, cerrs, cdone)
	go func() {
		for range cerrs {
		}
	}()

	want := []string{
		`produce "constant" "a" { value = "1" }`,
		`produce "constant" "b" { value = "2" }`,
	}
	for _, m := range want {
		recv <- []byte(m)
	}
	close(recv)
	<-cdone

	got := readN(t, send, len(want))
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("socket never appeared")
}

func readN(t *testing.T, ch <-chan []byte, n int) []string {
	t.Helper()
	out := make([]string, 0, n)
	timeout := time.After(2 * time.Second)
	for len(out) < n {
		select {
		case msg := <-ch:
			out = append(out, string(msg))
		case <-timeout:
			t.Fatalf("timed out after %d/%d messages: %v", len(out), n, out)
		}
	}
	return out
}
