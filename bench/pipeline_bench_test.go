package bench

import (
	"context"
	"testing"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/stdlib/consume"
	"github.com/gastrodon/psyduck/stdlib/produce"
	"github.com/gastrodon/psyduck/stdlib/transform"
)

// splitCount divides total as evenly as possible across n producers, the
// same way a real pipeline's producer-set would share `stop-after` load if
// each producer used the same per-producer cap.
func splitCount(total, n int) []int {
	out := make([]int, n)
	base, rem := total/n, total%n
	for i := range out {
		out[i] = base
		if i < rem {
			out[i]++
		}
	}
	return out
}

// closedProducer emits nothing -- the correct stand-in for "zero messages",
// used below instead of produce.Generate with stop-after=0. stop-after=0
// means "no cap" (loop forever) throughout psyduck, by design (see
// docs/stdlib.md); asking Generate for zero messages via stop-after=0 would
// therefore build an infinite producer instead of an empty one.
func closedProducer(_ context.Context, send chan<- []byte, errs chan<- error) {
	close(send)
	close(errs)
}

// stdlibProducers builds n real produce.Generate producers (stdlib, not a
// bench-only stand-in) that together emit exactly `total` copies of payload.
func stdlibProducers(b *testing.B, payload []byte, n, total int) []sdk.Producer {
	b.Helper()
	ps := make([]sdk.Producer, n)
	for i, c := range splitCount(total, n) {
		if c == 0 {
			ps[i] = closedProducer
			continue
		}
		fn, err := produce.Generate(psyParser(map[string]any{
			"values":     []string{string(payload)},
			"loop":       true,
			"stop-after": c,
		}))
		if err != nil {
			b.Fatalf("produce.Generate: %v", err)
		}
		ps[i] = fn
	}
	return ps
}

// stdlibConsumers builds n real consume.Trash consumers.
func stdlibConsumers(b *testing.B, n int) []sdk.Consumer {
	b.Helper()
	cs := make([]sdk.Consumer, n)
	for i := range cs {
		fn, err := consume.Trash(psyParser(nil))
		if err != nil {
			b.Fatalf("consume.Trash: %v", err)
		}
		cs[i] = fn
	}
	return cs
}

// stack composes transformers in declared order, short-circuiting on error
// or a filtered (nil) message. It reimplements core.stackTransform's
// contract, which is unexported and so not directly reusable from here.
func stack(ts ...sdk.Transformer) sdk.Transformer {
	if len(ts) == 0 {
		return nil
	}
	return func(in []byte) ([]byte, error) {
		cur := in
		for _, t := range ts {
			out, err := t(cur)
			if err != nil || out == nil {
				return out, err
			}
			cur = out
		}
		return cur, nil
	}
}

// runPipelineBench runs a real core.Pipeline end to end, with b.N *as the
// total message count* -- not an outer loop repeating a fixed-size run.
// Setup (building producers/consumers, proportional only to fan-out, not to
// b.N) happens before ResetTimer, so ns/op lands on nanoseconds-per-message
// and, via report()'s SetBytes, the default `go test -bench` output also
// gets a correct MB/s throughput column.
func runPipelineBench(b *testing.B, producers, consumers int, transformer sdk.Transformer) {
	b.Helper()
	pipeline := &core.Pipeline{
		Producers:   stdlibProducers(b, payloadMedium, producers, b.N),
		Consumers:   stdlibConsumers(b, consumers),
		Transformer: transformer,
	}
	report(b, len(payloadMedium))
	b.ResetTimer()
	if err := core.RunPipeline(context.Background(), pipeline); err != nil {
		b.Fatal(err)
	}
	if elapsed := b.Elapsed().Seconds(); elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed, "msgs/sec")
	}
}

// BenchmarkPipelineFanout isolates engine/channel overhead from transform
// cost: the transformer is nil (core.RunPipeline's identity passthrough), so
// any difference between these cases is purely the cost of producer fan-in
// and consumer fan-out -- core/stream.go's merge and core/sink.go's
// per-consumer send loop.
//
// core/sink.go's sink.send delivers to each live consumer with its own
// blocking select, one consumer at a time, not concurrently -- so the
// N-consumer cases are expected to scale close to linearly with consumer
// count rather than staying flat.
func BenchmarkPipelineFanout(b *testing.B) {
	cases := []struct {
		name                 string
		producers, consumers int
	}{
		{"1p1c", 1, 1},
		{"1p10c", 1, 10},
		{"10p1c", 10, 1},
		{"10p10c", 10, 10},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			runPipelineBench(b, tc.producers, tc.consumers, nil)
		})
	}
}

// BenchmarkPipelineTransformStack holds fan-out fixed at 1 producer/1
// consumer and varies only the transform stack, isolating per-message
// transform cost from engine overhead. "pick-map+set" and "jq-equivalent"
// perform the same logical reshape (pull two fields, tag the message) via
// the two different idioms docs/patterns.md contrasts; "pick-by-jq" repeats
// the BenchmarkPick comparison at full-pipeline scale.
func BenchmarkPipelineTransformStack(b *testing.B) {
	b.Run("passthrough", func(b *testing.B) {
		runPipelineBench(b, 1, 1, nil)
	})

	b.Run("pick-path", func(b *testing.B) {
		fn := buildTransformer(b, transform.Pick, map[string]any{"path": []string{"address", "city"}, "encode": "bytes"})
		runPipelineBench(b, 1, 1, fn)
	})

	b.Run("pick-by-jq", func(b *testing.B) {
		fn := buildTransformer(b, transform.Pick, map[string]any{"by": ".address.city", "encode": "bytes"})
		runPipelineBench(b, 1, 1, fn)
	})

	b.Run("pick-map+set", func(b *testing.B) {
		pickMap := buildTransformer(b, transform.PickMap, map[string]any{
			"fields": map[string][]string{"city": {"address", "city"}, "name": {"name"}},
		})
		set := buildTransformer(b, transform.Set, map[string]any{
			"values": map[string]string{"source": "bench"},
		})
		runPipelineBench(b, 1, 1, stack(pickMap, set))
	})

	b.Run("jq-equivalent", func(b *testing.B) {
		fn := buildTransformer(b, transform.Jq, map[string]any{
			"expression": `{city: .address.city, name: .name, source: "bench"}`,
		})
		runPipelineBench(b, 1, 1, fn)
	})
}
