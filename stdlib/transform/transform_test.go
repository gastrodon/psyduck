package transform

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/psyduck-etl/sdk"
)

// psyParser builds a fake sdk.Parser that populates a config struct from a map
// keyed by psy tag — the unit-test stand-in for the host's HCL decoder.
func psyParser(vals map[string]any) sdk.Parser {
	return func(dst any) error {
		rv := reflect.ValueOf(dst).Elem()
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			tag := rt.Field(i).Tag.Get("psy")
			if tag == "" {
				continue
			}
			if v, ok := vals[tag]; ok {
				rv.Field(i).Set(reflect.ValueOf(v))
			}
		}
		return nil
	}
}

func build(t *testing.T, provider func(sdk.Parser) (sdk.Transformer, error), vals map[string]any) sdk.Transformer {
	t.Helper()
	fn, err := provider(psyParser(vals))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return fn
}

// runOne feeds a single message through fn — a fresh in/out/errs channel set
// per call, so stateful transformers (dedupe, batch, count...) keep their
// closure state across successive calls the same way successive messages on
// one long-running channel would. It reports whatever fn emits for that one
// message (there is at most one, since every stdlib transformer under test
// here is 1-to-(0 or 1)) and whatever error it reports, without halting the
// test on either.
func runOne(t *testing.T, fn sdk.Transformer, in string) (string, error, bool) {
	t.Helper()
	inCh := make(chan []byte)
	outCh := make(chan []byte)
	errs := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(context.Background(), inCh, outCh, errs)
	}()
	go func() {
		defer close(inCh)
		inCh <- []byte(in)
	}()

	var out []byte
	ok := false
	for msg := range outCh {
		out, ok = msg, true
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transformer did not finish: hung after closing out")
	}

	var err error
	select {
	case err = <-errs:
	default:
	}

	if !ok {
		return "", err, false
	}
	return string(out), err, true
}

func run(t *testing.T, fn sdk.Transformer, in string) (string, bool) {
	t.Helper()
	out, err, ok := runOne(t, fn, in)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out, ok
}

// runAll feeds every input through a single invocation of fn — one shared
// in/out channel lifetime — and collects everything emitted until out
// closes. Unlike runOne, this is required for transformers (like Batch) that
// keep their buffered state as locals of the returned closure itself rather
// than in the provider's outer scope: those are only meant to be called once
// per stream, with in staying open across every message.
func runAll(t *testing.T, fn sdk.Transformer, ins ...string) []string {
	t.Helper()
	inCh := make(chan []byte)
	outCh := make(chan []byte)
	errs := make(chan error, len(ins)+1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(context.Background(), inCh, outCh, errs)
	}()
	go func() {
		defer close(inCh)
		for _, in := range ins {
			inCh <- []byte(in)
		}
	}()

	var got []string
	for msg := range outCh {
		got = append(got, string(msg))
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transformer did not finish: hung after closing out")
	}
	return got
}

// runAllErrs is runAll that also drains errs after the stream ends, for
// transformers whose per-message failures are forwarded on errs (filter, jq,
// codec on-error=raise) while the message is dropped and the stream keeps
// going — runAll alone can't observe those.
func runAllErrs(t *testing.T, fn sdk.Transformer, ins ...string) ([]string, []error) {
	t.Helper()
	inCh := make(chan []byte)
	outCh := make(chan []byte)
	errs := make(chan error, len(ins)+1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(context.Background(), inCh, outCh, errs)
	}()
	go func() {
		defer close(inCh)
		for _, in := range ins {
			inCh <- []byte(in)
		}
	}()

	var got []string
	for msg := range outCh {
		got = append(got, string(msg))
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transformer did not finish: hung after closing out")
	}
	close(errs)
	var errsGot []error
	for e := range errs {
		errsGot = append(errsGot, e)
	}
	return got, errsGot
}

