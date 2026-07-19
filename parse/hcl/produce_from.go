package hcl

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/psyduck-etl/sdk"
	"github.com/zclconf/go-cty/cty"

	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/stdlib/flow"
)

// remoteFirstMessageTimeout guards how long we wait for a seed producer to
// emit its first message. After the first message arrives the seed is
// considered alive and may stay quiet between messages for arbitrarily
// long — a socket-listener seed, for example. This is the default used
// when a pipeline doesn't set produce-from-timeout; the attribute (in
// seconds, 0 meaning wait indefinitely) overrides it per-pipeline.
const remoteFirstMessageTimeout = 10 * time.Second

// seedResult is one delivery from the seed producer: a parsed message's
// bindings (possibly empty), or an error terminating the stream.
type seedResult struct {
	bindings []parse.Resource
	err      error
}

// remoteBindings hides a dynamic producer behind the ordinary Bindings
// stream: the first drain binds and runs the seed producer, and every
// message the seed sends is parsed as HCL produce blocks and yielded as
// bindings. Core cannot tell these apart from literal producers, except
// that a call may block for as long as the seed stays quiet — waiting is
// bounded by the call's ctx, so callers that cannot wait must bring a
// deadline.
//
// The seed keeps running until it closes its send channel (natural
// exhaustion), fails, or the stream is torn down. The seed's lifetime is
// anchored to the ctx of the call that starts it (the run-time feeder's, in
// practice — see core.producerSource); any terminal return — exhaustion,
// error, a dead per-call ctx, or an explicit max < 1 release — stops the
// seed and joins its goroutine before returning, so no path leaves it
// running unobserved.
func remoteBindings(seed parse.Resource, ix *resourceIndex, timeout time.Duration, warn func(string)) parse.ResourceFunc {
	var (
		results chan seedResult
		stop    context.CancelFunc
		buf     []parse.Resource
		done    bool
		gotAny  bool // at least one message arrived; see closed-without-sending below
	)

	// The state above is captured by reference and mutated without locks: it
	// is safe only because the run-time feeder (core.producerSource) is the
	// sole caller and drives every pull from one goroutine. This is a plain
	// func, though, so nothing structurally enforces that. inflight makes the
	// single-caller contract executable — a concurrent call would be a data
	// race on buf/done/results, and a serialized-but-interleaved one would
	// scramble the ordered chunk stream, so we refuse it loudly rather than
	// corrupt state silently.
	var inflight atomic.Bool

	// finish tears the stream down: cancel the seed's ctx and join runSeed
	// by draining results until it closes. Idempotent; a no-op if the seed
	// never started.
	finish := func() {
		done = true
		if stop == nil {
			return
		}
		stop()
		for range results {
		}
		stop = nil
	}

	return func(ctx context.Context, max int) ([]parse.Resource, error) {
		if !inflight.CompareAndSwap(false, true) {
			panic(fmt.Sprintf("produce-from %s: ResourceFunc is not reentrant — called concurrently", seed.Ref))
		}
		defer inflight.Store(false)

		if max < 1 {
			finish()
			buf = nil
			return nil, nil
		}
		if done {
			if n := min(max, len(buf)); n > 0 {
				chunk := buf[:n]
				buf = buf[n:]
				return chunk, nil
			}
			return nil, nil
		}
		if err := ctx.Err(); err != nil {
			finish()
			return nil, fmt.Errorf("produce-from %s: waiting for remote producer: %w", seed.Ref, err)
		}

		if stop == nil {
			// first drain: the seed runs under its own ctx derived from this
			// call's, living across calls until finish or that ctx ends
			var seedCtx context.Context
			seedCtx, stop = context.WithCancel(ctx)
			results = make(chan seedResult)
			go runSeed(seedCtx, seed, ix, timeout, warn, results)
		}

		for len(buf) == 0 {
			select {
			case <-ctx.Done():
				finish()
				return nil, fmt.Errorf("produce-from %s: waiting for remote producer: %w", seed.Ref, ctx.Err())
			case r, ok := <-results:
				if !ok {
					// Regression guard for #8: a seed that closes without
					// ever sending is an error, not an empty config.
					if !gotAny {
						finish()
						return nil, fmt.Errorf("produce-from %s: seed producer closed without sending", seed.Ref)
					}
					finish()
					return nil, nil
				}
				if r.err != nil {
					finish()
					return nil, r.err
				}
				gotAny = true
				buf = append(buf, r.bindings...)
			}
		}

		n := min(max, len(buf))
		chunk := buf[:n]
		buf = buf[n:]
		return chunk, nil
	}
}

