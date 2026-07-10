# psyduck

psyduck is an extensible ETL engine. Pipelines are described in `.psy` files
(HCL syntax), built from a small standard library of primitives, and can be
extended with Go plugins that are cloned and compiled on demand.

- **Configure it**: producers → transformers → consumers, wired declaratively.
- **Extend it**: any `sdk.Plugin` can be published as a Git repo and referenced
  from a pipeline with a `plugin {}` block.
- **Run it**: `psyduck run <file>.psy`.

## Install

Build from source (Go 1.24+):

```sh
git clone https://github.com/gastrodon/psyduck
cd psyduck
go build -o psyduck .
```

Or use the provided Dockerfile. Plugin loading uses `plugin.Open`, which
requires CGO and a matching host toolchain — the same Go version and module
graph as psyduck itself. In practice this means external plugins must be built
against the same psyduck commit.

## Getting started

Each `.psy` file is its own unit of configuration — resources and locals
declared in one file are only visible in that file. To reuse something from
another file, `import` it explicitly (see [`docs/hcl.md`](docs/hcl.md)).
Every command that operates on a pipeline takes the file directly.

### 1. Write a pipeline

Create `hello/main.psy`:

```hcl
produce "sequence" "src" {
  start      = 1
  stop-after = 3
}

transform "render" "greet" {
  engine = "template"
  decode = "bytes"
  format = "hello #{{.}}"
}

consume "file" "out" {
  location = "-"   # stdout
  sep      = "\n"
}

pipeline "hello" {
  produce   = [produce.sequence.src]
  transform = [transform.render.greet]
  consume   = [consume.file.out]
}
```

This uses only stdlib resources (`sequence`, `render`, `file`) — no plugin
fetch step is needed, but every file still needs an `init` before it can run
(see below).

### 2. Init, then run it

```sh
psyduck init hello/main.psy    # writes hello/main.lock
psyduck run hello/main.psy
# hello #1
# hello #2
# hello #3
```

`init` is required before `run` for every file, even one like this that uses
no external plugins — it's what writes the `.lock` file `run` reads.

### 3. Explore

```sh
psyduck list hello/main.psy           # list pipelines declared in the file
psyduck list --stat hello/main.psy    # + resource counts
psyduck show hello/main.psy           # print resource config
```

Flags go *before* the file argument, not after — `psyduck list hello/main.psy
--stat` errors with `unrecognized flag "--stat"` rather than running. Go's
flag parsing stops looking for flags at the first non-flag argument, so
anything flag-shaped typed after the file never reaches the flag parser at
all; psyduck checks for that explicitly and rejects it instead of silently
ignoring it.

### Using an external plugin

Add a `plugin {}` block referencing a git repo:

```hcl
plugin "amqp" {
  source = "https://github.com/psyduck-etl/amqp"
  tag    = "v0.1.0"  # optional
}

produce "amqp-queue" "in" {
  connection = "amqp://guest:guest@localhost:5672/"
  queue      = "work"
}
```

Then init the file — this clones + compiles the plugin, content-addresses the
built binary into `.psyduck/plugins/` (next to the file, keyed by a hash of
its own bytes), and writes the result to `path/to/<name>.lock`:

```sh
psyduck init path/to/pipeline.psy
psyduck run path/to/pipeline.psy
```

`init` reads plugin declarations (following any `import{}`s) in a cheap
pre-pass, so it works before any plugin is available. `run` reads the lock
file it wrote, re-verifying each plugin binary's hash before loading it —
if the store is missing a binary, or its content no longer matches what was
locked, `run` fails with a clear error rather than loading something
unexpected. `<name>.lock` is meant to be committed alongside `<name>.psy`;
`.psyduck/` (the binaries themselves) is not.

**`init` is always safe to re-run** — it always re-fetches, re-builds, and
rewrites the lock from scratch, so it's also how you recover from a
corrupted or partially-deleted `.psyduck/` store. Re-run it whenever:

