# Benchmark results & optimization triage

Generated from the `bench/` suite (`golang.org/x/perf/cmd/benchstat` over
10 repeats via `scripts/bench.sh baseline`), profiled with plain
`go test -cpuprofile`/`-memprofile` and `go tool pprof`. Machine: 4-core
Intel Xeon @ 2.80GHz container. Raw data lives in `results/baseline.txt` /
`results/baseline.benchstat.txt` (gitignored -- regenerate with
`scripts/bench.sh baseline`).

This is a **triage of propositions**, not applied fixes -- nothing in
`stdlib`/`core` was changed. Every number below is measured, not estimated;
±% is benchstat's confidence interval across 10 repeats.

## Summary: where the time goes

| Rank | Finding | Evidence | Effort | Risk |
|---|---|---|---|---|
| 1 | Codec chain specs (`"json"`, `"gzip\|json"`, ...) are re-split on every `Decode`/`Encode` call | `splitSpec` = 8.5% of CPU / 5.5% of allocations across the codec benchmark family | Low | Low (needs `sync.Map`/`RWMutex`, not a plain map -- see below) |
| 2 | gzip encode/decode build a fresh compressor/decompressor per call, never pooled | gzip construction (deduplicated) is ~29% of all allocations in the codec profile -- the single largest cluster; `Encode/gzip` is 10x `Decode/gzip` for the same payload | Medium | Low-Medium (watch the zero-value-Writer trap below) |
| 3 | `by=` jq selectors (`pick`, `dedupe`, `uniq`) re-parse (not re-*compile* -- see below) the expression on every message | ~21% saving available in isolation; the larger 1.8x-3.0x deltas vs `path=` are mostly inherent to using jq at all, not this bug alone | Low | Low |
| 4 | `core/sink.go`'s fan-out sends to consumers sequentially, not concurrently | Real mechanism, but every one of this repo's 15 example pipelines uses exactly 1 consumer -- see reframing below | High | Medium |
| 5 | `flow.Head`/`Tail`/`Sample`/`transform.Count` guard a plain counter with an always-uncontended `sync.Mutex` | Engine calls the transform stack from one goroutine only; mutex is provably dead weight | Low | Low |

Findings 1 and 3 are the same anti-pattern in two different layers: **doing
per-message work (parsing a static config string) that only needs to
happen once, at transformer-build time.** Finding 2 is related but
distinct -- reusable, expensive-to-construct state (a compressor) that
simply isn't pooled.

**All five findings went through an adversarial verification pass**
(independent agents re-reading the actual source, re-running benchmarks,
and in one case writing and running an original repro program) before this
report was finalized -- not a rubber stamp. It caught and corrected: a
real mechanism error in the jq write-up (conflating jq *parsing* with jq
*compiling*, finding #3); a real concurrency hazard in a naive fix for
finding #1 (would race across psyduck's one-goroutine-per-pipeline model,
confirmed against `main.go`); a measurement error in finding #2 (the
original "well over half of all allocations" claim double-counted nested
profile frames and misattributed unrelated CSV-codec allocations -- the
real, still-significant figure is ~29%) plus a genuinely shippable silent
bug in its proposed fix (a naively pool-seeded `gzip.Writer` silently
stops compressing); and an overstated usage claim in finding #4 (corrected
against all 15 `examples/*.psy` files). Every correction is called out
inline in its section rather than swept under the rug.

---

## 1. Codec chain specs are re-split on every `Decode`/`Encode` call