// runSeed binds and runs the seed producer, parses every message it sends
// into produce bindings, and delivers them on out. It exits on ctx, on the
// first error, or when the seed closes its send channel — errors the seed
// emits between closing send and returning are still delivered, matching
// the engine's guarantee in core. Closes out on exit so the caller
// observes EOS. timeout guards the wait for the first message that
// actually declares producers — messages declaring none are skipped and
// leave the guard armed; 0 disables the guard. As everywhere else, a seed
// that ignores ctx can leak its own goroutine — but never runSeed itself,
// whose every send and receive also selects on ctx.
//
// The seed is a producer like any other, so its own host-owned meta
// (per-minute, stop-after) applies here via the same flow.Producer gate
// core's ordinary producer path uses — a listener that never exhausts on
// its own still respects a stop-after bound on the seed block.
func runSeed(ctx context.Context, seed parse.Resource, ix *resourceIndex, timeout time.Duration, warn func(string), out chan<- seedResult) {
	defer close(out)

	deliver := func(r seedResult) bool {
		select {
		case out <- r:
			return true
		case <-ctx.Done():
			return false
		}
	}

	plugin, ok := ix.plugins[seed.PluginID]
	if !ok {
		deliver(seedResult{err: fmt.Errorf("produce-from %s: plugin %q not loaded", seed.Ref, seed.PluginID)})
		return
	}

	instance, err := plugin.Bind(ctx, sdk.PRODUCER, seed.Resource.Name, seed.Block)
	if err != nil {
		deliver(seedResult{err: fmt.Errorf("produce-from %s: failed to bind: %w", seed.Ref, err)})
		return
	}
	defer instance.Close()

	produce := flow.Producer(instance.Produce, seed.Meta.PerMinute, 0, seed.Meta.StopAfter)

	send, errs := make(chan []byte), make(chan error)
	returned := make(chan struct{}) // closed when the seed function returns
	go func() {
		defer close(returned)
		produce(ctx, send, errs)
	}()

	var deadline <-chan time.Time
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		deadline = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline:
			deliver(seedResult{err: fmt.Errorf("produce-from %s: timeout waiting for remote producer", seed.Ref)})
			return
		case err, ok := <-errs:
			if !ok {
				errs = nil // closed; stop selecting on it
				continue
			}
			if err != nil {
				deliver(seedResult{err: fmt.Errorf("produce-from %s: remote producer error: %w", seed.Ref, err)})
				return
			}
		case msg, ok := <-send:
			if !ok {
				// The seed finished. Errors sent after the data close are
				// still owed delivery: wait for the seed function to return
				// (after which no sender can exist) and drain errs.
				select {
				case <-returned:
				case <-ctx.Done():
					return
				}
				for {
					select {
					case err, ok := <-errs:
						if !ok {
							return
						}
						if err != nil {
							deliver(seedResult{err: fmt.Errorf("produce-from %s: remote producer error: %w", seed.Ref, err)})
							return
						}
					default:
						return
					}
				}
			}
			bindings, err := parseRemoteUnit(seed.Ref, msg, ix, warn)
			if err != nil {
				deliver(seedResult{err: err})
				return
			}
			if len(bindings) == 0 {
				continue // nothing declared; the first-message guard stays armed
			}
			deadline = nil // the seed has yielded producers; it is alive
			if !deliver(seedResult{bindings: bindings}) {
				return
			}
		}
	}
}

// parseRemoteUnit parses one seed message as a self-contained config unit —
// the same lexer (gatherOne) and evaluation a .psy file gets, scoped to its
// own locals {} and the ambient environment, with no access to the host
// file's local.* or imports.*. Only produce {} blocks are honoured; every
// other known block type is inert and warned rather than rejected, so a
// long-lived listener is never torn down by a single stray message. The
// extracted produce bindings are all that flow on to the feeder.
func parseRemoteUnit(ref string, msg []byte, ix *resourceIndex, warn func(string)) ([]parse.Resource, error) {
	blocks, err := gatherOne(parse.Source{Name: "remote://" + ref, Content: msg})
	if err != nil {
		return nil, fmt.Errorf("produce-from %s: failed to parse remote config: %w", ref, err)
	}

	for _, b := range blocks.imports {
		warn(fmt.Sprintf("ignoring import block at %s: a remote unit cannot import", rangeOf(b.DefRange)))
	}
	for _, b := range blocks.plugins {
		warn(fmt.Sprintf("ignoring plugin %q at %s: a remote unit cannot declare plugins", b.Labels[0], rangeOf(b.DefRange)))
	}
	for _, b := range blocks.pipelines {
		warn(fmt.Sprintf("ignoring pipeline %q at %s: a remote unit declares producers, not pipelines", b.Labels[0], rangeOf(b.DefRange)))
	}

	// A remote unit is self-contained: env.* is ambient (os.Getenv at parse
	// time), locals {} are the message's own, and imports.* is empty — the
	// host file's local.*/imports.* never leak in.
	env := envVal(envNames(bodiesOf(blocks.locals, blocks.resources), nil))
	localsCtx, err := makeLocalsCtx(blocks.locals, env, cty.EmptyObjectVal)
	if err != nil {
		return nil, fmt.Errorf("produce-from %s: %w", ref, err)
	}

	bindings := make([]parse.Resource, 0, len(blocks.resources))
	for _, block := range blocks.resources {
		if block.Type != blockProduce {
			warn(fmt.Sprintf("ignoring %s %q at %s: a remote unit honours only produce blocks",
				block.Type, strings.Join(block.Labels, "."), rangeOf(block.DefRange)))
			continue
		}
		b, err := makeBinding(block, ix, localsCtx)
		if err != nil {
			return nil, fmt.Errorf("produce-from %s: %w", ref, err)
		}
		bindings = append(bindings, b)
	}

	return bindings, nil
}
