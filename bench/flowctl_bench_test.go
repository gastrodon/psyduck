package bench

import (
	"testing"

	"github.com/gastrodon/psyduck/stdlib/transform"
)

// BenchmarkHead, BenchmarkTail, BenchmarkSample, and BenchmarkCount all
// guard a few-instruction counter with a sync.Mutex (see stdlib/flow/flow.go
// and stdlib/transform/dev.go's Count) even though core.RunPipeline only
// ever calls the transform stack from a single goroutine, sequentially, one
// message at a time (see core/run.go) -- these transformers are never
// called concurrently by the engine, so the lock is uncontended on every
// single call. These benchmarks quantify that fixed per-message overhead;
// see the profiling notes for how it shows up in a CPU profile.

func BenchmarkHead(b *testing.B) {
	fn := buildTransformer(b, transform.Head, map[string]any{"count": 1 << 30})
	runTransformer(b, fn, payloadSmall)
}

func BenchmarkTail(b *testing.B) {
	fn := buildTransformer(b, transform.Tail, map[string]any{"skip": 0})
	runTransformer(b, fn, payloadSmall)
}

func BenchmarkSample(b *testing.B) {
	fn := buildTransformer(b, transform.Sample, map[string]any{"rate": 3})
	runTransformer(b, fn, payloadSmall)
}

func BenchmarkCount(b *testing.B) {
	fn := buildTransformer(b, transform.Count, map[string]any{"every": 1000})
	runTransformer(b, fn, payloadSmall)
}
