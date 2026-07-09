# bench

A suite of Go benchmarks for psyduck, in the same spirit as `examples/*.psy`:
every benchmark here builds a pipeline (or a single stdlib primitive) out of
**only stdlib resources**, and measures data actually moving through it --
not parsing, not config plumbing.

It uses nothing but the standard library's `testing` package (`testing.B`,
`go test -bench`). No third-party benchmarking framework is involved.

## Layout

| File | What it measures |
|---|---|
| `data_bench_test.go` | `stdlib/data`'s `Decode`/`Encode` -- the codec/Value-tree layer every codec-aware transformer sits on |
| `transform_bench_test.go` | The codec-aware "shape" transformers: `recode`, `pick`, `pick-map`, `set`, `drop`, `slice`, `chunk`, `every`, `render` |
| `jq_bench_test.go` | jq-driven paths: `data.ByJQ` vs `data.CompileJQ`+`EvalJQ`, the `jq`/`filter`/`assert` transformers |
| `keyed_bench_test.go` | `dedupe`, `uniq`, `batch` |
| `text_bench_test.go` | `split`, `join`, `replace`, `regex`, `trim`, `upper`, `lower`, `hash` |
| `flowctl_bench_test.go` | `head`, `tail`, `sample`, `count` |
| `transport_bench_test.go` | `transport.Delimit` framing (`Split`/`Joiner`) shared by every transport |
| `pipeline_bench_test.go` | end-to-end `core.RunPipeline`: fan-in/fan-out scaling, transform-stack depth |
| `helpers_test.go` | shared payload fixtures + the `psyParser` reflection helper (same pattern as `stdlib/*_test.go`) |

## Running

```sh
# quick smoke test (1 iteration per benchmark, just checks nothing panics/hangs)
go test -run='^$' -bench=. -benchtime=1x ./bench/...

# a single benchmark family
go test -run='^$' -bench=BenchmarkByJQ_vs_Compiled -benchmem ./bench/...
```

### "Criterion-like" statistics

Go's `testing.B` reports `ns/op` and, with `-benchmem`, `B/op`/`allocs/op` --
and where a benchmark calls `b.SetBytes`, a throughput (`MB/s`) column, the
closest the standard library comes to criterion's throughput report. It does
**not** have criterion's built-in statistical comparison (mean, confidence
interval, regression detection across runs) -- for that, the Go team ships a
separate but standard tool: **benchstat** (`golang.org/x/perf/cmd/benchstat`).
Combined with `go test -count=N`, this is the Go-native equivalent of
`cargo criterion`: N repeated samples in, a mean ± confidence interval out.

```sh
go install golang.org/x/perf/cmd/benchstat@latest

scripts/bench.sh baseline          # runs the whole suite 10x, benchstat-summarizes it
scripts/bench.sh baseline 20 1s    # count=20, benchtime=1s per rep, for tighter CIs

# before/after an optimization:
scripts/bench.sh before
# ...make a change...
scripts/bench.sh after
scripts/compare.sh before after    # benchstat delta % + significance
```

Output lands in `bench/results/<label>.txt` (raw) and
`bench/results/<label>.benchstat.txt` (formatted summary with `± %` CIs).
`results/` is gitignored -- these are regenerated, not committed.

### Profiling

`go test -cpuprofile`/`-memprofile` and `go tool pprof` are themselves
standard-library/standard-toolchain features -- no wrapper script needed:

```sh
go test -run='^$' -bench='BenchmarkByJQ_vs_Compiled|BenchmarkPick|BenchmarkDedupe' \
  -benchtime=2s -benchmem -cpuprofile=cpu.prof -memprofile=mem.prof ./bench/...

go tool pprof -top -nodecount=30 cpu.prof          # flat CPU time, text
go tool pprof -top -cum -nodecount=30 cpu.prof     # cumulative CPU time, text
go tool pprof -top -sample_index=alloc_space mem.prof   # top allocators by bytes
```

For the call graph and flame graph, `go tool pprof -http=:0 cpu.prof` opens
pprof's own web UI in a browser -- the "box -> box" call graph is the
default view, and the flame graph is one click away (`VIEW` menu). That web
UI is the standard/canonical way to look at these; it needs no extra
tooling beyond the Go toolchain itself (a `-svg`/`-png` export of the call
graph alone is also available and only needs graphviz's `dot` on `PATH`,
if a static image is preferred over the interactive UI).

## Design notes

- **`report(b, n)`** calls `b.ReportAllocs()` + `b.SetBytes(n)` so every
  benchmark gets alloc stats regardless of `-benchmem` and a throughput
  column for free.
- **`psyParser`** is the same reflection-based fake `sdk.Parser` already used
  by `stdlib/transform`, `stdlib/produce`, and `stdlib/integration`'s test
  suites -- benchmarks build real stdlib resources without going through
  the HCL/parse layers.
- **`pipeline_bench_test.go`'s `runPipelineBench`** uses `b.N` itself as the
  total message count (not an outer loop re-running a fixed-size batch) --
  producers/consumers are (re)built fresh per calibration call, sized to
  the current `b.N`, and only `core.RunPipeline` itself runs inside the
  timed region. This means `ns/op` lands directly on nanoseconds-per-message
  and `SetBytes` gives a correct `MB/s` for the engine's real per-message
  throughput.
- Every producer count in `pipeline_bench_test.go` is built through real
  `produce.Generate` -- **not** `stop-after: 0` with `loop: true`, which
  means "run forever" throughout psyduck (see `docs/stdlib.md`), not "emit
  zero". A producer needing zero messages gets a small local
  `closedProducer` that just closes its channels.
- Stateful transformers (`dedupe`, `uniq`) are benchmarked against a cycled
  set of distinct payloads (`runTransformerCycled`), not one repeated
  message, so the reported cost reflects realistic key-cardinality
  behavior instead of a degenerate best case.

See `RESULTS.md` for the actual run and the resulting optimization triage.
