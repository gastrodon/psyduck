package bench

import (
	"testing"

	"github.com/gastrodon/psyduck/stdlib/transform"
)

func BenchmarkRecode(b *testing.B) {
	b.Run("json-to-json/medium", func(b *testing.B) {
		fn := buildTransformer(b, transform.Recode, map[string]any{"decode": "json", "encode": "json"})
		runTransformer(b, fn, payloadMedium)
	})
	b.Run("gzip-json-to-json/medium", func(b *testing.B) {
		fn := buildTransformer(b, transform.Recode, map[string]any{"decode": "gzip|json", "encode": "json"})
		runTransformer(b, fn, payloadMediumGzipped)
	})
	b.Run("base64-to-bytes/medium", func(b *testing.B) {
		fn := buildTransformer(b, transform.Recode, map[string]any{"decode": "base64", "encode": "bytes"})
		runTransformer(b, fn, payloadMediumBase64)
	})
}

// BenchmarkPick compares the two selector strategies docs/patterns.md
// recommends choosing between: `path` (a plain key walk) and `by` (a jq
// expression). They are documented as "path is cheaper" -- this quantifies
// it, and it also demonstrates Pick's `by` path recompiling the jq
// expression on every call (see jq_bench_test.go for the isolated cost).
func BenchmarkPick(b *testing.B) {
	b.Run("path/medium", func(b *testing.B) {
		fn := buildTransformer(b, transform.Pick, map[string]any{"path": []string{"address", "city"}, "encode": "bytes"})
		runTransformer(b, fn, payloadMedium)
	})
	b.Run("by-jq/medium", func(b *testing.B) {
		fn := buildTransformer(b, transform.Pick, map[string]any{"by": ".address.city", "encode": "bytes"})
		runTransformer(b, fn, payloadMedium)
	})
}

func BenchmarkPickMap(b *testing.B) {
	fn := buildTransformer(b, transform.PickMap, map[string]any{
		"fields": map[string][]string{
			"city": {"address", "city"},
			"name": {"name"},
			"id":   {"id"},
		},
	})
	runTransformer(b, fn, payloadMedium)
}

func BenchmarkSet(b *testing.B) {
	fn := buildTransformer(b, transform.Set, map[string]any{
		"values": map[string]string{"source": "bench", "batch": "nightly"},
	})
	runTransformer(b, fn, payloadMedium)
}

func BenchmarkDrop(b *testing.B) {
	fn := buildTransformer(b, transform.Drop, map[string]any{"fields": []string{"meta", "tags"}})
	runTransformer(b, fn, payloadMedium)
}

func BenchmarkSlice(b *testing.B) {
	fn := buildTransformer(b, transform.Slice, map[string]any{"start": 0, "stop": 32, "step": 1})
	runTransformer(b, fn, payloadMedium)
}

func BenchmarkChunk(b *testing.B) {
	fn := buildTransformer(b, transform.Chunk, map[string]any{"size": 16})
	runTransformer(b, fn, payloadMedium)
}

func BenchmarkEvery(b *testing.B) {
	fn := buildTransformer(b, transform.Every, map[string]any{"step": 4, "size": 16})
	runTransformer(b, fn, payloadMedium)
}

// BenchmarkRender compares the three render engines on an equivalent
// extraction, mirroring docs/patterns.md's "template for structured data, jq
// for computed values" guidance.
func BenchmarkRender(b *testing.B) {
	b.Run("template", func(b *testing.B) {
		fn := buildTransformer(b, transform.Render, map[string]any{
			"engine": "template", "format": "{{.name}} <{{.address.city}}>",
		})
		runTransformer(b, fn, payloadMedium)
	})
	b.Run("printf", func(b *testing.B) {
		fn := buildTransformer(b, transform.Render, map[string]any{
			"engine": "printf", "format": "%v",
		})
		runTransformer(b, fn, payloadMedium)
	})
	b.Run("jq", func(b *testing.B) {
		fn := buildTransformer(b, transform.Render, map[string]any{
			"engine": "jq", "format": ".name",
		})
		runTransformer(b, fn, payloadMedium)
	})
}
