//go:build linux || darwin

package main

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/psyduck-etl/sdk"

	"github.com/gastrodon/psyduck/core"
	"github.com/gastrodon/psyduck/parse"
	"github.com/gastrodon/psyduck/plugins"
)

// benchSource emits n copies of payload from a single in-proc producer, so
// benchmark measurements isolate the transform stage's subprocess wire cost
// rather than mixing in a produce stream.
func benchSource(n int, payload []byte) core.ProducerSource {
	return func(ctx context.Context) (<-chan sdk.Producer, <-chan error) {
		feed := make(chan sdk.Producer, 1)
		errs := make(chan error)
		feed <- func(ctx context.Context, send chan<- []byte, perrs chan<- error) {
			defer close(send)
			defer close(perrs)
			for i := 0; i < n; i++ {
				select {
				case send <- payload:
				case <-ctx.Done():
					return
				}
			}
		}
		close(feed)
		close(errs)
		return feed, errs
	}
}

// stack chains transformers the way core's build step does: each stage's out
// feeds the next's in, sharing one errs channel.
func stack(ts []sdk.Transformer) sdk.Transformer {
	if len(ts) == 1 {
		return ts[0]
	}
	return func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
		stage := in
		for _, t := range ts[:len(ts)-1] {
			next := make(chan []byte)
			go t(ctx, stage, next, errs)
			stage = next
		}
		ts[len(ts)-1](ctx, stage, out, errs)
	}
}

// BenchmarkPluginTransform pushes b.N records through stacked subprocess
// transformer stages (the example plugin's affix, a minimal per-record
// mapping), so the number reflects the gRPC Transform stream itself:
// serialization, batching, and the per-stage hop. Compare against the unary
// Transform baseline by running the same benchmark on the grpc-plugins
// branch (pre-streaming) — the streamed wire amortizes what unary pays per
// record.
func BenchmarkPluginTransform(b *testing.B) {
	src, err := filepath.Abs("cmd/example-plugin")
	if err != nil {
		b.Fatal(err)
	}
	store := plugins.NewStore(b.TempDir())
	locked, err := store.Build([]parse.Plugin{{Name: "example-plugin", Source: src}})
	if err != nil {
		b.Fatalf("Build: %v", err)
	}
	loaded, err := store.Load(locked)
	if err != nil {
		b.Fatalf("Load: %v", err)
	}
	defer store.Close()
	plugin := loaded[0]

	payload := bytes.Repeat([]byte("x"), 64)
	block := sdk.NewJSONBlock(sdk.SourceRange{SourceName: "bench"}, []byte(`{"suffix":"!"}`))

	for _, stages := range []int{1, 3} {
		b.Run(fmt.Sprintf("stages=%d", stages), func(b *testing.B) {
			ts := make([]sdk.Transformer, stages)
			for i := range ts {
				inst, err := plugin.Bind(sdk.TRANSFORMER, "affix", block)
				if err != nil {
					b.Fatalf("Bind: %v", err)
				}
				ts[i] = inst.Transform
			}

			var got atomic.Int64
			pipe := &core.Pipeline{
				Producers: benchSource(b.N, payload),
				Parallel:  1,
				Consumers: []sdk.Consumer{func(_ context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{}) {
					defer close(done)
					defer close(errs)
					for range recv {
						got.Add(1)
					}
				}},
				Transformer: stack(ts),
				ExitOnError: true,
			}

			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			if err := core.RunPipeline(context.Background(), pipe); err != nil {
				b.Fatal(err)
			}
			b.StopTimer()

			if n := got.Load(); n != int64(b.N) {
				b.Fatalf("delivered %d of %d records", n, b.N)
			}
		})
	}
}