**Where:** `stdlib/data/codec.go`'s `Decode(b, as)` and `Encode(v, as)` both
call `splitSpec(as)` (`stdlib/data/pattern.go`: `strings.Split(spec, "|")`
plus a trim/normalize loop, allocating a fresh `[]string` every time) before
doing anything else. Every codec-aware transformer -- `recode`, `pick`,
`pick-map`, `set`, `drop`, `slice`, `chunk`, `every`, `render` (all built on
`transform/codec.go`'s `codecTransformer`) -- calls `data.Decode`/`Encode`
once per message with a `decode`/`encode` string that is fixed at
transformer-build time (`"json"`, `"gzip|json"`, ...) and never changes.
`Registry.Decode`/`Registry.Encode` (a second, related pair used by the
byte-codec chain machinery) have the same shape: they call `splitSpec` and
also allocate a fresh `[]Pattern` chain on every call.

This is the same anti-pattern as finding #3 below (jq re-parsing), just in
the codec layer instead -- and it's more widely felt, since it sits on the
path of most of the stdlib's transformers, not only the jq-based ones.

**Measured cost:** a CPU profile of the codec benchmark family
(`BenchmarkDecode`/`BenchmarkEncode`/`BenchmarkDecodeEncodeRoundTrip`, ~57s
of samples) attributes:
- `data.splitSpec`: **8.49% of cumulative CPU** (4.87s of 57.36s)
- `strings.genSplit`: **4.45% of cumulative CPU**
- `data.splitSpec`: **5.48% of cumulative allocation** (1.21GB of 22.15GB)

This was independently re-verified against the source (`codec.go`,
`pattern.go`, `transform/codec.go`) and the profile text files line-by-line;
all figures check out exactly as measured.

**Proposition, with a verified concurrency hazard:** cache the parsed chain
keyed by the spec string. **This must be a `sync.Map` or an
`RWMutex`-guarded map, not a plain map** -- `main.go` runs every pipeline in
a file on its own goroutine concurrently (`go func(b *core.Pipeline) { ...
core.RunPipeline(...) }`), and since `codecTransformer` defaults
`decode`/`encode` to `"json"` when unset, it's entirely realistic for two
concurrently-running pipelines to call `Decode`/`Encode` with the identical
spec string at the same moment -- a bare `map[string][]string` cache-miss
write under that condition is a real data race (`fatal error: concurrent
map writes`), not a theoretical one. Caching just the `[]string` chain (not
the resolved codec functions) is safe against `Registry.Register` adding or
overriding a codec later, since name-to-function resolution still happens
live, after the cached names are retrieved; if a future version instead
caches the *resolved* `[]Pattern` chain for extra savings, that cache would
need its own invalidation story for `Register` (not a concern today --
`Register` has no call sites anywhere in this repo currently, but worth
documenting as an invariant if that path is ever taken).

---

## 2. gzip encode/decode allocate a fresh compressor per call

**Where:** `stdlib/data/pattern.go`'s gzip codec:

```go
// encode
gz := gzip.NewWriter(&buf); gz.Write(b); gz.Close()
// decode
gz, _ := gzip.NewReader(bytes.NewReader(b)); io.ReadAll(gz)
```

Neither side reuses anything across calls.

**Measured cost:** `Encode/gzip/medium` (compressing a ~230B payload):
**364.2µs ± 32%**, 795KiB/op, 22 allocs/op -- roughly **10x**
`Decode/gzip/medium` (**34.74µs ± 7%**, 40.8KiB/op, 10 allocs/op) for the
same payload size. Compression is inherently pricier than decompression,
but not by architecture alone -- a memory profile of the codec family shows
gzip/flate *construction* (not the actual compress/decompress work)
dominating: `compress/flate.(*dictDecoder).init` alone is **15.3% of all
allocated bytes** in that profile (3.40GB of 22.15GB).

*Correction from verification:* the first draft of this finding summed
`flate.NewWriter` + `gzip.NewReader` + `flate.NewReader` + the
`bufio.New*Size` wrappers and claimed "well over half" of all allocations.
That double-counts: `gzip.NewReader`'s 4.27GB cumulative figure already
*includes* `flate.NewReader`'s 4.20GB, which already includes
`dictDecoder.init`'s 3.40GB -- they're parent/child frames on one call
stack, not independent contributors. The `bufio.New*Size` allocations are
also misattributed: this codec's gzip input is always a `bytes.Reader`,
which already satisfies `io.ByteReader`, so neither `gzip`/`flate`'s reader
nor its writer path ever wraps it in a `bufio.Reader`/`Writer` here -- that
allocation actually comes from the unrelated CSV codec in the same
benchmark suite (`encoding/csv` always bufio-wraps unconditionally). A
de-duplicated, gzip-only figure is `gzip.NewReader` cum (4.27GB, already
inclusive of its children) + `flate.NewWriter` cum (2.22GB) ≈ **6.49GB,
~29% of the 22.15GB suite total** -- still the single largest identifiable
cluster in the whole benchmark suite (encoding/json's own machinery is the
next biggest at ~15-19%), just not "over half."

**Proposition:** pool `*gzip.Writer`/`*gzip.Reader` via `sync.Pool`, using
`gzip.Writer.Reset(w)`/`gzip.Reader.Reset(r)` -- both exist specifically
for this reuse pattern, and both were confirmed (by reading the Go 1.24
stdlib source and by an actual concurrent-stress-plus-`-race` repro during
verification) to fully re-initialize all per-message state with no
cross-message leakage. `bufio` pooling is not applicable to this codec
specifically (see above) and would be wasted effort here. Two concrete,
non-obvious hazards a real implementation must handle -- the second one is
a genuine, easy-to-ship bug, not a theoretical caveat:

1. **Seed the pool with `gzip.NewWriter`, never `new(gzip.Writer)`.** A
   zero-value `*gzip.Writer`'s `level` field is `0`, which is
   `flate.NoCompression`, not `DefaultCompression` -- `Writer.Reset` only
   reuses whatever level is already on the struct, it can't set one. This
   was reproduced directly: a pool seeded with `new(gzip.Writer)` silently
   produced *larger* output than the input (225 bytes for a 197-byte
   payload) instead of compressing it. Round-trip tests would not catch
   this (decode still works fine) -- only compression ratio would reveal
   it, which is exactly the kind of regression that ships unnoticed.
2. If the destination `bytes.Buffer` is also pooled, the `[]byte` returned
   from `buf.Bytes()` aliases its backing array and must be copied out
   before the buffer is reset/reused -- or the buffer must not be pooled.
   `io.ReadAll` on the decode side already returns an unaliased slice, so
   decode has no equivalent hazard.

Standard `sync.Pool` discipline (never let a pooled reader/writer escape
the function that `Get()` it; one `Put()` per `Get()`) is what keeps this
safe under concurrent pipelines -- no gzip-specific concurrency hazard
beyond that.

---

## 3. `pick { by = ... }` / `dedupe`/`uniq { by = ... }` re-parse jq on every message

*(Numbers and mechanism below were corrected after an adversarial
verification pass caught a real mistake in the first draft of this
finding -- see the callout after the proposition.)*

**Where:** `stdlib/data/walk.go`'s `ByJQ(v, expr)` calls `gojq.Parse(expr)`
inline, every call (this file, not `pattern.go`, is where `ByJQ`/`CompileJQ`/
`EvalJQ`/`runByJQ` actually live):

```go
func ByJQ(v Value, expr string) (Value, bool, error) {
	query, err := gojq.Parse(expr)   // <-- runs again for every message
	...
	return runByJQ(query, v)
}
```

`stdlib/transform/shape.go`'s `Pick` calls `data.ByJQ(v, config.By)` inside
its per-message op when `by` is set; `stdlib/transform/keyed.go`'s `keyer.key`
(shared by `dedupe`/`uniq`) does the same. `config.By` is a string fixed once
at transformer-build time -- it is never different from one message to the
next. `data.CompileJQ(expr)` (parse once) + `data.EvalJQ(query, v)` (run the
pre-parsed query) already exist for exactly this purpose, and
`transform.Render`'s `"jq"` engine and `transform.Assert` already use them.

**What the fix actually buys you.** `gojq.Parse` only builds an AST. Both
`ByJQ` and `EvalJQ` funnel into the same `runByJQ`, which calls
`query.Run(input)` -- and `(*gojq.Query).Run` **recompiles the AST to
bytecode via `gojq.Compile` on every single call**, regardless of whether
the `*gojq.Query` was parsed once or freshly. So `CompileJQ`+`EvalJQ` only
removes the re-*parse*; it does **not** remove the re-*compile*, and neither
does `transform.Render`'s existing "jq" engine or `transform.Assert` -- they
all pay `gojq.Compile` per message today.

**Measured cost, corrected:**

| Comparison | Cost | What it actually isolates |
|---|---|---|
| `ByJQ_reparse_every_call` vs `CompileJQ_once_EvalJQ_per_call` | 9.287µs ± 10% vs 7.334µs ± 10% (**~21% less**, -8 allocs) | The *only* saving `CompileJQ`+`EvalJQ` provides: skipping `gojq.Parse`. `gojq.Compile` still runs in both arms. |
| `Pick/path/medium` vs `Pick/by-jq/medium` | 8.724µs ± 7% vs 15.78µs ± 8% (1.81x) | The full cost of choosing a jq selector (`Parse`+`Compile`+`Run`+the `Native`/`normalizeForJQ` round trip) over a plain key walk (`data.Walk`) -- not a bug, this is inherent to the two selector strategies and is what docs/patterns.md already means by "path is cheaper." |
| `Dedupe`/`Uniq` path vs by-jq | 2.9x-3.0x | Same as above, for the keyed transformers. |

A CPU profile of the whole `BenchmarkByJQ_vs_Compiled|BenchmarkPick|
BenchmarkDedupe|BenchmarkUniq|...` family shows `gojq.Compile` at **12.48%
of cumulative CPU** -- but that cost is paid via `Query.Run` in *both* the
`ByJQ` and `EvalJQ` paths, so applying the shallow fix will not remove it.

**Two propositions, ranked by what they actually close:**

1. **(medium priority, low risk, do this anyway)** Give `keyer` (keyed.go)
   and `Pick`'s `by` branch (shape.go) a build-time `data.CompileJQ(config.By)`
   call and switch their per-message call from `data.ByJQ` to `data.EvalJQ`.
   Real, measured win: ~21% inside the by-jq path (the isolated benchmark
   above), plus a genuine correctness improvement -- a malformed `by`
   expression now fails at pipeline-build time instead of on the first
   message that reaches it. Guard the same way `keyer.key` already does
   (only compile when `by != ""`, since `path`/`by` are mutually exclusive)
   so path-mode configs don't spuriously try to compile an empty expression.
   `*gojq.Query` is documented safe for concurrent reuse, so this introduces
   no concurrency hazard under the engine's per-pipeline-goroutine model.

2. **(higher priority if the 12.48%-CPU `gojq.Compile` hotspot itself is the
   target, more invasive)** Cache the actual bytecode: call `gojq.Compile`
   once (via a new/changed helper returning `*gojq.Code` instead of
   `*gojq.Query`) at build time, and call `(*gojq.Code).Run`/`RunWithContext`
   per message instead of going through `Query.Run`. This is the change that
   would actually remove the profiled `gojq.Compile` cost -- but it changes
   `data.CompileJQ`'s public return type (or adds a parallel function), and
   for consistency should also be applied to `transform.Render`'s `"jq"`
   engine and `transform.Assert`, which pay the identical recompile cost
   today despite already "doing it right" by the shallower definition.

---

## 4. Consumer fan-out is sequential, not concurrent

**Where:** `core/sink.go`'s `sink.send` delivers one message to every live
consumer with its own blocking `select`, one consumer at a time:

```go
for i := range s.ins {
    if s.finished[i] { continue }
    select {
    case s.ins[i] <- msg: continue
    case <-s.dones[i]: s.finished[i] = true; s.live--
    case <-ctx.Done(): return false
    }
}
```

Each `s.ins[i]` is unbuffered, so every send is a full goroutine-scheduling
rendezvous with that specific consumer, and the loop pays that cost
`len(consumers)` times sequentially per message.

**Measured cost** (`bench/pipeline_bench_test.go`'s `BenchmarkPipelineFanout`,
`nil` transformer so this isolates pure engine/channel overhead):

| Shape | ns/msg | msgs/sec |
|---|---|---|
| 1 producer x 1 consumer | 2.108µs ± 8% | 474.5k ± 7% |
| 1 producer x 10 consumers | 10.35µs ± 6% (**4.91x**) | 96.69k ± 6% |
| 10 producers x 1 consumer | 2.502µs ± 10% (~flat vs 1x1) | 399.8k ± 11% |
| 10 producers x 10 consumers | 8.647µs ± 7% (~flat vs 1x10) | 115.7k ± 6% |

Fan-**in** (more producers) costs essentially nothing extra -- `core/
stream.go`'s merge-into-one-channel design scales fine. Fan-**out** (more
consumers) scales close to linearly with consumer count, confirming the
sequential-send design is the bottleneck, not some other per-producer cost.
A CPU profile backs this up structurally: `runtime.selectgo` is **26.9% of
cumulative CPU** and `core.(*sink).send` is **14.8%** across this whole
benchmark family.