// assertCancelReleases starts fn, feeds it one message, never reads out, then
// cancels ctx — fn must return promptly instead of parking forever on its
// blocked send. Covers the `case <-ctx.Done()` send-side branch that every
// stdlib transformer grew in the channel-contract rewrite; runOne/runAll only
// ever exercise the clean close-of-in path.
func assertCancelReleases(t *testing.T, fn sdk.Transformer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan []byte)
	out := make(chan []byte) // deliberately never drained
	errs := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(ctx, in, out, errs)
	}()
	in <- []byte(`{"a":1}`) // accepted; fn then blocks trying to emit
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transformer did not return after ctx cancel: parked on blocked send")
	}
}

// driveOnce runs a single transformer invocation to completion over its own
// private in/out/errs channel set, feeding msgs and draining whatever it
// emits. It is the unit of concurrency for TestConcurrentInvocationRace: one
// call == one independent invocation of the same transformer value.
func driveOnce(fn sdk.Transformer, msgs []string) {
	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, len(msgs))
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(context.Background(), in, out, errs)
	}()
	go func() {
		defer close(in)
		for _, m := range msgs {
			in <- []byte(m)
		}
	}()
	for range out {
	}
	<-done
}

// TestConcurrentInvocationRace invokes one transformer VALUE from many
// goroutines at once, each over its own channel set. The stdlib contract is
// that a transformer may be invoked an arbitrary number of times in parallel,
// so any state a provider keeps in its outer scope (shared across every
// invocation) must be synchronized. Dedupe (seen/ring), Uniq (last/have) and
// Count (n) all keep such state; under `go test -race` the unsynchronized
// versions trip the detector here. Run with -race to exercise it.
func TestConcurrentInvocationRace(t *testing.T) {
	const goroutines = 8
	const perGoroutine = 50

	msgs := make([]string, perGoroutine)
	for i := range msgs {
		msgs[i] = fmt.Sprintf(`{"id":%d}`, i%7)
	}

	cases := map[string]sdk.Transformer{
		"dedupe": build(t, Dedupe, map[string]any{"by": "", "path": []string{"id"}, "window": 16}),
		"uniq":   build(t, Uniq, map[string]any{"by": ".id", "path": []string{}}),
		"count":  build(t, Count, map[string]any{"every": 3, "prefix": "n="}),
	}

	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			var wg sync.WaitGroup
			for i := 0; i < goroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					driveOnce(fn, msgs)
				}()
			}
			wg.Wait()
		})
	}
}

// driveCollect is driveOnce that returns everything the invocation emitted,
// so a caller can assert on the merged output of many parallel invocations.
func driveCollect(fn sdk.Transformer, msgs []string) []string {
	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error, len(msgs))
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(context.Background(), in, out, errs)
	}()
	go func() {
		defer close(in)
		for _, m := range msgs {
			in <- []byte(m)
		}
	}()

	var got []string
	for msg := range out {
		got = append(got, string(msg))
	}
	<-done
	return got
}

// parallelMerge invokes fn from `goroutines` goroutines at once — each over its
// own channel set, all sharing fn's outer-scope state — and returns the merged
// output. Each goroutine writes its own result slot, so the fan-out itself adds
// no shared state to the test.
func parallelMerge(fn sdk.Transformer, goroutines int, msgs []string) []string {
	results := make([][]string, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = driveCollect(fn, msgs)
		}(i)
	}
	wg.Wait()

	var all []string
	for _, r := range results {
		all = append(all, r...)
	}
	return all
}

