package bench

import (
	"testing"

	"github.com/gastrodon/psyduck/stdlib/transform"
)

var payloadJoinInput = []byte(`["alpha","beta","gamma","delta","epsilon","zeta","eta","theta"]`)

func BenchmarkSplit(b *testing.B) {
	fn := buildTransformer(b, transform.Split, map[string]any{"delimiter": " "})
	runTransformer(b, fn, payloadText)
}

func BenchmarkJoin(b *testing.B) {
	fn := buildTransformer(b, transform.Join, map[string]any{"delimiter": ","})
	runTransformer(b, fn, payloadJoinInput)
}

func BenchmarkReplace(b *testing.B) {
	fn := buildTransformer(b, transform.Replace, map[string]any{"old": "o", "new": "0"})
	runTransformer(b, fn, payloadText)
}

func BenchmarkRegex(b *testing.B) {
	fn := buildTransformer(b, transform.Regex, map[string]any{"pattern": `\bfox\b`, "replacement": "cat"})
	runTransformer(b, fn, payloadText)
}

func BenchmarkTrim(b *testing.B) {
	fn := buildTransformer(b, transform.Trim, map[string]any{})
	runTransformer(b, fn, payloadText)
}

func BenchmarkUpper(b *testing.B) {
	fn := buildTransformer(b, transform.Upper, map[string]any{})
	runTransformer(b, fn, payloadText)
}

func BenchmarkLower(b *testing.B) {
	fn := buildTransformer(b, transform.Lower, map[string]any{})
	runTransformer(b, fn, payloadText)
}

func BenchmarkHash(b *testing.B) {
	for _, algo := range []string{"sha256", "sha512", "md5"} {
		b.Run(algo, func(b *testing.B) {
			fn := buildTransformer(b, transform.Hash, map[string]any{"algorithm": algo})
			runTransformer(b, fn, payloadMedium)
		})
	}
}
