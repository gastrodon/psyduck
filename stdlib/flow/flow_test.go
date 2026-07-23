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

// A producer that buffers a final value in flight when stop-after is hit.
// Without proper stop() discipline (joining the inner producer), this late
// value races teardown and is lost. With stop(), the wrapper waits for the
// inner producer to finish before returning, so the final flush is delivered.
func TestProducerJoinsInnerOnStopAfter(t *testing.T) {
	innerFinished := make(chan struct{})
	buffered := func(ctx context.Context, send chan<- []byte, _ chan<- error) {
		defer close(innerFinished)
		for i := 0; i < 5; i++ {
			select {
			case send <- []byte{byte(i)}:
			case <-ctx.Done():
				return
			}
		}
	}

	send, errs := make(chan []byte), make(chan error)
	go Producer(buffered, 0, 0, 3)(t.Context(), send, errs)

	got := []byte{}
	for msg := range send {
		got = append(got, msg...)
	}

	// Expect exactly the first 3 messages (stop-after = 3).
	if len(got) != 3 {
		t.Fatalf("stop-after: delivered %d messages, want 3", len(got))
	}
	for i, b := range got {
		if b != byte(i) {
			t.Fatalf("message %d: got %d, want %d", i, b, i)
		}
	}

	// The inner producer should have finished by the time the wrapper returned.
	// If it races teardown instead, this would time out.
	select {
	case <-innerFinished:
	case <-time.After(time.Second):
		t.Fatal("inner producer never finished after wrapper returned")
	}
}

func TestConsumer(t *testing.T) {
	run := func(perMinute, feed int) (int, time.Duration) {
		count := 0
		consume := func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
			for range recv {
				count++
			}
			close(done)
		}

		recv, errs, done := make(chan []byte), make(chan error), make(chan struct{})
		go Consumer(consume, perMinute, 0)(t.Context(), recv, errs, done)

		start := time.Now()
		go func() {
			defer close(recv)
			for i := 0; i < feed; i++ {
				recv <- []byte{byte(i)}
			}
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("consumer never signalled done")
		}
		return count, time.Since(start)
	}

	if got, _ := run(0, 10); got != 10 {
		t.Fatalf("passthrough: want 10, got %d", got)
	}

	// per-minute paces messages: 6000/min = 10ms period, 5 msgs >= ~40ms
	if got, elapsed := run(6000, 5); got != 5 {
		t.Fatalf("per-minute: want 5, got %d", got)
	} else if elapsed < 40*time.Millisecond {
		t.Fatalf("per-minute: not throttled, took %s", elapsed)
	}
}

// A clean end of stream must be delivered to the inner consumer as a clean
// end of stream. The sdk contract distinguishes recv closing (upstream done:
// flush buffered work, finish up) from ctx cancellation (abandonment: stop
// now). Consumer's deferred cancel() runs before its deferred close(inner)
// (LIFO), so on a clean upstream close the inner consumer observes ctx.Done()
// before — or racing — the close of its recv, and takes the abandonment path.
// Any buffering consumer wrapped with a rate limit loses its final flush on
// every clean pipeline shutdown. This is a live bug, not a fixed regression:
// this test FAILS at the commit that introduces it.
func TestConsumerCleanEndIsNotAbandonment(t *testing.T) {
	for i := 0; i < 20; i++ {
		path := make(chan string, 1)
		inner := func(ctx context.Context, recv <-chan []byte, _ chan<- error, done chan<- struct{}) {
			defer close(done)
			for {
				select {
				case _, ok := <-recv:
					if !ok {
						path <- "clean" // where a final flush would happen
						return
					}
				case <-ctx.Done():
					path <- "cancelled" // buffered work dropped
					return
				}
			}
		}

		recv, errs, done := make(chan []byte), make(chan error), make(chan struct{})
		go Consumer(inner, 0, 1000000)(t.Context(), recv, errs, done)

		recv <- []byte{0}
		close(recv)

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("inner consumer never finished")
		}
		if got := <-path; got != "clean" {
			t.Fatalf("run %d: clean upstream close delivered to inner consumer as %s", i, got)
		}
	}
}

// A consumer that reads one message then blocks doing simulated external
// work — the realistic "consumer is mid-fetch when upstream ends" scenario.
// Only ctx.Done can unblock it: without the wrapper cancelling its ctx on
// exit, close(inner) alone doesn't help (the consumer isn't waiting on
// inner anymore), and the test times out.
func TestConsumerCancelsInnerOnRecvClose(t *testing.T) {
	ctxCancelled := make(chan struct{})
	consume := func(ctx context.Context, recv <-chan []byte, _ chan<- error, done chan<- struct{}) {
		defer close(done)
		<-recv // accept one message, then park in external work
		select {
		case <-ctx.Done():
			close(ctxCancelled)
		case <-time.After(2 * time.Second):
		}
	}

	recv, errs, done := make(chan []byte), make(chan error), make(chan struct{})
	go Consumer(consume, 0, 1000000)(t.Context(), recv, errs, done)

	recv <- []byte{0}
	close(recv)

	select {
	case <-ctxCancelled:
	case <-time.After(time.Second):
		t.Fatal("inner consumer never observed ctx cancel after recv close")
	}
	<-done
}

// runTransformer feeds inputs through tx synchronously and collects
// whatever it emits, closing in and waiting for out to close. A hang is a
// test failure, not a timeout to tolerate.
func runTransformer(t *testing.T, tx sdk.Transformer, inputs [][]byte) [][]byte {
	t.Helper()
	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, len(inputs))
	done := make(chan struct{})

	go func() {
		defer close(done)
		tx(t.Context(), in, out, errs)
	}()
	go func() {
		defer close(in)
		for _, msg := range inputs {
			in <- msg
		}
	}()

	var got [][]byte
	for msg := range out {
		got = append(got, msg)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transformer did not finish: hung after closing out")
	}
	return got
}

func TestGates(t *testing.T) {
	count := func(tx sdk.Transformer, n int) int {
		inputs := make([][]byte, n)
		for i := range inputs {
			inputs[i] = []byte("x")
		}
		return len(runTransformer(t, tx, inputs))
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

// TestGatesCancelRelease checks the send-side ctx.Done branch each flow gate
// grew in the channel-contract rewrite: a gate parked trying to emit must
// return when ctx is cancelled instead of leaking. runTransformer only ever
// exercises the clean close-of-in path.
func TestGatesCancelRelease(t *testing.T) {
	gates := map[string]sdk.Transformer{
		"wait":     Wait(0),
		"throttle": Throttle(1000),
		"head":     Head(10),
		"tail":     Tail(0),
		"sample":   Sample(1),
	}
	for name, tx := range gates {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			in := make(chan []byte)
			out := make(chan []byte) // never drained
			errs := make(chan error, 1)
			done := make(chan struct{})

			go func() {
				defer close(done)
				tx(ctx, in, out, errs)
			}()
			in <- []byte("x") // accepted; gate then blocks trying to emit
			cancel()

			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("gate did not return after ctx cancel: parked on blocked send")
			}
		})
	}
}