// TestConcurrentCountResult proves Count stays correct — not just race-free —
// under parallel invocation. The counter is global across every invocation, so
// over T total messages it must emit each checkpoint (every E-th count) exactly
// once and pass the rest through, regardless of how the T messages interleave
// across goroutines.
func TestConcurrentCountResult(t *testing.T) {
	const goroutines, perGoroutine, every = 8, 100, 4
	total := goroutines * perGoroutine

	msgs := make([]string, perGoroutine)
	for i := range msgs {
		msgs[i] = "x" // never collides with the "n=" checkpoint prefix
	}
	fn := build(t, Count, map[string]any{"every": every, "prefix": "n="})

	all := parallelMerge(fn, goroutines, msgs)

	// Count is 1-to-1: every input produces exactly one output.
	if len(all) != total {
		t.Fatalf("total outputs = %d, want %d (Count never drops or duplicates)", len(all), total)
	}

	counts := map[uint64]int{}
	passthrough := 0
	for _, s := range all {
		if v, ok := strings.CutPrefix(s, "n="); ok {
			n, err := strconv.ParseUint(v, 10, 64)
			if err != nil {
				t.Fatalf("unparseable checkpoint %q: %v", s, err)
			}
			counts[n]++
			continue
		}
		if s != "x" {
			t.Fatalf("unexpected output %q", s)
		}
		passthrough++
	}

	// The global counter runs 1..total with no gaps or repeats, so the emitted
	// checkpoints are exactly the multiples of `every` in that range, each once.
	wantCheckpoints := total / every
	if len(counts) != wantCheckpoints {
		t.Fatalf("distinct checkpoints = %d, want %d", len(counts), wantCheckpoints)
	}
	for n, c := range counts {
		if c != 1 {
			t.Errorf("checkpoint %d emitted %d times, want exactly once", n, c)
		}
		if n == 0 || n > uint64(total) || n%every != 0 {
			t.Errorf("checkpoint %d is not a multiple of %d within 1..%d", n, every, total)
		}
	}
	if passthrough != total-wantCheckpoints {
		t.Errorf("passthrough = %d, want %d", passthrough, total-wantCheckpoints)
	}
}

// TestConcurrentDedupeResult proves the dedup window is truly global across
// parallel invocations: with the window large enough that nothing is evicted,
// each distinct key must pass exactly once no matter which invocation happens
// to see it first.
func TestConcurrentDedupeResult(t *testing.T) {
	const goroutines, perGoroutine, distinct = 8, 200, 7

	msgs := make([]string, perGoroutine)
	for i := range msgs {
		msgs[i] = fmt.Sprintf(`{"id":%d}`, i%distinct)
	}
	// window > distinct keys => nothing is ever evicted, so the whole run is a
	// single global window and the expected result is order-independent.
	fn := build(t, Dedupe, map[string]any{"by": "", "path": []string{"id"}, "window": 1000})

	all := parallelMerge(fn, goroutines, msgs)

	seen := map[string]int{}
	for _, s := range all {
		seen[s]++
	}
	if len(seen) != distinct {
		t.Fatalf("distinct keys emitted = %d, want %d", len(seen), distinct)
	}
	for key, c := range seen {
		if c != 1 {
			t.Errorf("key %q emitted %d times, want exactly once (global dedup)", key, c)
		}
	}
}

// TestConcurrentUniqResult proves Uniq's compare-and-set is atomic across
// parallel invocations. When every invocation streams the same single key, the
// first message to win the lock emits and sets `last`; every later one sees the
// match and drops — so exactly one message survives, deterministically.
func TestConcurrentUniqResult(t *testing.T) {
	const goroutines, perGoroutine = 8, 200

	msgs := make([]string, perGoroutine)
	for i := range msgs {
		msgs[i] = `{"id":1}`
	}
	fn := build(t, Uniq, map[string]any{"by": ".id", "path": []string{}})

	all := parallelMerge(fn, goroutines, msgs)

	if len(all) != 1 {
		t.Fatalf("uniq of one repeated key across all invocations emitted %d, want exactly 1", len(all))
	}
}

func TestRecode(t *testing.T) {
	fn := build(t, Recode, map[string]any{"decode": "bytes", "encode": "base64", "on-error": "raise"})
	out, _ := run(t, fn, "hello")
	if out != "aGVsbG8=" {
		t.Errorf("recode base64 = %q", out)
	}

	// gzip|json -> json-pretty round-trips through the codec chain
	fn = build(t, Recode, map[string]any{"decode": "json", "encode": "json", "on-error": "raise"})
	out, _ = run(t, fn, `{"b":2,"a":1}`)
	if out != `{"a":1,"b":2}` {
		t.Errorf("recode json normalize = %q", out)
	}
}

