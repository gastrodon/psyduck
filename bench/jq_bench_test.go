package bench

import (
	"testing"

	"github.com/gastrodon/psyduck/stdlib/data"
	"github.com/gastrodon/psyduck/stdlib/transform"
)

// BenchmarkByJQ_vs_Compiled is the headline comparison of this package: it
// isolates the cost of parsing a jq expression on every call (what
// data.ByJQ does today, and therefore what `pick { by = ... }` and the
// keyed transformers' `by` selector do on *every message*) against compiling
// the expression once and evaluating the parsed query repeatedly
// (data.CompileJQ + data.EvalJQ, already used correctly by
// transform.Render's "jq" engine and transform.Assert).
//
// gojq.Parse does real lexing/parsing/AST-building work; it is not a cheap
// map lookup. Re-running it per message is pure waste when the expression
// string is static for the transformer's lifetime -- which it always is.
func BenchmarkByJQ_vs_Compiled(b *testing.B) {
	v, err := data.Decode(payloadMedium, "json")
	if err != nil {
		b.Fatal(err)
	}
	const expr = ".address.city"

	b.Run("ByJQ_reparse_every_call", func(b *testing.B) {
		report(b, len(payloadMedium))
		for i := 0; i < b.N; i++ {
			if _, _, err := data.ByJQ(v, expr); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("CompileJQ_once_EvalJQ_per_call", func(b *testing.B) {
		query, err := data.CompileJQ(expr)
		if err != nil {
			b.Fatal(err)
		}
		report(b, len(payloadMedium))
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, _, err := data.EvalJQ(query, v); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkJqTransformer benchmarks the `jq` transformer, which compiles its
// expression once at build time -- the correct pattern.
func BenchmarkJqTransformer(b *testing.B) {
	fn := buildTransformer(b, transform.Jq, map[string]any{"expression": ".address.city"})
	runTransformer(b, fn, payloadMedium)
}

// BenchmarkFilterTransformer benchmarks the `filter` transformer (also
// compiles once at build time).
func BenchmarkFilterTransformer(b *testing.B) {
	fn := buildTransformer(b, transform.Filter, map[string]any{"expression": ".active"})
	runTransformer(b, fn, payloadMedium)
}

// BenchmarkAssertTransformer benchmarks `assert`, which also compiles once.
func BenchmarkAssertTransformer(b *testing.B) {
	fn := buildTransformer(b, transform.Assert, map[string]any{"expression": ".active"})
	runTransformer(b, fn, payloadMedium)
}
