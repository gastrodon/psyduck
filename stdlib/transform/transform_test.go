package transform

import (
	"context"
	"reflect"
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