func TestPick(t *testing.T) {
	// discrete: path
	fn := build(t, Pick, map[string]any{"path": []string{"user", "name"}, "by": "", "decode": "json", "encode": "json", "on-error": "raise"})
	out, ok := run(t, fn, `{"user":{"name":"ann"}}`)
	if !ok || out != `"ann"` {
		t.Errorf("pick path = %q ok=%v", out, ok)
	}

	// continuous: by (jq)
	fn = build(t, Pick, map[string]any{"path": []string{}, "by": ".items[0]", "decode": "json", "encode": "json", "on-error": "raise"})
	out, ok = run(t, fn, `{"items":[7,8]}`)
	if !ok || out != "7" {
		t.Errorf("pick by = %q ok=%v", out, ok)
	}

	// both set -> configuration error
	if _, err := Pick(psyParser(map[string]any{"path": []string{"a"}, "by": ".a"})); err == nil {
		t.Error("expected error when both path and by set")
	}
}

func TestPickMapSetDrop(t *testing.T) {
	fn := build(t, PickMap, map[string]any{
		"fields": map[string][]string{"n": {"user", "name"}},
		"decode": "json", "encode": "json", "on-error": "raise",
	})
	out, _ := run(t, fn, `{"user":{"name":"bob"}}`)
	if out != `{"n":"bob"}` {
		t.Errorf("pick-map = %q", out)
	}

	fn = build(t, Set, map[string]any{"values": map[string]string{"tag": "x"}, "decode": "json", "encode": "json", "on-error": "raise"})
	out, _ = run(t, fn, `{"a":1}`)
	if out != `{"a":1,"tag":"x"}` {
		t.Errorf("set = %q", out)
	}

	fn = build(t, Drop, map[string]any{"fields": []string{"secret"}, "decode": "json", "encode": "json", "on-error": "raise"})
	out, _ = run(t, fn, `{"a":1,"secret":"s"}`)
	if out != `{"a":1}` {
		t.Errorf("drop = %q", out)
	}
}

func TestSliceChunk(t *testing.T) {
	fn := build(t, Slice, map[string]any{"start": 1, "stop": 4, "step": 1, "decode": "bytes", "encode": "bytes", "on-error": "raise"})
	out, _ := run(t, fn, "0123456")
	if out != "123" {
		t.Errorf("slice = %q", out)
	}

	fn = build(t, Chunk, map[string]any{"size": 2, "keep-tail": true, "decode": "bytes", "encode": "json", "on-error": "raise"})
	out, _ = run(t, fn, "abcde")
	if out != `["ab","cd","e"]` {
		t.Errorf("chunk = %q", out)
	}
}

func TestRender(t *testing.T) {
	fn := build(t, Render, map[string]any{"engine": "template", "format": "{{.name}}!", "decode": "json", "encode": "bytes", "on-error": "raise"})
	out, _ := run(t, fn, `{"name":"ann"}`)
	if out != "ann!" {
		t.Errorf("render template = %q", out)
	}

	fn = build(t, Render, map[string]any{"engine": "printf", "format": "[%s]", "decode": "bytes", "encode": "bytes", "on-error": "raise"})
	out, _ = run(t, fn, "hi")
	if out != "[hi]" {
		t.Errorf("render printf = %q", out)
	}

	fn = build(t, Render, map[string]any{"engine": "jq", "format": ".a + .b", "decode": "json", "encode": "bytes", "on-error": "raise"})
	out, _ = run(t, fn, `{"a":2,"b":3}`)
	if out != "5" {
		t.Errorf("render jq = %q", out)
	}
}

