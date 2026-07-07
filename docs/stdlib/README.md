# psyduck standard library

The standard library is the built-in plugin — no `plugin {}` block needed. Every
resource here is available in any `.psy` file.

Rather than one resource per format (`write-tar`, `gz-lines`, `base64-encode`,
…), the stdlib is a small set of **generic primitives** whose behaviour is
driven by a shared codec and framing system. `recode { decode = "gzip|json" }`
replaces a dozen encode/decode items; `pick { path = [...] }` and
`pick-map { fields = {...} }` cover field extraction and reshaping; one `file`
resource reads *and* writes.

## The data model

Under the hood every message is decoded into one of two shapes:

- **Continuous** — array-like data referenced by index or linear pattern:
  bytes, strings, lists, and encoded byte forms (base64, gzip, …).
- **Discrete** — struct-like data referenced by key: JSON objects.

Selection mirrors this split, and the rule is used everywhere a resource names a
value inside a message:

- **`path = ["a","b","0"]`** walks discrete/object data by key (numeric segments
  index into continuous data).
- **`by = "<jq>"`** selects continuous/linear data with a jq expression.

### Codecs (`decode` / `encode`)

Codec-aware transformers take `decode` and `encode` chain specs. A chain is
`|`-separated and applies left-to-right on decode, right-to-left on encode:

| Codec | Meaning |
|---|---|
| `bytes` | raw bytes (identity) |
| `utf-8` / `ascii` / `latin1` | text; invalid bytes error (see `on-error`) |
| `json` / `json-pretty` | JSON object/list/scalar |
| `csv` | CSV row(s) → list |
| `base64` / `base64-url` / `hex` / `url` | binary/text encodings |
| `gzip` | compression |

Example: `decode = "gzip|json"` gunzips then parses JSON; `encode = "json|gzip"`
serializes then compresses.

### Framing (transports)

Transports cut a byte stream into messages with mutually-exclusive separators
plus grouping:

| Attribute | Default | Meaning |
|---|---|---|
| `sep` | `"\n"` | string separator |
| `sep-byte` | `-1` | single byte 0..255 (`-1` = unset) |
| `sep-byte-index` | `0` | fixed chunk size in bytes (`0` = unset) |
| `group` | `0` | pieces per emitted message (`0`/`1` = one) |

Set `sep = ""` for whole-stream (one message). On write, the same options join
messages back together.

### Errors (`on-error`)

Codec-aware and text transformers take `on-error`: `"raise"` (default — surface
the error) or `"drop"` (swallow the failed message). The set is deliberately
small and open to more modes later.

---

## Producers

### Dev / testing

| Resource | Attributes |
|---|---|
| `constant` | `value`, `stop-after` |
| `sequence` | `start`, `step`, `stop-after` — arithmetic integer sequence |
| `generate` | `values` (list), `loop`, `stop-after` — emit literals |
| `ticker` | `interval-ms`, `format` (`unix`/`unix-ms`/`rfc3339`), `stop-after` |

### Transports

| Resource | Roles | Key attributes |
|---|---|---|
| `file` | produce + consume | `location` (path, `-` stdin/stdout, `--` stderr, or a socket URI), `follow` (tail), `append`, `create`, + framing |
| `socket` | produce + consume | `location` (`tcp://`/`udp://`/`unix://`), `create`, + framing |
| `listen` | produce | `location` (`tcp://`/`unix://`/`udp://`), `create`, + framing |
| `request` | produce + consume | `url`, `method`, `headers`, `body`, `query-params`, `basic-auth`, `timeout-ms`, `success-codes`, `interval-ms` |
| `http-listen` | produce | `address`, `path`, `method`, `status`, `reply` |

`produce "file" {}` reads; `consume "file" {}` writes — you write files the way
you read them, and POST the way you GET.

---

## Consumers

`trash` discards everything. Writing is done by the dual-role transports above
(`file`, `socket`, `request`).

---

## Transformers

### Shape (codec-aware)

| Resource | Attributes | Purpose |
|---|---|---|
| `recode` | `decode`, `encode` | universal format converter |
| `pick` | `path` \| `by`, `decode`, `encode` | extract one value |
| `pick-map` | `fields = { dst = [src,path] }` | reshape into a new object |
| `set` | `values = { field = literal }` | add/overwrite fields |
| `drop` | `fields = [..]` | remove fields |
| `slice` | `start`, `stop`, `step` | sub-range of continuous data |
| `chunk` | `size`, `keep-tail` | fixed windows → list |
| `every` | `step`, `size` | sliding windows → list |
| `render` | `engine` (`template`/`printf`/`jq`), `format` | format a message |

### jq escape hatches

| Resource | Attributes |
|---|---|
| `jq` | `expression` — full reshape |
| `filter` | `expression` — drop when false/null |

### Keyed

| Resource | Attributes |
|---|---|
| `dedupe` | `by` \| `path`, `window` |
| `uniq` | `by` \| `path` — drop consecutive duplicates |
| `batch` | `size` — collect into a JSON array |

### Flow control

| Resource | Attributes |
|---|---|
| `wait` | `milliseconds` |
| `throttle` | `per-second` |
| `head` | `count` |
| `tail` | `skip` |
| `sample` | `rate` |

### Text (string-domain)

`split`, `join`, `replace`, `regex`, `trim`, `upper`, `lower`, `hash`. All take a
string `decode` (default `utf-8`); rune-vs-byte behaviour is the codec's
contract, and invalid bytes follow `on-error`.

### Dev / testing

`inspect` (`prefix`, `output`), `assert` (`expression`, `message`),
`count` (`every`, `prefix`).

---

## Example: data → local socket → meta-producer

One pipeline generates producer HCL and writes it to a unix socket; another
listens on that socket and uses the stream as its `produce-from` description.
Many writers can fan into one listener.

```hcl
# generator: turn a sequence into produce {} blocks, write to a socket
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
}

pipeline "config-gen" {
  produce   = [produce.sequence.pages]
  transform = [transform.render.cfg]
  consume   = [consume.socket.meta]
}

# consumer: read configs off the socket, run them as producers
produce "listen" "meta-in" {
  location = "unix:///tmp/psyduck-meta.sock"
  create   = true
}

consume "file" "results" { location = "results.jsonl" }

pipeline "scrape" {
  produce-from = produce.listen.meta-in
  consume      = [consume.file.results]
}
```
