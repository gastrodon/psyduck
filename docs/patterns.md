# Idiomatic patterns

Small recipes for writing `.psy` files that read well. Each pattern is a
distilled version of something in [`../examples/`](../examples/). See also
[hcl.md](hcl.md) for the language and [stdlib.md](stdlib.md) for the resource
reference.

## Extract, then annotate

`pick-map` and `set` compose: reshape into the fields you want, then bolt on
static context. `drop` peels back off if you have too much.

```hcl
transform "pick-map" "core" {
  fields = { "name" = ["user", "name"], "id" = ["user", "id"] }
}

transform "set" "tag" {
  values = { "source" = "e2e", "batch" = "nightly" }
}

pipeline "extract" {
  produce   = [produce.request.api]
  transform = [transform.pick-map.core, transform.set.tag]
  consume   = [consume.file.out]
}
```

Rule of thumb: `pick-map` for the reshape, `set`/`drop` for the annotation
sweep afterwards. If you find yourself reaching for `jq` here, you probably
want `pick-map` — save `jq` for computed values.

## Filter first, then reshape

Drop what you don't care about before spending work on it. `filter` runs
before `jq`; the pipeline is shorter and downstream stages see less traffic.

```hcl
transform "filter" "keep" { expression = ".keep" }
transform "jq"     "shape" { expression = ".v" }

pipeline "gated" {
  produce   = [produce.generate.src]
  transform = [transform.filter.keep, transform.jq.shape]
  consume   = [consume.file.out]
}
```

## Selection: `path` vs `by`

Every codec-aware selection takes one of `path = [...]` (discrete, by key)
or `by = "..."` (continuous, jq expression). They aren't interchangeable —
they encode the two data shapes:

- `path = ["user", "name"]` — struct-like navigation. Numeric segments
  index into continuous data (bytes, lists).
- `by = ".user.name | ascii_downcase"` — computed, expression-language
  selection over decoded JSON.

Reach for `path` first: it's cheaper, self-documenting, and error-proof.
Fall through to `by` when you need arithmetic, string ops, or predicates.
The same duality shows up in `pick`, `dedupe`, and `uniq`.

## Codec chains

`recode`, `pick`, `pick-map`, `set`, `drop`, `slice`, `chunk`, `every`, and
`render` all take `decode` and `encode`. They read left-to-right on decode,
right-to-left on encode:

```hcl
transform "recode" "unzip-parse" { decode = "gzip|json", encode = "json" }
transform "recode" "serialize"   { decode = "json",      encode = "json|gzip" }
```

The identity codec is `bytes`. `encode = "bytes"` on `pick` emits the leaf
value unquoted — useful when the next stage is a text transport:

```hcl
transform "pick" "name" {
  path   = ["user", "name"]
  encode = "bytes"           # "ann", not "\"ann\""
}
```

## Framing decisions

Transports carry a byte stream; framing carves it into messages. You must set
exactly one of `sep`, `sep-byte`, or `sep-byte-index`. The `group` option can
layer on top of any of them:

| You want | Set |
|---|---|
| Newline-delimited records | `sep = "\n"` |
| Whole-stream as one message | `sep = ""` |
| Fixed-size binary records | `sep-byte-index = N` |
| Split on a single byte (e.g. 0x00) | `sep-byte = 0` |
| Emit windows of records | `group = N` (with any of the above) |

Framing on `consume` is symmetric: whatever separator split incoming data
also joins outgoing data. Reading and writing the same file with the same
resource round-trips.

## Dual-role transports

`file`, `socket`, and `request` are all producers *and* consumers. The verb
you use decides which role; the attributes are the same.

```hcl
produce "file" "src" { location = env.PSYDUCK_IN  }  # reads
consume "file" "out" { location = env.PSYDUCK_OUT }  # writes

# HTTP: producer polls; consumer POSTs.
produce "request" "poll" { url = env.API_URL  interval-ms = 5000 }
consume "request" "post" { url = env.API_URL  method       = "POST" }
```

Two pipelines sharing a transport is how you turn one-shot flows into
continuous ones. Read the way you write, and post the way you get.

## Composing pipelines through a transport

Any transport can be the seam between two pipelines. One writes messages
into it, another reads them out. Buffers, back-pressure, and lifetime
become the transport's job — not yours.

```hcl
consume "socket" "bus" {
  location = "unix:///tmp/psyduck-bus.sock"
  create   = true
  sep      = "\n"
}

produce "socket" "bus" {
  location = "unix:///tmp/psyduck-bus.sock"
  create   = true
  sep      = "\n"
}

pipeline "ingest" {                  # readers of raw data → bus
  produce = [produce.http-listen.hook]
  consume = [consume.socket.bus]
}

pipeline "process" {                 # bus → transforms → sink
  produce   = [produce.socket.bus]
  transform = [transform.jq.shape, transform.set.tag]
  consume   = [consume.file.out]
}
```