func TestText(t *testing.T) {
	fn := build(t, Split, map[string]any{"delimiter": ",", "decode": "utf-8", "on-error": "raise"})
	out, _ := run(t, fn, "a,b,c")
	if out != `["a","b","c"]` {
		t.Errorf("split = %q", out)
	}

	fn = build(t, Join, map[string]any{"delimiter": "-", "on-error": "raise"})
	out, _ = run(t, fn, `["a","b","c"]`)
	if out != "a-b-c" {
		t.Errorf("join = %q", out)
	}

	fn = build(t, Upper, map[string]any{"decode": "utf-8", "on-error": "raise"})
	out, _ = run(t, fn, "héllo")
	if out != "HÉLLO" {
		t.Errorf("upper = %q", out)
	}

	fn = build(t, Regex, map[string]any{"pattern": `(\d+)`, "replacement": "#$1", "decode": "utf-8", "on-error": "raise"})
	out, _ = run(t, fn, "id 42")
	if out != "id #42" {
		t.Errorf("regex = %q", out)
	}

	fn = build(t, Hash, map[string]any{"algorithm": "sha256", "output": "hex"})
	out, _ = run(t, fn, "")
	if out != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("hash sha256(empty) = %q", out)
	}
}

func TestTextGarbageOnError(t *testing.T) {
	garbage := string([]byte{0xff, 0xfe})

	// invalid utf-8 with on-error=raise surfaces the error
	fn, err := Upper(psyParser(map[string]any{"decode": "utf-8", "on-error": "raise"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err, _ := runOne(t, fn, garbage); err == nil {
		t.Error("expected utf-8 error on garbage with on-error=raise")
	}

	// on-error=drop swallows it
	fn, _ = Upper(psyParser(map[string]any{"decode": "utf-8", "on-error": "drop"}))
	out, err, ok := runOne(t, fn, garbage)
	if err != nil || ok {
		t.Errorf("on-error=drop should swallow: out=%q err=%v", out, err)
	}
}

func TestKeyed(t *testing.T) {
	// dedupe by path
	fn := build(t, Dedupe, map[string]any{"by": "", "path": []string{"id"}, "window": 100})
	if _, ok := run(t, fn, `{"id":1}`); !ok {
		t.Error("first id=1 should pass")
	}
	if _, ok := run(t, fn, `{"id":1}`); ok {
		t.Error("duplicate id=1 should drop")
	}
	if _, ok := run(t, fn, `{"id":2}`); !ok {
		t.Error("id=2 should pass")
	}

	// uniq drops consecutive only
	fn = build(t, Uniq, map[string]any{"by": ".", "path": []string{}})
	run(t, fn, `1`)
	if _, ok := run(t, fn, `1`); ok {
		t.Error("consecutive dup should drop")
	}
	if _, ok := run(t, fn, `2`); !ok {
		t.Error("changed value should pass")
	}

	// batch groups into arrays of size — one channel lifetime for both
	// messages, since Batch's buffer lives in the returned closure itself
	// and is only meant to be called once per stream.
	fn = build(t, Batch, map[string]any{"size": 2})
	if got := runAll(t, fn, `1`, `2`); len(got) != 1 || got[0] != "[1,2]" {
		t.Errorf("batch flush = %v", got)
	}
}

func TestFlow(t *testing.T) {
	// Head/Tail/Sample keep their counters inside the returned closure body
	// (invocation-local, same as Batch), so each is meant to run once per
	// stream — feed every message through one channel lifetime with runAll
	// rather than calling run repeatedly.
	fn := build(t, Head, map[string]any{"count": 2})
	if got := runAll(t, fn, "x", "x", "x", "x", "x"); len(got) != 2 {
		t.Errorf("head passed %d, want 2", len(got))
	}

	fn = build(t, Tail, map[string]any{"skip": 3})
	if got := runAll(t, fn, "x", "x", "x", "x", "x"); len(got) != 2 {
		t.Errorf("tail passed %d, want 2", len(got))
	}

	fn = build(t, Sample, map[string]any{"rate": 2})
	if got := runAll(t, fn, "x", "x", "x", "x"); len(got) != 2 {
		t.Errorf("sample kept %d, want 2", len(got))
	}
}

func TestAssertCount(t *testing.T) {
	fn, _ := Assert(psyParser(map[string]any{"expression": ".ok", "message": "not ok"}))
	if _, err, _ := runOne(t, fn, `{"ok":true}`); err != nil {
		t.Errorf("assert true errored: %v", err)
	}
	if _, err, _ := runOne(t, fn, `{"ok":false}`); err == nil {
		t.Error("assert false should error")
	}

	c := build(t, Count, map[string]any{"every": 2, "prefix": "n="})
	run(t, c, "a")
	out, _ := run(t, c, "b")
	if out != "n=2" {
		t.Errorf("count = %q", out)
	}
}

// Batch flushes a short trailing batch when the stream ends, rather than
// dropping the unfilled remainder — the behavior the channel-contract rewrite
// added (a transformer now sees end-of-stream via in closing). TestKeyed only
// covers the exact-size flush; these cover the close-driven paths.
func TestBatchFlush(t *testing.T) {
	// fewer than size: the partial batch still flushes on close
	fn := build(t, Batch, map[string]any{"size": 3})
	if got := runAll(t, fn, `1`, `2`); len(got) != 1 || got[0] != "[1,2]" {
		t.Errorf("partial flush = %v, want [[1,2]]", got)
	}

	// empty stream: nothing to flush, no empty array emitted
	fn = build(t, Batch, map[string]any{"size": 3})
	if got := runAll(t, fn); len(got) != 0 {
		t.Errorf("empty stream = %v, want nothing", got)
	}

	// full batch then a short trailing one, both emitted in order
	fn = build(t, Batch, map[string]any{"size": 2})
	got := runAll(t, fn, `1`, `2`, `3`)
	if len(got) != 2 || got[0] != "[1,2]" || got[1] != "[3]" {
		t.Errorf("full + trailing = %v, want [[1,2] [3]]", got)
	}
}

func TestFilter(t *testing.T) {
	// a true predicate passes the original message through unchanged
	fn := build(t, Filter, map[string]any{"expression": ".keep"})
	if out, ok := run(t, fn, `{"keep":true}`); !ok || out != `{"keep":true}` {
		t.Errorf("true predicate = %q ok=%v, want original passed", out, ok)
	}
	// false drops
	if _, ok := run(t, fn, `{"keep":false}`); ok {
		t.Error("false predicate should drop")
	}
	// null result (missing field) drops
	if _, ok := run(t, fn, `{"other":1}`); ok {
		t.Error("null predicate should drop")
	}
	// a non-bool truthy result passes the original through
	fn = build(t, Filter, map[string]any{"expression": ".user"})
	if out, ok := run(t, fn, `{"user":{"n":1}}`); !ok || out != `{"user":{"n":1}}` {
		t.Errorf("non-bool predicate = %q ok=%v, want original passed", out, ok)
	}

	// an unparseable expression is a construction error
	if _, err := Filter(psyParser(map[string]any{"expression": "["})); err == nil {
		t.Error("expected parse error for bad expression")
	}

	// a mid-stream runtime error (non-JSON input) is forwarded on errs and the
	// message dropped, but the stream keeps running for later messages
	fn = build(t, Filter, map[string]any{"expression": ".keep"})
	got, errs := runAllErrs(t, fn, `notjson`, `{"keep":true}`)
	if len(got) != 1 || got[0] != `{"keep":true}` {
		t.Errorf("stream after error = %v, want the good message", got)
	}
	if len(errs) != 1 {
		t.Errorf("want 1 forwarded error, got %v", errs)
	}
}

func TestJq(t *testing.T) {
	// a mapping expression emits the transformed value
	fn := build(t, Jq, map[string]any{"expression": ".a + 1"})
	if out, ok := run(t, fn, `{"a":4}`); !ok || out != "5" {
		t.Errorf("jq map = %q ok=%v, want 5", out, ok)
	}
	// an expression that yields no output drops the message
	fn = build(t, Jq, map[string]any{"expression": "empty"})
	if _, ok := run(t, fn, `{"a":1}`); ok {
		t.Error("empty output should drop")
	}

	// unparseable expression is a construction error
	if _, err := Jq(psyParser(map[string]any{"expression": "{"})); err == nil {
		t.Error("expected parse error for bad expression")
	}

	// mid-stream runtime error is forwarded, later messages still processed
	fn = build(t, Jq, map[string]any{"expression": ".a"})
	got, errs := runAllErrs(t, fn, `notjson`, `{"a":7}`)
	if len(got) != 1 || got[0] != "7" {
		t.Errorf("stream after error = %v, want [7]", got)
	}
	if len(errs) != 1 {
		t.Errorf("want 1 forwarded error, got %v", errs)
	}
}

// TestRecodeOnError covers codecTransformer's on-error branches (Recode is the
// plainest codec transformer). TestTextGarbageOnError covers the same for the
// text/stringTransformer family; this is its codec counterpart.
func TestRecodeOnError(t *testing.T) {
	garbage := "notjson"

	// on-error=raise surfaces the decode failure on errs
	fn := build(t, Recode, map[string]any{"decode": "json", "encode": "json", "on-error": "raise"})
	if _, err, _ := runOne(t, fn, garbage); err == nil {
		t.Error("on-error=raise should surface the decode error")
	}

	// on-error=drop swallows it: no output, no error
	fn = build(t, Recode, map[string]any{"decode": "json", "encode": "json", "on-error": "drop"})
	if out, err, ok := runOne(t, fn, garbage); err != nil || ok {
		t.Errorf("on-error=drop should swallow: out=%q err=%v ok=%v", out, err, ok)
	}
}

// TestCancelReleases exercises the send-side ctx.Done branch across every loop
// shape in the package: a cancelled context must unpark a transformer blocked
// trying to emit. The engine relies on this for chain teardown (see core's
// Chain* tests); here each stdlib transformer is checked in isolation.
func TestCancelReleases(t *testing.T) {
	cases := map[string]sdk.Transformer{
		"recode": build(t, Recode, map[string]any{"decode": "json", "encode": "json", "on-error": "raise"}),
		"upper":  build(t, Upper, map[string]any{"decode": "utf-8", "on-error": "raise"}),
		"filter": build(t, Filter, map[string]any{"expression": ".a"}),
		"jq":     build(t, Jq, map[string]any{"expression": ".a"}),
		"dedupe": build(t, Dedupe, map[string]any{"by": "", "path": []string{"a"}, "window": 10}),
		"batch":  build(t, Batch, map[string]any{"size": 1}),
		"assert": build(t, Assert, map[string]any{"expression": ".a", "message": "no"}),
		"count":  build(t, Count, map[string]any{"every": 1, "prefix": "n="}),
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) { assertCancelReleases(t, fn) })
	}
}

// TestCodecErrSendCancelReleases covers reportErr's other cancellation branch:
// a transformer parked trying to forward an error (unbuffered, undrained errs)
// must still return when ctx is cancelled, rather than leaking on the err send.
func TestCodecErrSendCancelReleases(t *testing.T) {
	fn := build(t, Recode, map[string]any{"decode": "json", "encode": "json", "on-error": "raise"})

	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan []byte)
	out := make(chan []byte)
	errs := make(chan error) // unbuffered, never drained
	done := make(chan struct{})

	go func() {
		defer close(done)
		fn(ctx, in, out, errs)
	}()
	in <- []byte(`notjson`) // decode fails; fn parks trying to send on errs
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("transformer did not return after ctx cancel: parked on blocked err send")
	}
}
