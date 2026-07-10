# HTTP API

A control and observability surface for a running psyduck instance: read
what pipelines are executing and their stats, dispatch new pipelines to run,
and expose metrics for Grafana and a graph board.

This document is the design for **stage 1 — everything that concerns a single
instance**. Peer-to-peer (siblings, job splitting) is [stage 2](#stage-2-peers),
sketched at the end but deliberately not built yet.

The API is **live**: `psyduck serve` parses, builds, and runs dispatched
pipelines through the real engine, and reports their status and stats as they
run. What's wired and what's still a follow-up is called out in
[What's wired](#whats-wired-this-pr).

## Why an API at all

psyduck is a CLI: `psyduck run file.psy` builds pipelines and runs them to
completion. That's the right unit for a one-shot job, but three things want
more:

- **Workers.** We want to launch psyduck instances as Nomad (or other
  microservice) workers and hand them jobs at runtime, not bake a `.psy`
  file into each deploy. That's a long-running process that accepts work over
  the network — a daemon, not a one-shot command.
- **Observability.** A self-feeding discovery pipeline flies blind today —
  no throughput, drop, lag, or error numbers (see
  [#18](https://github.com/gastrodon/psyduck/issues/18), item 9). A running
  instance should be able to *tell you* what it's doing.
- **A graph board.** The producer→transform→consumer shape of a pipeline is
  a graph; a live instance can expose it as nodes and edges for a board.

The API is one `psyduck serve` daemon that answers all three.

```sh
psyduck serve --addr :8080
```

## Model

Two nouns.

**Instance** — one `psyduck serve` process. It has an id, a version, an
uptime, and owns some pipelines. In stage 2 an instance also knows about
[peers](#stage-2-peers).

**Pipeline** — one built-and-running (or recently-finished) pipeline the
instance owns. It has:

- an **id** (assigned by the instance) and a **name** (from its `pipeline {}`
  block, or a dispatch label);
- a **status** — `pending` → `running` → one terminal state (`succeeded`,
  `failed`, `canceled`);
- a **source** — where it came from: a file the daemon was told to run, or a
  `dispatch:` label for one POSTed in;
- a **topology** — the resource refs in each slot (`produce.request.api`, …),
  in declaration order. This is display metadata only: the API never returns
  *evaluated* resource config, because that holds secrets (bearer tokens,
  DSNs). Refs are safe; values are not;
- **stats** — the counter snapshot below.

### Stats

The counters map onto the observable events in `core.RunPipeline`'s loop —
one message is produced, then it is filtered, delivered, or errors:

| Field | Meaning |
|---|---|
| `produced` | messages emitted by the producers |
| `transformed` | messages that passed the whole transform stack |
| `filtered` | messages a transformer dropped (a `nil` result today) |
| `delivered` | messages handed to the consumers |
| `errors` | errors reported by any stage |
| `in_flight` | `produced − (delivered + filtered + errors)` — a coarse lag gauge |

Counters are monotonic within a run. A UI computes **rates** by diffing two
snapshots over time; Grafana does the same with `rate()`/`increase()` over
the [`/metrics`](#metrics) series. `in_flight` is the one gauge — it's the
"is a stage falling behind?" signal issue #18 asked for.

## Endpoints

Base path `/api/v1`. Everything is JSON except [`/metrics`](#metrics).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/healthz` | Liveness. `{"status":"ok"}`. |
| `GET` | `/api/v1/instance` | Instance identity + rolled-up pipeline counts. |
| `GET` | `/api/v1/pipelines` | List every pipeline (running, pending, recently terminal). |
| `POST` | `/api/v1/pipelines` | **Dispatch** a pipeline to run. `202 Accepted`. |
| `GET` | `/api/v1/pipelines/{id}` | One pipeline: topology + stats + status. |
| `GET` | `/api/v1/pipelines/{id}/stats` | Just the stats block, for cheap polling. |
| `DELETE` | `/api/v1/pipelines/{id}` | Cancel a running/pending pipeline. |
| `GET` | `/api/v1/graph` | Node/edge projection of everything running. |
| `GET` | `/api/v1/plugins` | List the instance's plugin manifest. |
| `POST` | `/api/v1/plugins` | **Register** a plugin (clone + compile into the store). `202 Accepted`. |
| `GET` | `/api/v1/plugins/{name}` | One plugin's manifest: its resources and their options. |
| `PUT` | `/api/v1/plugins/{name}` | **Update** a plugin's source/tag and rebuild. `202 Accepted`. |
| `DELETE` | `/api/v1/plugins/{name}` | **Remove** a plugin from the manifest. |
| `GET` | `/metrics` | Prometheus/OpenMetrics exposition. |
| `GET` | `/api/v1/peers` | **Stage 2.** Returns `501` today. |

Error responses are `{"error": "..."}` with a matching status
(`400` bad request, `404` not found, `409` not cancelable, `501` peers).

### Observe

`GET /api/v1/instance`:

```json
{
  "id": "psyduck-local",
  "version": "0.1.0-dev",
  "started_at": "2026-07-10T12:00:00Z",
  "uptime_seconds": 1.02,
  "pipelines": { "running": 1, "pending": 0, "succeeded": 1,
                 "failed": 0, "canceled": 0, "total": 2 }
}
```

`GET /api/v1/pipelines/{id}`:

```json
{
  "id": "pipe-000001",
  "name": "ingest",
  "status": "running",
  "source": "file:examples/ingest.psy",
  "topology": {
    "producers":    [{"ref": "produce.http-listen.hook", "kind": "produce"}],
    "transformers": [{"ref": "transform.jq.shape", "kind": "transform"},
                     {"ref": "transform.set.tag",  "kind": "transform"}],
    "consumers":    [{"ref": "consume.socket.bus",  "kind": "consume"}]
  },
  "stats": {"produced": 1280, "transformed": 1204, "filtered": 76,
            "delivered": 1204, "errors": 2, "in_flight": 0},
  "created_at": "2026-07-10T12:00:00Z",
  "started_at": "2026-07-10T12:00:00Z"
}
```

A pipeline that uses `produce-from` reports its seed as `topology.remote_seed`
instead of `producers` — mirroring `parse.PipelineSpec`, which the API view is
built from.

### Dispatch

`POST /api/v1/pipelines` is the network front door to the pattern psyduck
already has internally. Today, the [meta-pipeline pattern](patterns.md#meta-pipelines)
feeds `produce-from` off a socket or queue: one process writes `produce {}`
blocks, a worker reads and runs them. Dispatch is the same idea with HTTP as
the transport — an operator (or, in stage 2, a peer) POSTs a whole pipeline
and the instance runs it.

The body is a complete `.psy` document — the same HCL a `psyduck run` file
holds — plus advisory metadata:

```json
{
  "name": "adhoc-backfill",
  "source": "produce \"sequence\" \"pages\" { stop-after = 50 }\nconsume \"file\" \"out\" { location = \"-\" }\npipeline \"adhoc\" { produce = [produce.sequence.pages] consume = [consume.file.out] }",
  "labels": { "run": "manual" }
}
```

Response is `202 Accepted` with a `Location: /api/v1/pipelines/{id}` header and
the created record (status `pending`). The instance parses, builds, and runs
the pipeline asynchronously; the caller polls `GET .../{id}` (or watches
`/metrics`) for progress. `DELETE .../{id}` cancels it.

Why a full `.psy` document rather than a JSON pipeline spec? Because it reuses
`parse/hcl` verbatim — the same strict attribute validation, `env.*`
resolution, imports, and `plugin {}` handling a file gets — with no second
schema to keep in sync. (The known sharp edge is that dispatched HCL is
stringly-typed, exactly the concern in issue #18 item 7; a structured
alternative can come later without changing the route.)

> **Security.** Dispatch runs arbitrary pipeline config on the instance,
> which can read files, open sockets, and load plugins. It is **not**
> authenticated today (only the [plugin routes](#plugins) are, via
> `--basic-auth`) — bind `serve` to a private interface / mesh and, until
> dispatch shares the same gate, keep it off any public bind. Extending the
> Basic-auth guard to dispatch is a one-line change (the middleware is
> reusable); see [Open questions](#open-questions).

### Metrics

Two audiences, two formats — that's why we expose both:

- **JSON stats** on the API (`/api/v1/pipelines/{id}/stats`, or the `stats`
  block on each pipeline) — for a UI / graph board that wants to shape the
  numbers itself.
- **Prometheus/OpenMetrics** text at `GET /metrics` — for Grafana. Point a
  Prometheus data source at the instance; Prometheus *pulls* on a schedule
  (no push infra to run), and the counters become dashboards.

The exposition is hand-written (no client-library dependency — the format is
small and stable):

```
# TYPE psyduck_pipelines gauge
psyduck_pipelines{status="running"} 1
# TYPE psyduck_pipeline_messages_produced_total counter
psyduck_pipeline_messages_produced_total{pipeline="pipe-000001",name="ingest"} 1280
# TYPE psyduck_pipeline_in_flight gauge
psyduck_pipeline_in_flight{pipeline="pipe-000001",name="ingest"} 0
```

| Metric | Type | Labels | |
|---|---|---|---|
| `psyduck_build_info` | gauge | `version`, `instance` | always `1` |
| `psyduck_instance_uptime_seconds` | gauge | — | |
| `psyduck_pipelines` | gauge | `status` | count by lifecycle state |
| `psyduck_pipeline_messages_produced_total` | counter | `pipeline`, `name` | |
| `psyduck_pipeline_messages_transformed_total` | counter | `pipeline`, `name` | |
| `psyduck_pipeline_messages_filtered_total` | counter | `pipeline`, `name` | |
| `psyduck_pipeline_messages_delivered_total` | counter | `pipeline`, `name` | |
| `psyduck_pipeline_errors_total` | counter | `pipeline`, `name` | |
| `psyduck_pipeline_in_flight` | gauge | `pipeline`, `name` | coarse lag |

A typical Grafana panel is `rate(psyduck_pipeline_messages_delivered_total[1m])`
per pipeline, with `psyduck_pipeline_in_flight` on a second axis to spot a
lagging stage.

### Graph board

`GET /api/v1/graph` is a denormalized projection of the same data, shaped for
a node/edge visualization instead of per-pipeline polling. Each pipeline is a
container node; each stage is a node wired producer → transform → consumer;
each edge carries the message count that has crossed it.

```json
{
  "nodes": [
    {"id": "pipe-000001", "kind": "pipeline", "label": "ingest", "status": "running",
     "stats": {"produced": 1280, "delivered": 1204, "...": 0}},
    {"id": "pipe-000001:produce:0",   "kind": "produce",   "label": "produce.http-listen.hook", "pipeline": "pipe-000001"},
    {"id": "pipe-000001:transform:0", "kind": "transform", "label": "transform.jq.shape",        "pipeline": "pipe-000001"}
  ],
  "edges": [
    {"from": "pipe-000001:produce:0", "to": "pipe-000001:transform:0", "messages": 1204}
  ]
}
```

### Plugins

An instance ships with the stdlib resources. To let dispatched jobs use an
external plugin (amqp, mysql, ifunny, …), you register it with the instance —
and this is deliberately a **store/manifest operation, not an in-process
load**. A running node doesn't hold every plugin resident forever; instead it
keeps a *manifest* (name → source, ref, hash) and the compiled `.so` in a
content-addressed store, and a **job loads the plugins it needs at dispatch**.

The model, in one breath: managing plugins edits what *future* jobs can use;
a dispatched job must carry a `plugin {}` block that matches the manifest,
exactly like a `.psy` file needs its lock for `psyduck run`; and editing the
manifest never disturbs a pipeline already running (it holds the plugin
snapshot it was dispatched with).

**Register** — `POST /api/v1/plugins`, mirroring a `plugin {}` block:

```json
{ "name": "amqp", "source": "https://github.com/psyduck-etl/amqp", "tag": "v0.1.0" }
```

This clones + compiles the plugin and content-addresses the binary into the
store (`--plugin-dir`, default `.psyduck/`) — a partial, on-demand `psyduck
init` scoped to one plugin. It's slow (network + `go build`), so it's
asynchronous: `202 Accepted`, status `loading`, then `ready` (built, in the
store) or `failed` (with the build error). `ready` means *available to jobs*,
not *resident in the process*.

**List / manifest** — `GET /api/v1/plugins` returns every manifest entry with
its status; `GET /api/v1/plugins/{name}` returns the full manifest — the
resources the plugin offers and every option each accepts:

```json
{
  "name": "amqp",
  "source": "https://github.com/psyduck-etl/amqp",
  "ref": "refs/tags/v0.1.0",
  "status": "ready",
  "resources": [
    {
      "name": "amqp-queue",
      "kinds": ["produce", "consume"],
      "options": [
        {"name": "connection", "type": "string", "required": true},
        {"name": "queue", "type": "string", "required": true}
      ]
    }
  ]
}
```

Reading the manifest is the one operation that *opens* the binary (a plugin's
resources are only knowable from the loaded plugin) — so it's what first makes
a plugin resident; the handle is then cached.

**Update** — `PUT /api/v1/plugins/{name}` re-points a plugin at a new
`source`/`tag` and rebuilds it into the store. **Delete** —
`DELETE /api/v1/plugins/{name}` removes it from the manifest so new jobs can
no longer declare it. Both are file/manifest operations; both leave running
pipelines untouched.

**Using it** — a dispatched job declares the plugin and uses its resources:

```hcl
plugin "amqp" {
  source = "https://github.com/psyduck-etl/amqp"
  tag    = "v0.1.0"
}

produce "amqp-queue" "in" {
  connection = "amqp://guest:guest@localhost:5672/"
  queue      = "work"
}

consume "file" "out" { location = "-" }

pipeline "drain" {
  produce = [produce.amqp-queue.in]
  consume = [consume.file.out]
}
```

At dispatch the instance resolves the `plugin {}` block against its manifest,
loads that binary from the store, and builds the pipeline with it. A job that
names a plugin the instance doesn't have — or whose source/tag doesn't match
the manifest — is rejected with a clear error.

**Auth.** Because registration clones and compiles operator-supplied sources
(arbitrary code on the host), the whole `/api/v1/plugins` subtree is gated by
HTTP Basic auth when `serve --basic-auth user:pass` (or
`PSYDUCK_SERVE_BASIC_AUTH`) is set — the same `"user:pass"` convention
stdlib's `request` transport uses. Unset, the routes stay open and `serve`
logs a warning at startup. A gated request without valid credentials gets
`401` with a `WWW-Authenticate` challenge; observability, dispatch, and
`/metrics` are not behind this gate.

> **The one hard constraint: Go plugins can't be unloaded or reloaded.** Once
> a `.so` is opened it stays resident for the life of the process. So
> *delete* frees the manifest, not the memory, and *update* can't hot-swap a
> plugin a job has already loaded: the rebuilt binary lands in the store and
> the record is flagged `restart_required`, taking effect for new jobs after
> a restart. This is why registration is a store operation and loading is
> per-job — it keeps the unavoidable residency scoped to what's actually been
> used, and keeps add/list/update/delete honest as file operations. Freeing
> orphaned store binaries (a `psyduck` store-GC) is a follow-up.

## Runtime shape

`psyduck serve` is a long-running daemon. Its layers:

```
 HTTP router (server.Server)      routes + JSON/Prometheus marshaling; no runtime types
        │  server.Supervisor (interface)
        ▼
 supervise.Supervisor            owns pipelines; parses/builds/runs; List/Get/Dispatch/Cancel/…
        │
        ▼
 core.BuildPipeline / RunPipeline the engine, one goroutine per pipeline; counters wired in
```

The **`server` package never imports `core` or `parse`** — it talks only to a
`Supervisor` interface. That boundary is the whole point, and it holds even
now that the API is live: the concrete `supervise.Supervisor` imports `core`,
`parse`, and `stdlib`; `server` imports none of them. The same routes serve
either the live supervisor (`psyduck serve`) or the in-memory `StubSupervisor`
(HTTP-layer tests), and the HTTP concerns stay testable without standing up a
pipeline.

### What's wired (this PR)

The API observes and runs real pipelines:

- **`supervise.Supervisor`** (new package) implements `server.Supervisor`.
  `Dispatch` parses `source` with `hcl.NewParserHCL()` against an in-memory
  loader, requires exactly one `pipeline{}` block, and — asynchronously —
  calls `core.BuildPipeline` and `core.RunPipeline` in a goroutine under a
  cancelable child of the serve context. `Cancel` cancels it; terminal state
  (`succeeded`/`failed`/`canceled`) and any error come from `RunPipeline`'s
  return. Topology is extracted from the parsed `parse.PipelineSpec` at
  dispatch time, so it's visible before the run finishes. `psyduck serve` now
  runs this, not the stub.
- **Stats in `core`.** `core.Stats` is a set of `atomic.Uint64` counters on
  `core.Pipeline`; `RunPipeline` bumps them at each observable event
  (produced / transformed / filtered / delivered / errors), and
  `Stats.Snapshot()` reads a copy concurrently. The change is additive — a
  nil `Stats` means "don't count", so the CLI `run` path and any caller that
  skips `BuildPipeline` are unaffected. The supervisor reads a snapshot per
  pipeline and computes `in_flight`; `server` marshals it to JSON and
  `/metrics`.

- **Plugin manifest.** The supervisor keeps a manifest of plugins (see
  [Plugins](#plugins)). `AddPlugin`/`UpdatePlugin` build a spec into the
  content-addressed `plugins.Store` (via `store.Build`) — asynchronously,
  since it clones and compiles — and record the resolved ref/hash;
  `RemovePlugin` drops the manifest entry. Dispatch extracts a job's
  `plugin{}` blocks (`hcl…Plugins`), resolves them against the manifest, and
  loads just those from the store (`store.Load`) for that job. Opened
  binaries are cached by content hash, since Go can't unload them.

`StubSupervisor` stays as the fixture the `server` package tests against —
same interface, representative data, no runtime.

### Still to wire (follow-ups)

- **Persistence.** The manifest and terminal pipelines live in memory; a
  restart forgets them (the store's compiled binaries do persist on disk).
  Persisting the manifest is what makes an update's `restart_required` fully
  meaningful.
- **Store GC.** `DELETE` removes a plugin from the manifest but leaves its
  content-addressed binary in the store cache; a `psyduck` store-GC to reap
  unreferenced binaries is a follow-up.
- **Auth.** The plugin routes are gated by HTTP Basic auth (`--basic-auth`);
  dispatch/cancel are not yet, though they're just as sensitive. Extending
  the same guard to them (and, later, tokens/mTLS) is the follow-up — see
  [Open questions](#open-questions).
- **Per-resource stats.** Counters are per-pipeline; per-stage attribution
  (which transformer drops, which consumer errors) wants the counter keyed by
  resource ref.

None of these change a route or payload — the contract in this document is
fixed.

## Stage 2: peers

Out of scope for this PR, reserved so the shape is visible. A `Peer` type
exists in the `server` package and `GET /api/v1/peers` answers `501` (not
`404` — the route is planned, just unavailable).

The use case: one instance gets a job, learns about its siblings, and
distributes parts of the job across them. Open design questions for the next
round:

- **Discovery** — how does an instance learn its peers? Static config, a
  Nomad/Consul service catalog, or gossip?
- **Identity & health** — `GET /api/v1/instance` is already the "who are you"
  handshake a peer would call; peers add a liveness/heartbeat notion on top.
- **Job splitting** — how is one job partitioned? The natural fit is the
  existing dispatch surface: a coordinator splits work into N `.psy` documents
  and POSTs each to a peer's `/api/v1/pipelines` — peer distribution as
  fan-out over the stage-1 dispatch endpoint, no new runtime concept.
- **Aggregation & backpressure** — where do partial results land, and how does
  a slow peer signal it?

Reserved endpoints (shape TBD): `GET/POST /api/v1/peers`,
`POST /api/v1/peers/{id}/dispatch`.

## Open questions

- **Auth.** The plugin routes have a built-in HTTP Basic guard
  (`--basic-auth user:pass`, mirroring stdlib's `request` credential format);
  the reusable middleware lives in `server/auth.go`. Open: extend it to
  dispatch/cancel (equally sensitive), and whether to add token/mTLS as
  stronger options in front of `serve`.
- **Persistence.** Terminal pipelines live in memory; a restart forgets them.
  Do we need a retention window, or a bounded ring of recent runs?
- **Dispatch payload.** Full `.psy` HCL reuses `parse/hcl` for free but is
  stringly-typed (issue #18 item 7). A structured variant can be added behind
  the same route later.
- **Per-stage stats.** Counters are per-pipeline today. Per-resource stats
  (which transformer is dropping? which consumer errors?) need the
  instrumentation hook to key by resource ref.

## Try it

```sh
go run . serve --addr :8080 --basic-auth ops:s3cret
curl -s localhost:8080/api/v1/instance | jq
curl -s localhost:8080/api/v1/pipelines | jq
curl -s -XPOST localhost:8080/api/v1/pipelines \
  -d '{"name":"demo","source":"pipeline \"demo\" {}"}'
curl -s localhost:8080/metrics

# plugin routes need the credential:
curl -s -u ops:s3cret localhost:8080/api/v1/plugins | jq
curl -s -u ops:s3cret -XPOST localhost:8080/api/v1/plugins \
  -d '{"name":"amqp","source":"https://github.com/psyduck-etl/amqp","tag":"v0.1.0"}'
```