**Reframed after verification -- this is real but its priority is lower
than "10 consumers" makes it look, and its actual bite is different from
what the homogeneous benchmark above shows:**

- *Usage reality check:* every one of this repo's 15 `examples/*.psy`
  pipelines declares exactly one consumer; none declare two or more,
  despite multi-consumer fan-out being a documented, supported feature.
  "10 consumers" is a synthetic stress case with no analog anywhere in this
  project today -- the first draft of this finding said "most examples use
  one or two consumers," which overstated how much multi-consumer usage
  actually exists.
- *But the sequential design has a sharper edge than raw consumer count
  suggests:* because `sink.send` only moves to consumer `i+1` once
  consumer `i`'s rendezvous resolves, **one slow consumer taxes every
  consumer after it in the slice**, every message. The benchmark above uses
  identical, near-instant `consume.Trash` consumers, so it only measures
  the fixed per-consumer scheduling cost -- a real pipeline mixing a slow
  consumer (a blocking HTTP POST, say) with a fast one (a local file) would
  see materially worse behavior than this benchmark shows, and would see it
  at **2-3 consumers**, not 10.
- *The sequential design isn't accidental, either:* it currently provides
  two properties for free that a naive concurrent rewrite must go out of
  its way to preserve -- strict per-consumer in-order delivery, and natural
  backpressure (the whole fan-out for message N can't finish until the
  slowest live consumer accepts it, which is what keeps a fast producer
  from running away from a slow consumer).

