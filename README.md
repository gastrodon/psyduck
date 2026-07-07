# psyduck

psyduck is an extensible ETL engine. Pipelines are described in `.psy` files
(HCL syntax), built from a small standard library of primitives, and can be
extended with Go plugins that are cloned and compiled on demand.

- **Configure it**: producers → transformers → consumers, wired declaratively.
- **Extend it**: any `sdk.Plugin` can be published as a Git repo and referenced
  from a pipeline with a `plugin {}` block.
- **Run it**: `psyduck run <pipeline-name>`.

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

A pipeline workspace is a directory of `.psy` files. All files in the directory
parse together: resources declared in one file can be referenced from any
other. Each pipeline is run by name.

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
fetch step is needed.

### 2. Run it

```sh
psyduck --chdir hello run hello
# hello #1
# hello #2
# hello #3
```

### 3. Explore

```sh
psyduck --chdir hello list          # list pipelines by name
psyduck --chdir hello list --stat   # + resource counts
psyduck --chdir hello show hello    # print resource config
```

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

Then run once to clone + compile the plugin into `.psyduck/plugins/`:

```sh
psyduck --chdir . init
psyduck --chdir . run <pipeline-name>
```

`init` reads plugin declarations in a cheap pre-pass, so it works before any
plugin is available. Its output — the manifest at `.psyduck/plugin.json` and
the `.so` binaries — is loaded automatically on the next `run`.

## CLI

```
psyduck [--chdir DIR] <command> [args]
```

| Command | Purpose |
|---|---|
| `run <name>` | Build and run a pipeline. |
| `list [--stat]` | List pipelines by name. `--stat` adds `r<producers> x<transformers> c<consumers>` and an `r` flag when `produce-from` is used. |
| `show [name ...]` | Print resource references and evaluated config for each pipeline. |
| `init` | Fetch and compile every `plugin {}` declared in the workspace. |

`--chdir` selects the workspace directory (default `.`). Set
`PSYDUCK_LOG_LEVEL` to `trace`/`debug`/`warn`/`error`/`fatal`/`panic` to change
runtime log verbosity.

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
- [`examples/`](examples/) — self-contained `.psy` fixtures exercised by the
  test suite. Each `examples/<name>/main.psy` is one pipeline demonstrating a
  specific stdlib feature.

## Layout

| Package | Purpose |
|---|---|
| `github.com/psyduck-etl/sdk` | Format-agnostic plugin SDK (`Plugin`, `Resource`, `Spec`, `Instance`). |
| `parse` | `Parser`, `Pipeline`, `Resource`, `Source` — format-agnostic pipeline descriptions. |
| `parse/hcl` | HCL/.psy implementation of `parse.Parser`. |
| `plugins` | `Store` — clones, builds, and loads external plugins into `.psyduck/`. |
| `stdlib` | The built-in plugin. Always loaded; no `plugin {}` block needed. |
| `core` | `BuildPipeline`, `RunPipeline` — turns a parsed pipeline into a running one. |
