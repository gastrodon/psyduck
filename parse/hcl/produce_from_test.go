package hcl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
)

func TestParseProduceFromEnv(t *testing.T) {
	// remote config may query env vars unseen in local sources
	t.Setenv("PSYDUCK_REMOTE_ONLY", "from-remote-env")
	seed := sdk.NewInProc("meta",
		&sdk.Resource{
			Name:  "seed",
			Kinds: sdk.PRODUCER,
			ProvideProducer: func(_ context.Context, _ sdk.Parser) (sdk.Producer, error) {
				return func(_ context.Context, send chan<- []byte, errs chan<- error) {
					send <- []byte(`produce "constant" "remote" { value = env.PSYDUCK_REMOTE_ONLY }`)
					close(send)
				}, nil
			},
		},
	)

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	opts := new(constantOpts)
	if err := producers[0].Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-remote-env" {
		t.Fatalf("remote env not resolved: %q", opts.Value)
	}
}

func TestParseProduceFrom(t *testing.T) {
	// a producer whose single message is itself psyduck config
	meta := sdk.NewInProc("meta",
		&sdk.Resource{
			Name:  "seed",
			Kinds: sdk.PRODUCER,
			ProvideProducer: func(_ context.Context, _ sdk.Parser) (sdk.Producer, error) {
				return func(_ context.Context, send chan<- []byte, errs chan<- error) {
					send <- []byte(`
					produce "constant" "remote" {
						value = "from-remote"
					}
					`)
					close(send)
				}, nil
			},
		},
	)

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), meta})
	if err != nil {
		t.Fatal(err)
	}

	pipe := result["main"]

	producers := drainAll(t, pipe.Producers)
	if len(producers) != 1 {
		t.Fatalf("want 1 remote producer, got %d", len(producers))
	}

	b := producers[0]
	if b.PluginID != "test" || b.Resource.Name != "constant" {
		t.Fatalf("bad remote binding: %#v", b)
	}
	if !strings.HasPrefix(b.Block.Origin().SourceName, "remote://") {
		t.Fatalf("bad remote origin: %s", b.Block.Origin())
	}

	opts := new(constantOpts)
	if err := b.Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-remote" {
		t.Fatalf("bad remote value: %q", opts.Value)
	}
}

// seedPlugin builds a produce-from seed plugin around the given producer.
func seedPlugin(p sdk.Producer) sdk.Plugin {
	return sdk.NewInProc("meta",
		&sdk.Resource{
			Name:            "seed",
			Kinds:           sdk.PRODUCER,
			ProvideProducer: func(_ context.Context, _ sdk.Parser) (sdk.Producer, error) { return p, nil },
		},
	)
}

const seedEntry = `
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`

// Regression for #8: a seed that closes without sending used to read as an
// empty remote config, surfacing much later as "pipeline has no producers".
func TestParseProduceFromClosedSeed(t *testing.T) {
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		close(send)
		close(errs)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	_, err = result["main"].Producers(t.Context(), 4)
	if err == nil || !strings.Contains(err.Error(), "closed without sending") {
		t.Fatalf("want closed-without-sending error, got %v", err)
	}
}

// Draining a produce-from stream is bounded by the caller's ctx, not only
// by the builtin timeout. The seed honors ctx like a well-behaved plugin
// should — it's the stream's own ctx.Done() handling under test here, not
// resilience against a non-cooperating plugin (that's core's job, see
// core/regression_test.go).
func TestParseProduceFromCancel(t *testing.T) {
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		<-ctx.Done() // never sends
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err = result["main"].Producers(ctx, 4)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline error, got %v", err)
	}
}