**Proposition (higher effort, architectural, lower priority than it first
looked):** deliver to all live consumers concurrently -- e.g. spawn
per-consumer work and join with a `sync.WaitGroup` before returning, or a
persistent per-consumer relay goroutine fed by its own queue -- so total
per-message fan-out latency approaches `max(consumer latency)` rather than
`sum(consumer latencies)`. A join-based design can preserve both properties
above (it's still a per-message barrier), but is not a safe drop-in:
- `s.finished[i]`/`s.live` are mutated today from a single goroutine with
  no synchronization; any version with multiple goroutines writing them
  directly is a real data race on `s.live` specifically (writes to
  disjoint `s.finished[i]` slice elements from goroutine `i` are fine on
  their own). Collect each worker's outcome and only mutate `s.finished`/
  `s.live` from the calling goroutine after `Wait()` returns.
- `send`'s `false` return today means "ctx ended *or* every consumer
  finished," and `RunPipeline` depends on that exact disjunction to decide
  whether to report `outer.Err()` or a clean stop. A concurrent version
  must reproduce it precisely when aggregating per-worker results, not
  just count finishes.
- Needs new tests: a `-race` run under N-consumer fan-out with mid-fan-out
  cancellation, plus an explicit per-consumer *ordering* assertion --
  `core/run_test.go` today only asserts message *counts* per consumer, so
  an ordering regression here would currently slip past the test suite.

Given the usage reality check above, this is worth doing if/when
multi-consumer or heterogeneous-latency-consumer pipelines become a real
shape this project supports at scale -- not an urgent item today.

---

## 5. Uncontended mutexes in `head`/`tail`/`sample`/`count`

**Where:** `stdlib/flow/flow.go`'s `Head`/`Tail`/`Sample` and
`stdlib/transform/dev.go`'s `Count` each guard a plain `int` counter with a
`sync.Mutex`. `core/run.go`'s `RunPipeline` calls the single stacked
`Transformer` synchronously, once per message, from exactly one goroutine
(the main `for msg, err := range produce(...)` loop) -- transformers are
never invoked concurrently by the engine itself, so the lock is always
uncontended.

**Measured cost:** these are already fast in absolute terms --
`BenchmarkHead` 23.25ns ± 2%, `BenchmarkTail` 22.79ns ± 3%, `BenchmarkSample`
28.77ns ± 3%, `BenchmarkCount` 26.69ns ± 2%, all 0 allocs/op -- so this is a
small, fixed, per-message tax (a lock/unlock pair), not a scaling problem.
Low priority, but also basically free to fix and provably safe to fix given
the engine's single-goroutine-per-transform-stack contract.

This was independently verified by re-reading `core/run.go`/`core/build.go`/
`sdk/resource.go` line-by-line (confirming exactly one call site for the
transform stack, and that `Bind` always constructs a fresh closure per
pipeline -- nothing shared even across the concurrently-run pipelines
`main.go`'s `run` command starts) and by re-running the four benchmarks
independently, which reproduced closely (22.88/23.68/29.40/27.08ns vs.
23.25/22.79/28.77/26.69ns here).

**Proposition:** drop the `sync.Mutex` in favor of a plain `int`. One
caveat the verification pass surfaced: `sdk.Transformer` is declared as a
bare `func(in []byte) ([]byte, error)` with no documented concurrency
contract at the type level, and `sdk.Plugin`'s doc comment explicitly
anticipates future non-in-process (RPC/socket) implementations -- so this
single-goroutine guarantee is a fact about *this* engine's current
`RunPipeline`, not something the type system enforces. If the mutex is
dropped, add a doc comment on `Head`/`Tail`/`Sample`/`Count` stating they
rely on being called from one goroutine at a time, so a future
worker-pool-style engine change (or any other host reusing
`stdlib/flow`/`stdlib/transform` directly) doesn't silently reintroduce a
race instead of a compile error.

---

## Other things worth a look, not deep enough to call findings

- **`Render/printf`** (16.92µs ± 14%) is noticeably slower than
  `Render/template` (12.35µs ± 7%) or `Render/jq` (12.30µs ± 5%) for an
  equivalent extraction -- likely `fmt.Sprintf`'s reflection path plus
  `data.Native`'s allocation. Not investigated deeply; worth a look if
  `render { engine = "printf" }` shows up in a hot pipeline.
- **`data.Native`/`fromNative`** (the Value-tree <-> native Go data
  conversion) account for a combined ~21% of allocation in the jq-benchmark
  profile -- every jq-driven operation currently pays for building a Value
  tree from decoded JSON *and then* unwrapping it straight back to native
  data for gojq. This is architectural (the Value-tree abstraction is
  central to the whole data model) and not a quick fix, but worth knowing
  about if jq-heavy pipelines become a bigger share of real workloads.
- gzip aside, **`Decode`/`Encode` cost scales roughly linearly with payload
  size** as expected (`json/large-200rec` ~660µs/599µs vs `json/medium`
  ~7.7µs/7.3µs for ~20x the bytes) -- no surprises there, included for
  completeness.

---

## How these numbers were produced

```sh
bench/scripts/bench.sh baseline 10 200ms .          # full suite, benchstat summary

# profiles for each family below, via plain go test + go tool pprof:
go test -run='^$' -bench='BenchmarkByJQ_vs_Compiled|BenchmarkPick|BenchmarkDedupe|BenchmarkUniq|BenchmarkJqTransformer|BenchmarkFilterTransformer|BenchmarkAssertTransformer' \
  -benchtime=1500ms -benchmem -cpuprofile=jq.cpu.prof -memprofile=jq.mem.prof ./bench/...
go test -run='^$' -bench='BenchmarkDecode|BenchmarkEncode|BenchmarkDecodeEncodeRoundTrip' \
  -benchtime=1500ms -benchmem -cpuprofile=codec.cpu.prof -memprofile=codec.mem.prof ./bench/...
go test -run='^$' -bench='BenchmarkPipelineFanout|BenchmarkPipelineTransformStack' \
  -benchtime=1500ms -benchmem -cpuprofile=pipeline.cpu.prof -memprofile=pipeline.mem.prof ./bench/...

go tool pprof -top -cum jq.cpu.prof            # text reports quoted throughout this file
go tool pprof -http=: jq.cpu.prof              # interactive call graph + flame graph
```

See `README.md` for how to reproduce/extend this.
