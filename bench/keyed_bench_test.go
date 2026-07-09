package bench

import (
	"testing"

	"github.com/gastrodon/psyduck/stdlib/transform"
)

// BenchmarkDedupe covers both selector strategies (see keyer in
// stdlib/transform/keyed.go) across two key-cardinality regimes: a small
// set of keys repeating constantly (the map stays small, most messages are
// dropped) and a large set of effectively-unique keys (the map/ring keep
// growing up to `window`, almost nothing is dropped).
func BenchmarkDedupe(b *testing.B) {
	b.Run("path/duplicate-heavy", func(b *testing.B) {
		fn := buildTransformer(b, transform.Dedupe, map[string]any{"path": []string{"id"}, "window": 1000})
		runTransformerCycled(b, fn, jsonIDVariants(16))
	})
	b.Run("path/unique-heavy", func(b *testing.B) {
		fn := buildTransformer(b, transform.Dedupe, map[string]any{"path": []string{"id"}, "window": 1000})
		runTransformerCycled(b, fn, jsonIDVariants(50_000))
	})
	b.Run("by-jq/duplicate-heavy", func(b *testing.B) {
		fn := buildTransformer(b, transform.Dedupe, map[string]any{"by": ".id", "window": 1000})
		runTransformerCycled(b, fn, jsonIDVariants(16))
	})
	b.Run("by-jq/unique-heavy", func(b *testing.B) {
		fn := buildTransformer(b, transform.Dedupe, map[string]any{"by": ".id", "window": 1000})
		runTransformerCycled(b, fn, jsonIDVariants(50_000))
	})
}

// BenchmarkUniq is dedupe's lighter cousin (only compares to the previous
// message), benchmarked the same way.
func BenchmarkUniq(b *testing.B) {
	b.Run("path", func(b *testing.B) {
		fn := buildTransformer(b, transform.Uniq, map[string]any{"path": []string{"id"}})
		runTransformerCycled(b, fn, jsonIDVariants(16))
	})
	b.Run("by-jq", func(b *testing.B) {
		fn := buildTransformer(b, transform.Uniq, map[string]any{"by": ".id"})
		runTransformerCycled(b, fn, jsonIDVariants(16))
	})
}

func BenchmarkBatch(b *testing.B) {
	fn := buildTransformer(b, transform.Batch, map[string]any{"size": 50})
	runTransformer(b, fn, payloadSmall)
}
