package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
)

// TestFileBroadcast verifies fan-out broadcast semantics: two independent file
// producers reading the same path each receive every message — reads are
// non-destructive, so the file acts as a broadcast log. It also verifies
// graceful exit: both producers must close their send channels once the file
// is fully consumed (drainChan waits for close, so a hang is a test failure).
func TestFileBroadcast(t *testing.T) {
	const N = 10
	path := filepath.Join(t.TempDir(), "source.txt")

	var buf bytes.Buffer
	want := make([]string, N)
	for i := 0; i < N; i++ {
		want[i] = fmt.Sprintf("msg-%02d", i)
		fmt.Fprintln(&buf, want[i])
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	newReader := func() <-chan []byte {
		p, err := produce.File(context.Background(), parser(fileCfg(path, false)))
		if err != nil {
			t.Fatalf("File producer: %v", err)
		}
		send := make(chan []byte)
		perrs := make(chan error, 1)
		go p(t.Context(), send, perrs)
		drainErrs(perrs)
		return send
	}

	send1 := newReader()
	send2 := newReader()

	// Drain both channels concurrently; drainChan blocks until each closes,
	// verifying graceful exit without a hang.
	var (
		got1, got2 []string
		wg         sync.WaitGroup
	)
	wg.Add(2)
	go func() { defer wg.Done(); got1 = drainChan(t, send1, 5*time.Second) }()
	go func() { defer wg.Done(); got2 = drainChan(t, send2, 5*time.Second) }()
	wg.Wait()

	// Both readers must have received every message.
	assertSameSet(t, got1, want)
	assertSameSet(t, got2, want)
	assertNoDups(t, got1)
	assertNoDups(t, got2)
}

// TestUnixSocketFanIn complements TestTCPFanIn using unix-domain sockets and a
// higher message volume, verifying that the fan-in transport works across socket
// families and that no messages are dropped under larger loads.
func TestUnixSocketFanIn(t *testing.T) {
	const writers = 3
	const msgsPerWriter = 20
	const total = writers * msgsPerWriter

	sockPath := filepath.Join(t.TempDir(), "fanin.sock")
	loc := "unix://" + sockPath

	lp, err := produce.Listen(context.Background(), parser(delimitCfg(loc, true)))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	send := make(chan []byte, total)
	lerrs := make(chan error, 1)
	go lp(t.Context(), send, lerrs)
	drainErrs(lerrs)

	waitForSocket(t, sockPath)

	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		w := w
		go func() {
			defer wg.Done()
			sw, err := consume.Socket(context.Background(), parser(delimitCfg(loc, false)))
			if err != nil {
				t.Errorf("writer %d: %v", w, err)
				return
			}
			recv := make(chan []byte, msgsPerWriter)
			cerrs := make(chan error, msgsPerWriter)
			cdone := make(chan struct{})
			go sw(t.Context(), recv, cerrs, cdone)
			drainErrs(cerrs)

			for m := 0; m < msgsPerWriter; m++ {
				recv <- []byte(fmt.Sprintf("w%d-m%02d", w, m))
			}
			close(recv)
			select {
			case <-cdone:
			case <-time.After(5 * time.Second):
				t.Errorf("writer %d: consumer hung", w)
			}
		}()
	}
	wg.Wait()

	msgs := readN(t, send, total, 5*time.Second)
	assertNoDups(t, msgs)

	want := make([]string, 0, total)
	for w := 0; w < writers; w++ {
		for m := 0; m < msgsPerWriter; m++ {
			want = append(want, fmt.Sprintf("w%d-m%02d", w, m))
		}
	}
	assertSameSet(t, msgs, want)
}