Same idea works with `file` (persistent log), `request` (HTTP fan-out), or a
plugin transport.

## Meta-pipelines

`produce-from` reads producer configurations off *another* producer, then
runs them. Combine with `render` + a transport to emit new work into the
system while it runs:

```hcl
# generator: turn each page index into a produce {} block, write to a socket
produce "sequence" "pages" { stop-after = 50 }

transform "render" "cfg" {
  engine = "template"
  decode = "bytes"
  format = <<-EOF
  produce "request" "page" {
    url        = "https://api.example.com/items?page={{.}}"
    stop-after = 1
  }
  EOF
}

consume "socket" "meta" {
  location = "unix:///tmp/psyduck-meta.sock"
  create   = true
  sep      = "\n"
}

pipeline "plan" {
  produce   = [produce.sequence.pages]
  transform = [transform.render.cfg]
  consume   = [consume.socket.meta]
}

# consumer: read configs off the socket, run them as producers
produce "listen" "meta-in" {
  location = "unix:///tmp/psyduck-meta.sock"
  create   = true
  sep      = "\n"
}

consume "file" "results" {
  location = "results.jsonl"
  sep      = "\n"
}

pipeline "scrape" {
  produce-from       = produce.listen.meta-in
  parallel-producers = 5
  consume            = [consume.file.results]
}
```

`plan` writes; `scrape` executes. Many `plan`-like writers can fan into a
single `scrape` listener, which keeps listening — every new config that
arrives becomes another producer, for as long as `scrape` runs.
`parallel-producers` caps how many of those producers run at once (waves of
5 in the example; 0 means unbounded, the default). This is how you get
dynamic parallel producers without recompiling anything.

## Rendering messages

`render` has three engines, each best for a different shape:

- **`template`** — Go `text/template` over decoded JSON. Named-field access
  (`{{.user.name}}`), branches, ranges. The go-to for anything structured.
- **`printf`** — Go `fmt` verbs. Compact for scalars and lists.
- **`jq`** — jq expression that returns a string. Reach for it when you need
  computed values or when you're already thinking in jq.

```hcl
transform "render" "line" {
  engine = "template"
  format = "{{.user.name}} <{{.user.email}}>"
}
```

Combine with `encode = "bytes"` (default) to emit plain text.

## Windows and batching

Three ways to group messages, for three different needs:

| Resource | Groups | When |
|---|---|---|
| `chunk` | Fixed windows over *continuous* data | Slicing bytes / a list into equal pieces. |
| `every`  | Sliding windows over continuous data | Overlapping views, moving averages. |
| `batch`  | N *messages* into one JSON array | Bulk-writing to an API or queue. |

`chunk` and `every` are codec-aware (they walk your `decode`). `batch` is
message-scoped: it counts inputs, regardless of their shape.

## Flow control layering

Rate and count controls sit at two levels. Prefer the block-level ones
when they express what you mean — they're free at the resource site:

- On any resource block: `stop-after = N` (host-owned, works everywhere).
- On any resource block: `per-minute = N` (host-owned rate limit).
- As transformers: `head`, `tail`, `sample`, `throttle`, `wait`.

Rule of thumb: use `stop-after` and `per-minute` to bound *sources* and
*sinks*; use the transformer forms mid-pipeline where the shape of the
stream matters (e.g. `throttle` before a downstream API, `head` to trim
after a filter).

## Text pipelines stack

Text transformers (`trim`, `replace`, `regex`, `upper`, `lower`, `split`,
`join`, `hash`) live in the string domain. They compose cleanly as a stack:

```hcl
transform "trim"    "t" {}
transform "replace" "r" { old = " "  new = "_" }
transform "upper"   "u" {}

pipeline "normalize" {
  produce   = [produce.file.src]
  transform = [transform.trim.t, transform.replace.r, transform.upper.u]
  consume   = [consume.file.out]
}
```

`decode` defaults to `utf-8`; set it explicitly (`ascii`, `latin1`, `bytes`)
if you need different rune semantics. Invalid inputs follow `on-error`.

## Assertions as tripwires

`assert` passes messages through unchanged when a jq predicate holds, and
errors the pipeline otherwise. Drop one into a test pipeline (or any
pipeline with `exit-on-error = true`) to catch regressions where and when
they happen.

```hcl
transform "assert" "shape" {
  expression = "(.id | type) == \"number\" and (.name | length) > 0"
  message    = "malformed user record"
}
```

Combine with `inspect` during debugging: `inspect` before, `assert` after —
you see the message that fell through, then the pipeline stops.

## Sharing config with `locals`

When the same value appears in more than one block, hoist it into `locals`.
`env.*` reads through unchanged, so environment overrides still work:

```hcl
locals {
  bus = "unix:///tmp/${env.RUN_ID}-bus.sock"
}

consume "socket" "in"  { location = local.bus, create = true }
produce "socket" "out" { location = local.bus, create = true }
```

One place to edit; every reader/writer sees the change.
