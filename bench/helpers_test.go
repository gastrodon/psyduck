// Package bench is a suite of Go benchmarks (stdlib `testing.B`, no third
// party framework) that exercise psyduck pipelines built entirely from
// stdlib resources -- the benchmarking analogue of examples/*.psy. It
// measures data actually flowing through the engine and its primitives, not
// just parsing or config plumbing.
//
// See README.md for how to run these and how to read the output.
package bench

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"github.com/psyduck-etl/sdk"
)

// psyParser builds a fake sdk.Parser that populates a config struct's
// psy-tagged fields from vals -- the same reflection-based stand-in for the
// host's HCL decoder already used by stdlib/transform, stdlib/produce, and
// stdlib/integration's own test suites, reused here so benchmarks build real
// stdlib resources without depending on the HCL/parse layers.
func psyParser(vals map[string]any) sdk.Parser {
	return func(dst any) error {
		rv := reflect.ValueOf(dst).Elem()
		rt := rv.Type()
		for i := 0; i < rt.NumField(); i++ {
			tag := rt.Field(i).Tag.Get("psy")
			if tag == "" {
				continue
			}
			if v, ok := vals[tag]; ok && v != nil {
				rv.Field(i).Set(reflect.ValueOf(v))
			}
		}
		return nil
	}
}

// buildTransformer binds a stdlib transformer provider against a literal
// config, failing the benchmark (not the whole run) on a build error.
func buildTransformer(b *testing.B, provider func(sdk.Parser) (sdk.Transformer, error), vals map[string]any) sdk.Transformer {
	b.Helper()
	fn, err := provider(psyParser(vals))
	if err != nil {
		b.Fatalf("build transformer: %v", err)
	}
	return fn
}

// report configures the two knobs nearly every benchmark below wants:
// allocation stats regardless of -benchmem, and a per-op byte size so
// `go test -bench` prints a throughput (MB/s) column -- the closest the
// standard testing package comes to criterion's throughput reporting.
func report(b *testing.B, opBytes int) {
	b.Helper()
	b.ReportAllocs()
	b.SetBytes(int64(opBytes))
}

// runTransformer times fn against a fixed payload, b.N times.
func runTransformer(b *testing.B, fn sdk.Transformer, payload []byte) {
	b.Helper()
	report(b, len(payload))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn(payload); err != nil {
			b.Fatal(err)
		}
	}
}

// runTransformerCycled times fn cycling through payloads round-robin --
// for stateful transformers (dedupe/uniq) where feeding the same message
// b.N times would misrepresent steady-state behavior.
func runTransformerCycled(b *testing.B, fn sdk.Transformer, payloads [][]byte) {
	b.Helper()
	n := len(payloads)
	if n == 0 {
		b.Fatal("runTransformerCycled: no payloads")
	}
	total := 0
	for _, p := range payloads {
		total += len(p)
	}
	report(b, total/n)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := fn(payloads[i%n]); err != nil {
			b.Fatal(err)
		}
	}
}

// ── payload fixtures ───────────────────────────────────────────────────────
// Deterministic (fixed rand seed) so runs are reproducible across machines
// and comparable with benchstat.

var (
	payloadSmall  = []byte(`{"id":1,"name":"ann","active":true}`)
	payloadMedium = []byte(`{"id":1042,"name":"ann arbor","active":true,"tags":["a","b","c","d","e"],"address":{"street":"123 main st","city":"ann arbor","zip":"48104"},"score":93.25,"meta":{"created":"2024-01-01T00:00:00Z","source":"e2e"}}`)
	payloadLarge  = buildLargeJSON(200)
	payloadText   = []byte("  The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs.  \n")
	payloadBinary = deterministicBytes(4096)
	payloadCSV    = []byte("id,name,score\n1,ann,93.2\n2,bob,81.5\n3,cy,77.0\n")

	payloadMediumGzipped = mustGzip(payloadMedium)
	payloadMediumBase64  = mustBase64(payloadMedium)
	payloadBinaryHex     = mustHex(payloadBinary)
)

// buildLargeJSON renders a JSON array of `records` small objects -- stands
// in for a "big batch" message, e.g. a bulk API response.
func buildLargeJSON(records int) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i := 0; i < records; i++ {
		if i > 0 {
			buf.WriteByte(',')
		}
		fmt.Fprintf(&buf, `{"id":%d,"name":"user-%d","active":%t,"score":%d.5,"tags":["a","b","c"]}`, i, i, i%2 == 0, i)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}

// jsonIDVariants renders n distinct small JSON documents, used to drive
// keyed transformers (dedupe/uniq) through realistic key-cardinality
// scenarios rather than a single repeated message.
func jsonIDVariants(n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte(fmt.Sprintf(`{"id":%d,"name":"user-%d"}`, i, i))
	}
	return out
}

func deterministicBytes(n int) []byte {
	r := rand.New(rand.NewSource(1))
	b := make([]byte, n)
	r.Read(b)
	return b
}

func mustGzip(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func mustBase64(b []byte) []byte {
	out := make([]byte, base64.StdEncoding.EncodedLen(len(b)))
	base64.StdEncoding.Encode(out, b)
	return out
}

func mustHex(b []byte) []byte {
	out := make([]byte, hex.EncodedLen(len(b)))
	hex.Encode(out, b)
	return out
}