// The produce-from ResourceFunc holds unsynchronized state and is safe only
// because the run-time feeder drives it from one goroutine. It is not
// reentrant: a second concurrent call must panic rather than race the state.
// Here one pull parks inside the stream (holding the guard) while a second
// races in and is refused.
func TestParseProduceFromNotReentrant(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		close(entered) // the first pull is now parked waiting for a message
		<-release
		close(send)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}
	produce := result["main"].Producers

	// First pull: parks waiting for the seed's first message, holding the
	// reentrancy guard. It unwinds once we release the seed at the end.
	go produce(t.Context(), 4)
	<-entered

	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		produce(t.Context(), 4)
	}()

	got := <-panicked
	if got == nil {
		t.Fatal("want a panic on a concurrent (reentrant) call, got none")
	}
	if msg, _ := got.(string); !strings.Contains(msg, "not reentrant") {
		t.Fatalf("want a not-reentrant panic, got %v", got)
	}
	close(release)
}

func TestParseProduceFromStream(t *testing.T) {
	// a seed that emits multiple messages, each defining new produce
	// blocks. Every message should surface on the Producers stream.
	values := []string{"one", "two", "three"}
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		for _, v := range values {
			send <- fmt.Appendf(nil, `produce "constant" "remote" { value = %q }`, v)
		}
		close(send)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	if len(producers) != len(values) {
		t.Fatalf("want %d streamed remote producers, got %d", len(values), len(producers))
	}
	for i, b := range producers {
		opts := new(constantOpts)
		if err := b.Block.Decode(opts); err != nil {
			t.Fatalf("producer %d decode: %s", i, err)
		}
		if opts.Value != values[i] {
			t.Fatalf("producer %d value: got %q, want %q", i, opts.Value, values[i])
		}
	}
}

// A remote message is a self-contained config unit: it declares and uses its
// own locals {} in one byte span, exactly like a .psy file, and non-produce
// blocks are inert (warned, not fatal) rather than tearing the stream down.
func TestParseProduceFromSelfContained(t *testing.T) {
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		send <- []byte(`
		locals { v = "from-remote-local" }
		consume "trash" "ignored" {}
		produce "constant" "remote" { value = local.v }
		`)
		close(send)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	if len(producers) != 1 {
		t.Fatalf("want 1 remote producer (consume inert), got %d", len(producers))
	}
	opts := new(constantOpts)
	if err := producers[0].Block.Decode(opts); err != nil {
		t.Fatal(err)
	}
	if opts.Value != "from-remote-local" {
		t.Fatalf("remote local not resolved against the message's own locals: %q", opts.Value)
	}
}

// A remote unit has no access to the host file's scope: a message that reads
// local.* the host declares must fail to evaluate, not silently borrow it.
func TestParseProduceFromNoHostLocalLeak(t *testing.T) {
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		send <- []byte(`produce "constant" "remote" { value = local.hostonly }`)
		close(send)
	})

	entry, load := src(`
	locals { hostonly = "H" }
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from = produce.seed.s
		consume      = [trash.t]
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := result["main"].Producers(t.Context(), 4); err == nil {
		t.Fatal("want an error: the host file's local.* must not leak into a remote unit")
	}
}

// Messages that declare no producers are skipped, not treated as the
// stream's first delivery — the bindings from a later message still arrive.
func TestParseProduceFromEmptyMessage(t *testing.T) {
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		send <- []byte(`# nothing declared`)
		send <- []byte(`produce "constant" "remote" { value = "real" }`)
		close(send)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	if len(producers) != 1 {
		t.Fatalf("want 1 remote producer, got %d", len(producers))
	}
}

// Calling the stream with max < 1 releases it: the seed producer is
// stopped and later drains observe exhaustion.
func TestParseProduceFromRelease(t *testing.T) {
	stopped := make(chan struct{})
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		select {
		case send <- []byte(`produce "constant" "remote" { value = "x" }`):
		case <-ctx.Done():
			close(stopped)
			return
		}
		<-ctx.Done()
		close(stopped)
	})

	entry, load := src(seedEntry)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	stream := result["main"].Producers
	chunk, err := stream(t.Context(), 4)
	if err != nil || len(chunk) != 1 {
		t.Fatalf("first drain: got %d resources, err %v", len(chunk), err)
	}

	if _, err := stream(t.Context(), 0); err != nil {
		t.Fatalf("release: %s", err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("seed producer not stopped by release")
	}

	chunk, err = stream(t.Context(), 4)
	if err != nil || chunk != nil {
		t.Fatalf("drain after release: got %v, err %v; want exhaustion", chunk, err)
	}
}

