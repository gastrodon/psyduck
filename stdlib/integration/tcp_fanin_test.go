package integration

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
)

// TestTCPFanIn exercises the queue-broker pattern over TCP: multiple concurrent
// socket consumers write to a single TCP listener. The test verifies:
//   - every message is delivered exactly once (no drops, no duplicates)
//   - each individual consumer exits cleanly after its source closes
//   - the test does not hang (all verifications run inside readN's timeout)
func TestTCPFanIn(t *testing.T) {
	const writers = 4
	const msgsPerWriter = 6
	const total = writers * msgsPerWriter

	addr := freePort(t)
	loc := "tcp://" + addr

	lp, err := produce.Listen(parser(delimitCfg(loc, false)))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	send := make(chan []byte, total)
	lerrs := make(chan error, 1)
	go lp(send, lerrs)
	drainErrs(lerrs)

	waitForTCP(t, addr)

	// Each writer goroutine sends msgsPerWriter unique messages then closes.
	// We wait for all consumers to signal done before asserting — this confirms
	// each consumer exited cleanly and all its messages were flushed.
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		w := w
		go func() {
			defer wg.Done()
			cw, err := consume.Socket(parser(delimitCfg(loc, false)))
			if err != nil {
				t.Errorf("writer %d: socket consumer: %v", w, err)
				return
			}
			recv := make(chan []byte, msgsPerWriter)
			cerrs := make(chan error, msgsPerWriter)
			cdone := make(chan struct{})
			go cw(recv, cerrs, cdone)
			drainErrs(cerrs)

			for m := 0; m < msgsPerWriter; m++ {
				recv <- []byte(fmt.Sprintf("w%d-m%d", w, m))
			}
			close(recv)

			select {
			case <-cdone:
			case <-time.After(5 * time.Second):
				t.Errorf("writer %d: consumer did not close done", w)
			}
		}()
	}
	wg.Wait()

	msgs := readN(t, send, total, 5*time.Second)
	assertNoDups(t, msgs)

	want := make([]string, 0, total)
	for w := 0; w < writers; w++ {
		for m := 0; m < msgsPerWriter; m++ {
			want = append(want, fmt.Sprintf("w%d-m%d", w, m))
		}
	}
	assertSameSet(t, msgs, want)
}

// TestTCPFanInSequential verifies fan-in correctness when writers connect and
// disconnect one at a time rather than all at once — the listener must keep
// accepting new connections and not close early.
func TestTCPFanInSequential(t *testing.T) {
	const rounds = 3
	const msgsPerRound = 4

	addr := freePort(t)
	loc := "tcp://" + addr

	lp, err := produce.Listen(parser(delimitCfg(loc, false)))
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	send := make(chan []byte, rounds*msgsPerRound)
	lerrs := make(chan error, 1)
	go lp(send, lerrs)
	drainErrs(lerrs)

	waitForTCP(t, addr)

	var allWant []string
	for r := 0; r < rounds; r++ {
		cw, err := consume.Socket(parser(delimitCfg(loc, false)))
		if err != nil {
			t.Fatalf("round %d: socket consumer: %v", r, err)
		}
		recv := make(chan []byte, msgsPerRound)
		cerrs := make(chan error, msgsPerRound)
		cdone := make(chan struct{})
		go cw(recv, cerrs, cdone)
		drainErrs(cerrs)

		for m := 0; m < msgsPerRound; m++ {
			msg := fmt.Sprintf("r%d-m%d", r, m)
			recv <- []byte(msg)
			allWant = append(allWant, msg)
		}
		close(recv)
		select {
		case <-cdone:
		case <-time.After(3 * time.Second):
			t.Fatalf("round %d: consumer did not close done", r)
		}
	}

	msgs := readN(t, send, len(allWant), 5*time.Second)
	assertNoDups(t, msgs)
	assertSameSet(t, msgs, allWant)
}
