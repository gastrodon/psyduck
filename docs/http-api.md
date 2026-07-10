# HTTP API

A control and observability surface for a running psyduck instance: read
what pipelines are executing and their stats, dispatch new pipelines to run,
and expose metrics for Grafana and a graph board.

This document is the design for **stage 1 — everything that concerns a single
instance**. Peer-to-peer (siblings, job splitting) is [stage 2](#stage-2-peers),
sketched at the end but deliberately not built yet.

The API ships today as a **compiling skeleton**: every route exists and
returns correctly-shaped data, backed by a stub. What's stubbed and what's
real is called out throughout, and the runtime work to make it live is in
[Wiring it live](#wiring-it-live).

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
> authenticated in this skeleton — bind `serve` to a private interface / mesh
> and put auth (token, mTLS) in front before exposing it. Auth is called out
> as a follow-up in [Open questions](#open-questions), not designed here.

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

## Runtime shape

`psyduck serve` is a long-running daemon. Its layers:

```
 HTTP router (server.Server)      routes + JSON/Prometheus marshaling; no runtime types
        │
        ▼
 Supervisor (interface)           owns pipelines; List/Get/Dispatch/Cancel/Graph/Instance
        │
        ▼
 core.BuildPipeline / RunPipeline unchanged engine, one goroutine per pipeline
```

The **`server` package never imports `core` or `parse`** — it talks only to a
`Supervisor` interface. That boundary is the whole point: it lets the exact
same routes serve a stub today and a live supervisor tomorrow, and keeps the
HTTP concerns (status codes, payload shapes) testable without standing up a
pipeline.

### What's real vs. stubbed today

**Real:** the router, every route and status code, the JSON payload types,
the Prometheus exposition, the graph projection (`buildGraph`, a pure
function the live supervisor reuses verbatim), graceful shutdown on
SIGINT/SIGTERM, and the full test suite (`server/server_test.go`).

**Stubbed:** `StubSupervisor` is in-memory. It seeds two representative
pipelines, records a `pending` pipeline on dispatch (running nothing), and
honors cancel. It never parses `source`, builds a `core.Pipeline`, or moves a
counter on its own. Everything it returns has the exact shape the live
implementation will — so a UI, a Grafana dashboard, or a client can be built
against it now.

### Wiring it live

Turning the stub into a real supervisor is the follow-up, in two pieces:

1. **A live `Supervisor`.** Holds a map of `id → running pipeline`. `Dispatch`
   parses `source` with `hcl.NewParserHCL()`, calls `core.BuildPipeline`,
   assigns an id, and runs it in its own goroutine under a cancelable context;
   `Cancel` cancels that context; `List`/`Get` read the map. Terminal state
   and `error` come from `RunPipeline`'s return.

2. **Instrumentation in `core`.** The stats counters need a home. `RunPipeline`
   already sees every event — it increments `delivered`, calls `report(err)`,
   and knows when `transform` returns `nil` (filtered). The minimal hook is an
   optional counter sink on the pipeline (e.g. a `core.Stats` struct of
   `atomic.Uint64`s the loop bumps, exposed via a getter). The supervisor
   reads that snapshot for each pipeline; `server` marshals it. This is the
   only change outside the `server` package, and it's additive — a nil sink
   means "don't count", so `psyduck run` is unaffected.

Neither piece changes any route or payload, which is why they're safe to
defer: the contract in this document is already fixed.

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

- **Auth.** None in the skeleton. Token or mTLS in front of `serve`, or a
  built-in middleware? Dispatch especially needs it before any public bind.
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
go run . serve --addr :8080
curl -s localhost:8080/api/v1/instance | jq
curl -s localhost:8080/api/v1/pipelines | jq
curl -s -XPOST localhost:8080/api/v1/pipelines \
  -d '{"name":"demo","source":"pipeline \"demo\" {}"}'
curl -s localhost:8080/metrics
```
