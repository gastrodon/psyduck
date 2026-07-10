package flow

import (
	"context"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
)

func TestLimiter(t *testing.T) {
	// unlimited never blocks
	wait := Limiter(0, 0)
	start := time.Now()
	for i := 0; i < 1000; i++ {
		wait()
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("Limiter(0,0) blocked")
	}

	// 6000/min = 10ms period; 5 waits must take at least ~40ms
	wait = Limiter(6000, 0)
	start = time.Now()
	for i := 0; i < 5; i++ {
		wait()
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("Limiter(6000/min): 5 waits took only %s", elapsed)
	}
}

func TestProducer(t *testing.T) {
	emit := func(n int) sdk.Producer {
		return func(_ context.Context, send chan<- []byte, errs chan<- error) {
			for i := 0; i < n; i++ {
				send <- []byte{byte(i)}
			}
			close(send)
		}
	}
	recvAll := func(p sdk.Producer) [][]byte {
		send, errs := make(chan []byte), make(chan error)
		go p(t.Context(), send, errs)
		got := [][]byte{}
		for msg := range send {
			got = append(got, msg)
		}
		return got
	}

	// all limits unset: passthrough
	if got := recvAll(Producer(emit(10), 0, 0, 0)); len(got) != 10 {
		t.Fatalf("passthrough: want 10, got %d", len(got))
	}

	// stop-after cuts off exactly at n and closes send
	got := recvAll(Producer(emit(10), 0, 0, 3))
	if len(got) != 3 {
		t.Fatalf("stop-after: want 3, got %d", len(got))
	}
	if got[0][0] != 0 || got[2][0] != 2 {
		t.Fatalf("stop-after: wrong messages %v", got)
	}

	// stop-after larger than stream: everything arrives
	if got := recvAll(Producer(emit(4), 0, 0, 100)); len(got) != 4 {
		t.Fatalf("stop-after overshoot: want 4, got %d", len(got))
	}

	// per-minute paces messages: 6000/min = 10ms period, 5 msgs >= ~40ms
	start := time.Now()
	if got := recvAll(Producer(emit(5), 6000, 0, 0)); len(got) != 5 {
		t.Fatalf("per-minute: want 5, got %d", len(got))
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("per-minute: not throttled, took %s", elapsed)
	}
}

// A live-subscription-shaped producer: it sends until its ctx is
// cancelled, then reports via done that ctx.Done fired. Without the
// wrapper cancelling on stop-after, this loop would keep sending until
// the outer ctx ends.
func TestProducerCancelsInnerOnStopAfter(t *testing.T) {
	done := make(chan struct{})
	live := func(ctx context.Context, send chan<- []byte, _ chan<- error) {
		defer close(done)
		defer close(send)
		for {
			select {
			case send <- []byte{0}:
			case <-ctx.Done():
				return
			}
		}
	}

	send, errs := make(chan []byte), make(chan error)
	go Producer(live, 0, 0, 3)(t.Context(), send, errs)

	got := 0
	for range send {
		got++
	}
	if got != 3 {
		t.Fatalf("stop-after: delivered %d, want 3", got)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("inner producer never observed ctx cancel after cutoff")
	}
}

func TestConsumer(t *testing.T) {
	run := func(perMinute, stopAfter, feed int) int {
		count := 0
		consume := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for range recv {
				count++
			}
			close(done)
		}

		recv, errs, done := make(chan []byte), make(chan error), make(chan struct{})
		go Consumer(consume, perMinute, 0, stopAfter)(t.Context(), recv, errs, done)

		stop := make(chan struct{})
		go func() {
			defer close(recv)
			for i := 0; i < feed; i++ {
				select {
				case recv <- []byte{byte(i)}:
					continue
				case <-stop:
					return
				}
			}
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("consumer never signalled done")
		}
		close(stop)
		return count
	}

	if got := run(0, 0, 10); got != 10 {
		t.Fatalf("passthrough: want 10, got %d", got)
	}
	if got := run(0, 3, 10); got != 3 {
		t.Fatalf("stop-after: want 3, got %d", got)
	}
}

// A consumer that would block on a slow external call unless its ctx
// cancels: it signals ctx observation via ctxCancelled. Without the
// wrapper cancelling on stop-after, this call would hang past cutoff.
func TestConsumerCancelsInnerOnStopAfter(t *testing.T) {
	ctxCancelled := make(chan struct{})
	consume := func(ctx context.Context, recv <-chan []byte, _ chan<- error, done chan<- struct{}) {
		defer close(done)
		for {
			select {
			case _, ok := <-recv:
				if !ok {
					return
				}
			case <-ctx.Done():
				close(ctxCancelled)
				return
			}
		}
	}

	recv, errs, done := make(chan []byte), make(chan error), make(chan struct{})
	go Consumer(consume, 0, 0, 3)(t.Context(), recv, errs, done)

	go func() {
		defer close(recv)
		for i := 0; i < 10; i++ {
			select {
			case recv <- []byte{byte(i)}:
			case <-done:
				return
			}
		}
	}()

	select {
	case <-ctxCancelled:
	case <-time.After(time.Second):
		t.Fatal("inner consumer never observed ctx cancel after cutoff")
	}
	<-done
}

func TestGates(t *testing.T) {
	count := func(fn sdk.Transformer, n int) int {
		passed := 0
		for i := 0; i < n; i++ {
			out, err := fn([]byte("x"))
			if err != nil {
				t.Fatal(err)
			}
			if out != nil {
				passed++
			}
		}
		return passed
	}

	if got := count(Head(2), 5); got != 2 {
		t.Errorf("head passed %d, want 2", got)
	}
	if got := count(Tail(3), 5); got != 2 {
		t.Errorf("tail passed %d, want 2", got)
	}
	if got := count(Sample(2), 4); got != 2 {
		t.Errorf("sample kept %d, want 2", got)
	}
}