- you add, remove, or edit a `plugin {}` block (directly, or in a file you
  `import`),
- you change a plugin's `tag`, or
- a plugin has no `tag` and you want to pick up whatever's new on its
  default branch — `init` records the exact ref it resolved (a branch, a
  tag, or a commit SHA) in the lock file's `ref` field, so you can always
  tell what a given `.lock` was actually built from, even without `tag`
  pinned.

## CLI

```
psyduck <command> <file>.psy [args]
```

| Command | Purpose |
|---|---|
| `run <file>` | Build and run every pipeline declared directly in the file (concurrently, if there's more than one). |
| `list [--stat] <file>` | List the file's pipelines by name. `--stat` adds `r<producers> x<transformers> c<consumers>` and an `r` flag when `produce-from` is used. |
| `show <file> [name ...]` | Print resource references and evaluated config for each pipeline. |
| `init <file>` | Fetch and compile every `plugin {}` reachable from the file (including through imports), content-address the built binaries into `.psyduck/`, and write `<file>.lock`. Required before `run`/`list`/`show` will work — see [above](#using-an-external-plugin). |
| `serve [--addr]` | Run the HTTP control/observability API as a long-running daemon (takes no file). Observe running pipelines, dispatch new ones, and expose JSON + Prometheus metrics. See [`docs/http-api.md`](docs/http-api.md). Single-instance today; peer-to-peer is a planned stage 2. |

Set `PSYDUCK_LOG_LEVEL` to `trace`/`debug`/`warn`/`error`/`fatal`/`panic` to
change runtime log verbosity.

## Docs

- [`docs/hcl.md`](docs/hcl.md) — writing `.psy` files: resources, refs,
  `locals`, `env`, `produce-from`, fan shapes.
- [`docs/stdlib.md`](docs/stdlib.md) — every built-in resource, the codec /
  framing / selection model, and the shape of `on-error`.
- [`docs/patterns.md`](docs/patterns.md) — idiomatic patterns: reshape,
  framing, meta-pipelines, composing pipelines through a transport.
- [`docs/plugins.md`](docs/plugins.md) — writing plugins in Go: the
  `sdk.Plugin` interface, `Spec` fields, `psy` struct tags, and how psyduck
  loads binaries.
- [`docs/http-api.md`](docs/http-api.md) — the `psyduck serve` HTTP API:
  observe running pipelines, dispatch new ones, and expose JSON + Prometheus
  metrics (single-instance; peer-to-peer is a planned stage 2).
- [`examples/`](examples/) — `.psy` fixtures exercised by the test suite, one
  file per example. `shared.psy` holds consumers reused across the others;
  files that want them declare their own `import { shared = "shared.psy" }`.

## Layout

| Package | Purpose |
|---|---|
| `github.com/psyduck-etl/sdk` | Format-agnostic plugin SDK (`Plugin`, `Resource`, `Spec`, `Instance`). |
| `parse` | `Parser`, `Pipeline`, `Resource`, `Source`, `Loader` — format-agnostic pipeline descriptions and file resolution. |
| `parse/hcl` | HCL/.psy implementation of `parse.Parser`, including `import{}` resolution. |
| `plugins` | `Store` — clones, builds, and content-addresses external plugins into `.psyduck/`; `Lock`/`ReadLock`/`WriteLock` for the per-file `.lock` format. |
| `stdlib` | The built-in plugin. Always loaded; no `plugin {}` block needed. |
| `core` | `BuildPipeline`, `RunPipeline` — turns a parsed pipeline into a running one. |
| `server` | HTTP control/observability API for `psyduck serve`; talks only to a `Supervisor` interface (see [`docs/http-api.md`](docs/http-api.md)). |
| `supervise` | Live `server.Supervisor`: parses, builds, and runs dispatched pipelines with `core`, and reports their status and stats. |
