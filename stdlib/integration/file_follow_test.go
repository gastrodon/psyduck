package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/produce"
)

// TestFileFollowTail verifies that a file producer with follow=true does not
// exit at EOF but continues emitting lines appended after it started. This
// models the `tail -f` pattern: the consumer is live before the writer and must
// still see every line the writer produces.
func TestFileFollowTail(t *testing.T) {
	const N = 8
	path := filepath.Join(t.TempDir(), "tail.log")

	// Pre-create the file so the producer can open it before any writes.
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := produce.File(parser(fileCfg(path, true)))
	if err != nil {
		t.Fatalf("File follow: %v", err)
	}
	send := make(chan []byte, N)
	perrs := make(chan error, 1)
	go p(send, perrs)
	drainErrs(perrs)

	// Append N lines with a delay that forces the tail reader to wake from its
	// sleep loop between some writes (tailReader polls every 200 ms at EOF).
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}

	want := make([]string, N)
	for i := 0; i < N; i++ {
		want[i] = fmt.Sprintf("line-%02d", i)
		fmt.Fprintln(f, want[i])
		time.Sleep(50 * time.Millisecond)
	}
	f.Close()

	msgs := readN(t, send, N, 5*time.Second)
	for i, w := range want {
		if msgs[i] != w {
			t.Errorf("msg[%d] = %q, want %q", i, msgs[i], w)
		}
	}
}

// TestFileFollowExistingContent verifies that a file producer with follow=true
// delivers lines already in the file before blocking for new ones — existing
// content and appended content both arrive.
func TestFileFollowExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.log")

	// Write some lines before the producer starts.
	initial := "pre-0\npre-1\npre-2\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := produce.File(parser(fileCfg(path, true)))
	if err != nil {
		t.Fatalf("File follow: %v", err)
	}
	send := make(chan []byte, 8)
	perrs := make(chan error, 1)
	go p(send, perrs)
	drainErrs(perrs)

	// Collect the 3 pre-existing lines first.
	preLines := readN(t, send, 3, 3*time.Second)
	for i, want := range []string{"pre-0", "pre-1", "pre-2"} {
		if preLines[i] != want {
			t.Errorf("pre-line[%d] = %q, want %q", i, preLines[i], want)
		}
	}

	// Append one more line and verify it also arrives.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(f, "appended")
	f.Close()

	post := readN(t, send, 1, 3*time.Second)
	if post[0] != "appended" {
		t.Errorf("appended line = %q, want %q", post[0], "appended")
	}
}