// produce-from has no fixed producer count, so produce-parallel = 0 there is
// meaningless and rejected at parse.
func TestParseProduceParallelZeroRemote(t *testing.T) {
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) { close(send) })
	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from     = produce.seed.s
		consume          = [trash.t]
		produce-parallel = 0
	}
	`)
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err == nil || !strings.Contains(err.Error(), "requires a static produce list") {
		t.Fatalf("want produce-from rejection of 0, got: %v", err)
	}
}

func TestParseProduceFromTimeout(t *testing.T) {
	// a seed that never sends should trip the configured
	// produce-from-timeout, rather than the 10-second default.
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		<-ctx.Done() // never sends
	})

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = 1
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	_, err = result["main"].Producers(t.Context(), 4)
	if err == nil || !strings.Contains(err.Error(), "timeout waiting for remote producer") {
		t.Fatalf("want timeout error, got: %v", err)
	}
}

func TestParseProduceFromTimeoutZero(t *testing.T) {
	// produce-from-timeout = 0 disables the first-message timeout entirely.
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) {
		time.Sleep(1200 * time.Millisecond)
		send <- []byte(`produce "constant" "remote" { value = "late" }`)
		close(send)
	})

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = 0
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	producers := drainAll(t, result["main"].Producers)
	if len(producers) != 1 {
		t.Fatalf("want 1 remote producer, got %d", len(producers))
	}
}

// With produce-from-timeout = 0 and a seed that never sends, the drain must
// block indefinitely on the channel — only the caller's ctx ends it. The
// error is the ctx cancellation, never "timeout waiting for remote producer":
// a zero timeout arms no timer at all (resource.go: nil deadline channel).
func TestParseProduceFromTimeoutZeroWaitsForever(t *testing.T) {
	seed := seedPlugin(func(ctx context.Context, send chan<- []byte, errs chan<- error) {
		<-ctx.Done() // never sends
	})

	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = 0
	}
	`)
	result, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = result["main"].Producers(ctx, 4)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want ctx deadline to end the wait, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "timeout waiting for remote producer") {
		t.Fatalf("timeout=0 must arm no timer, got a timeout error: %v", err)
	}
}

// LIVE BUG (discovered by QA audit — this test FAILS at the commit that
// introduces it): a produce-from-timeout too large for time.Duration must be
// rejected at parse, like a negative one is. Today resource.go takes
// AsBigFloat().Int64(), which saturates at MaxInt64 for huge values, and only
// then multiplies by time.Second — the multiplication overflows and wraps
// negative (MaxInt64 seconds ≈ -1s as a Duration). That slips past the
// `secs < 0` check (which runs before the multiplication), and a negative
// timeout arms no timer at all in runSeed (`if timeout > 0`): the user asked
// for a huge-but-finite timeout and silently got "wait forever".
func TestParseProduceFromTimeoutOverflow(t *testing.T) {
	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = 10000000000000000000
	}
	`)
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) { close(send) })
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err == nil || !strings.Contains(err.Error(), "produce-from-timeout") {
		t.Fatalf("want produce-from-timeout overflow rejection, got: %v", err)
	}
}

func TestParseProduceFromTimeoutNegative(t *testing.T) {
	entry, load := src(`
	produce "seed" "s" {}
	consume "trash" "t" {}
	pipeline "main" {
		produce-from         = produce.seed.s
		consume              = [trash.t]
		produce-from-timeout = -1
	}
	`)
	seed := seedPlugin(func(_ context.Context, send chan<- []byte, errs chan<- error) { close(send) })
	_, err := NewParserHCL().Parse(t.Context(), entry, load, []sdk.Plugin{testPlugin("test"), seed})
	if err == nil || !strings.Contains(err.Error(), "produce-from-timeout") {
		t.Fatalf("want produce-from-timeout error, got: %v", err)
	}
}
