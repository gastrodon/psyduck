package integration

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
)

// TestSocketMetaRoundTrip exercises the unix-socket transport path that the
// socket→meta-producer pattern sits on: a listen producer reads newline-framed
// messages written by a socket consumer into the same socket. Every sent message
// must be received exactly once and the consumer must exit cleanly after its
// source closes.
func TestSocketMetaRoundTrip(t *testing.T) {
	sockPath := filepath.Join(t.TempDir(), "meta.sock")
	sock := "unix://" + sockPath

	lp, err := produce.Listen(parser(delimitCfg(sock, true)))
	if err != nil {
		t.Fatalf("listen producer: %v", err)
	}
	send := make(chan []byte)
	lerrs := make(chan error, 1)
	go lp(t.Context(), send, lerrs)
	drainErrs(lerrs)

	waitForSocket(t, sockPath)

	cw, err := consume.Socket(parser(delimitCfg(sock, false)))
	if err != nil {
		t.Fatalf("socket consumer: %v", err)
	}
	recv := make(chan []byte)
	cerrs := make(chan error, 1)
	cdone := make(chan struct{})
	go cw(t.Context(), recv, cerrs, cdone)
	drainErrs(cerrs)

	want := []string{
		`produce "constant" "a" { value = "1" }`,
		`produce "constant" "b" { value = "2" }`,
	}
	for _, m := range want {
		recv <- []byte(m)
	}
	close(recv)

	// Consumer must signal done after its source closes — verify no hang.
	select {
	case <-cdone:
	case <-time.After(3 * time.Second):
		t.Fatal("consumer did not close done after recv closed")
	}

	got := readN(t, send, len(want), 2*time.Second)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("msg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
