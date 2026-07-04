# psyduck standard library

The psyduck standard library is the built-in plugin — no `plugin {}` block
needed. Every resource listed here is available in any `.psy` file without
configuration.

## Quick reference

| Kind | Resource | Purpose |
|---|---|---|
| produce | [`constant`](#constant) | emit a fixed string repeatedly |
| produce | [`increment`](#increment) | emit an increasing integer counter |
| produce | [`stdin`](#stdin) | read lines from standard input |
| produce | [`file-lines`](#file-lines) | read a text file line-by-line |
| produce | [`file-bytes`](#file-bytes) | read a file in fixed-size chunks |
| produce | [`gz-lines`](#gz-lines) | read lines from a gzip-compressed file |
| produce | [`zip-lines`](#zip-lines) | read lines from entries inside a ZIP archive |
| produce | [`tar-lines`](#tar-lines) | read lines from entries inside a tar archive |
| produce | [`cmd`](#cmd-producer) | run a command and stream its output |
| produce | [`http-poll`](#http-poll) | repeatedly HTTP-fetch a URL |
| produce | [`http-server`](#http-server) | receive HTTP request bodies |
| produce | [`tcp-read`](#tcp-read) | connect to a TCP server and read lines |
| produce | [`tcp-listen`](#tcp-listen) | accept TCP connections and read lines |
| produce | [`udp-listen`](#udp-listen) | receive UDP datagrams |
| consume | [`trash`](#trash) | discard all messages |
| consume | [`stdout`](#stdout) | write messages to standard output |
| consume | [`stderr`](#stderr) | write messages to standard error |
| consume | [`file-write`](#file-write) | write messages to a file |
| consume | [`gz-write`](#gz-write) | write messages into a gzip-compressed file |
| consume | [`tar-write`](#tar-write) | write messages as entries in a tar archive |
| consume | [`cmd`](#cmd-consumer) | feed each message to a command via stdin |
| consume | [`http-post`](#http-post) | HTTP-send each message to a URL |
| consume | [`tcp-write`](#tcp-write) | write messages to a TCP server |
| consume | [`udp-write`](#udp-write) | send each message as a UDP datagram |
| transform | [`inspect`](#inspect) | log the raw message for debugging |
| transform | [`snippet`](#snippet) | truncate long JSON string fields |
| transform | [`sprintf`](#sprintf) | format the message with a printf-style template |
| transform | [`transpose`](#transpose) | remap JSON fields |
| transform | [`zoom`](#zoom) | replace the message with a nested JSON field |
| transform | [`wait`](#wait) | sleep between messages |
| transform | [`filter`](#filter) | drop messages that fail a jq predicate |
| transform | [`jq`](#jq) | transform messages with a jq expression |
| transform | [`template`](#template) | render a Go text/template |
| transform | [`set-field`](#set-field) | add or overwrite a JSON field with a computed value |
| transform | [`dedupe`](#dedupe) | drop duplicate messages within a sliding window |
| transform | [`gzip-compress`](#gzip-compress) | compress message bytes with gzip |
| transform | [`gzip-decompress`](#gzip-decompress) | decompress gzip-compressed message bytes |
| transform | [`base64-encode`](#base64-encode) | base64-encode message bytes |
| transform | [`base64-decode`](#base64-decode) | base64-decode message bytes |

---

## Producers

### `constant`

Emits a single fixed string on a loop. Combine with `stop-after` to produce
a finite number of identical messages.

```hcl
produce "constant" "tick" {
  value      = "ping"
  stop-after = 5
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `value` | string | — | `"0"` | string to emit on every iteration |
| `stop-after` | int | — | `0` | stop after n messages (0 = run forever) |

---

### `increment`

Emits an ever-increasing decimal integer (`0`, `1`, `2`, …) as a string.

```hcl
produce "increment" "counter" {
  stop-after = 100
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `stop-after` | int | — | `0` | stop after n messages (0 = run forever) |

---

### `stdin`

Reads lines from standard input. Useful for piping data into a pipeline.

```hcl
produce "stdin" "input" {}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `skip-empty` | bool | — | `false` | silently drop blank lines |

---

### `file-lines`

Reads a text file one line per message. With `follow = true` the producer
behaves like `tail -f` — it keeps reading as the file grows.

```hcl
produce "file-lines" "events" {
  path       = "/var/log/app.log"
  follow     = true
  skip-empty = true
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | path to the text file |
| `follow` | bool | — | `false` | tail the file — keep reading after EOF |
| `skip-empty` | bool | — | `false` | silently drop blank lines |

---

### `file-bytes`

Reads a file in fixed-size byte chunks. Each message is at most `chunk-size`
bytes. Useful for streaming binary files or large blobs.

```hcl
produce "file-bytes" "blob" {
  path       = "/data/dump.bin"
  chunk-size = 8192
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | path to the file |
| `chunk-size` | int | — | `4096` | maximum bytes per message |

---

### `gz-lines`

Decompresses a `.gz` file on-the-fly and emits its lines one per message.

```hcl
produce "gz-lines" "compressed-log" {
  path = "/data/archive.log.gz"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | path to the `.gz` file |
| `skip-empty` | bool | — | `false` | silently drop blank lines |

---

### `zip-lines`

Opens a ZIP archive and reads lines from every matching entry. Entries are
processed in the order they appear in the archive's directory.

```hcl
produce "zip-lines" "csvs" {
  path       = "/data/batch.zip"
  match      = "*.csv"
  skip-empty = true
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | path to the `.zip` file |
| `match` | string | — | `"*"` | glob pattern to select archive entries |
| `skip-empty` | bool | — | `false` | silently drop blank lines |

---

### `tar-lines`

Opens a tar archive (optionally compressed) and reads lines from every
matching entry. Set `compression = "auto"` to detect gzip or bzip2 by
the file header.

```hcl
produce "tar-lines" "logs" {
  path        = "/data/logs.tar.gz"
  match       = "*.log"
  compression = "auto"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | path to the tar archive |
| `match` | string | — | `"*"` | glob pattern to select archive entries |
| `compression` | string | — | `"auto"` | `none`, `gz`, `bz2`, or `auto` |
| `skip-empty` | bool | — | `false` | silently drop blank lines |

---

### `cmd` (producer) {#cmd-producer}

Runs a command and streams its output. With `split-lines = true` (the
default) each output line becomes one message; with `split-lines = false`
the entire stdout is emitted as a single message.

```hcl
produce "cmd" "ps" {
  command     = "ps"
  args        = ["aux"]
  split-lines = true
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `command` | string | ✓ | — | executable to run |
| `args` | list(string) | — | `[]` | command-line arguments |
| `split-lines` | bool | — | `true` | emit one message per output line |

---

### `http-poll`

Issues an HTTP request in a loop and emits each response body as a message.
Use `interval-ms` for simple throttling, or the pipeline `per-minute` meta
attribute for rate limiting.

```hcl
produce "http-poll" "metrics" {
  url         = "https://api.example.com/metrics"
  method      = "GET"
  headers     = { Authorization = "******" }
  interval-ms = 5000
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `url` | string | ✓ | — | URL to request |
| `method` | string | — | `"GET"` | HTTP method |
| `headers` | map(string) | — | `{}` | request headers |
| `body` | string | — | `""` | request body |
| `interval-ms` | int | — | `0` | sleep between requests in milliseconds (0 = no sleep) |

---

### `http-server`

Starts an HTTP server and emits each incoming request body as a message.
The server runs until the pipeline stops.

```hcl
produce "http-server" "webhook" {
  address = ":9000"
  path    = "/events"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `address` | string | — | `":8080"` | `host:port` to listen on |
| `path` | string | — | `"/"` | URL path to handle |

---

### `tcp-read`

Connects to a remote TCP server and reads lines from the connection.

```hcl
produce "tcp-read" "stream" {
  address    = "logs.internal:5000"
  skip-empty = true
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `address` | string | ✓ | — | `host:port` to connect to |
| `skip-empty` | bool | — | `false` | silently drop blank lines |

---

### `tcp-listen`

Binds a TCP server port. Accepts one connection at a time and reads lines
from it; moves to the next connection after the current one closes.

```hcl
produce "tcp-listen" "intake" {
  address = ":5514"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `address` | string | ✓ | — | `host:port` to listen on |
| `skip-empty` | bool | — | `false` | silently drop blank lines |

---

### `udp-listen`

Binds a UDP port. Each datagram becomes one message.

```hcl
produce "udp-listen" "stats" {
  address = ":8125"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `address` | string | ✓ | — | `host:port` to listen on |

---

## Consumers

### `trash`

Silently discards every message. Useful for benchmarking or when side-effects
live entirely in the transform stack.

```hcl
consume "trash" "discard" {}
```

No configurable attributes.

---

### `stdout`

Writes each message to standard output, optionally with a delimiter appended
after each one.

```hcl
consume "stdout" "out" {
  delimiter = "\n"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `delimiter` | string | — | `"\n"` | appended after each message |

---

### `stderr`

Same as `stdout` but writes to standard error.

```hcl
consume "stderr" "log" {}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `delimiter` | string | — | `"\n"` | appended after each message |

---

### `file-write`

Writes each message to a file. By default the file is truncated on open;
set `append = true` to accumulate across runs.

```hcl
consume "file-write" "sink" {
  path      = "/data/output.txt"
  append    = false
  delimiter = "\n"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | destination file path |
| `append` | bool | — | `false` | append to existing file instead of truncating |
| `delimiter` | string | — | `"\n"` | appended after each message |

---

### `gz-write`

Compresses messages with gzip and writes them to a file.

```hcl
consume "gz-write" "compressed" {
  path  = "/data/output.gz"
  level = 6
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | destination `.gz` file path |
| `level` | int | — | `0` | compression level 1–9 (0 = gzip default) |
| `delimiter` | string | — | `"\n"` | appended after each compressed message |

---

### `tar-write`

Writes each message as a separate entry in a tar archive. When
`entry-name-field` is set, the named JSON field's value is used as the
entry filename. Without it entries are auto-named sequentially
(`entry-000000`, `entry-000001`, …).

```hcl
consume "tar-write" "archive" {
  path             = "/data/batch.tar.gz"
  compression      = "gz"
  entry-name-field = "filename"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `path` | string | ✓ | — | destination tar archive path |
| `compression` | string | — | `"none"` | `none` or `gz` |
| `entry-name-field` | string | — | `""` | JSON field to use as the entry filename |

---

### `cmd` (consumer) {#cmd-consumer}

Starts the command fresh for each message, passing the message on its
standard input.

```hcl
consume "cmd" "process" {
  command   = "jq"
  args      = ["."]
  delimiter = "\n"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `command` | string | ✓ | — | executable to run per message |
| `args` | list(string) | — | `[]` | command-line arguments |
| `delimiter` | string | — | `"\n"` | appended after the message on stdin |

---

### `http-post`

Sends each message as the body of an HTTP request.

```hcl
consume "http-post" "ingest" {
  url          = "https://ingest.example.com/events"
  method       = "POST"
  headers      = { Authorization = "******" }
  content-type = "application/json"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `url` | string | ✓ | — | destination URL |
| `method` | string | — | `"POST"` | HTTP method |
| `headers` | map(string) | — | `{}` | additional request headers |
| `content-type` | string | — | `"application/octet-stream"` | `Content-Type` header |

---

### `tcp-write`

Opens a TCP connection and writes each message to it. Reconnects if the
connection is lost.

```hcl
consume "tcp-write" "forwarder" {
  address   = "logstash.internal:5000"
  delimiter = "\n"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `address` | string | ✓ | — | `host:port` to connect to |
| `delimiter` | string | — | `"\n"` | appended after each message |

---

### `udp-write`

Sends each message as a UDP datagram.

```hcl
consume "udp-write" "statsd" {
  address = "localhost:8125"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `address` | string | ✓ | — | `host:port` to send to |

---

## Transformers

### `inspect`

Logs the raw message to stderr for debugging. The message passes through
unchanged.

```hcl
transform "inspect" "debug" {
  be-string = true
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `be-string` | bool | — | `true` | interpret the bytes as a UTF-8 string when logging |

---

### `snippet`

Truncates long string fields inside a JSON message so that log output stays
readable. The truncation is applied only for display — use with `inspect`.

```hcl
transform "snippet" "trim" {
  fields = ["body", "payload"]
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `fields` | list(string) | ✓ | — | JSON field names to truncate |

---

### `sprintf`

Formats the raw message bytes using a `fmt.Sprintf`-style format string.
The result can optionally be re-encoded.

```hcl
transform "sprintf" "wrap" {
  format   = "event: %s"
  encoding = "bytes"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `format` | string | ✓ | — | `fmt.Sprintf` format string (message bytes are the single argument) |
| `encoding` | string | — | `"bytes"` | output encoding; `"bytes"` or `"string"` |

---

### `transpose`

Remaps JSON fields. Each entry in `fields` maps a target field name to a
source path, described as a list of strings (a JSON pointer-style path).

```hcl
transform "transpose" "remap" {
  fields = {
    user_id  = ["data", "userId"]
    event    = ["type"]
  }
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `fields` | map(list(string)) | ✓ | — | mapping of target field → source path (list of keys) |

---

### `zoom`

Replaces the entire message with the value of a single nested JSON field.
Useful for unwrapping envelope formats.

```hcl
transform "zoom" "unwrap" {
  field = "payload"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `field` | string | ✓ | — | JSON field name to extract |

---

### `wait`

Sleeps for a fixed number of milliseconds before passing the message
downstream. Useful for rate-limiting without using `per-minute`.

```hcl
transform "wait" "throttle" {
  milliseconds = 500
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `milliseconds` | int | ✓ | — | duration to sleep in milliseconds |

---

### `filter`

Evaluates a jq expression against each message. The message is passed
through only when the result is a truthy value (anything other than `false`
or `null`).

```hcl
transform "filter" "only-errors" {
  expression = ".level == \"error\""
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `expression` | string | ✓ | — | jq expression; message is dropped when result is `false` or `null` |

---

### `jq`

Transforms the message by applying a jq expression. The expression receives
the message as its input (decoded from JSON if possible; raw bytes otherwise).
If the expression produces no output the message is dropped. String outputs
are emitted as plain bytes; all other types are JSON-encoded.

```hcl
transform "jq" "extract" {
  expression = "{id: .user.id, ts: .timestamp}"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `expression` | string | ✓ | — | jq expression to apply |

---

### `template`

Renders a [Go `text/template`](https://pkg.go.dev/text/template) string
against the message. If the message is valid JSON it is decoded first, so
fields are accessible as `{{.fieldName}}`. Non-JSON messages are passed as a
plain string available via `{{.}}`.

```hcl
transform "template" "format" {
  format = "{{.user}} logged in at {{.timestamp}}"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `format` | string | ✓ | — | Go `text/template` string |

---

### `set-field`

Evaluates a jq expression and writes the result into a named JSON field.
The message must be a JSON object. Existing fields are overwritten.

```hcl
transform "set-field" "add-timestamp" {
  field      = "ingest_at"
  expression = "now | todate"
}
```

Common expressions:

| Goal | Expression |
|---|---|
| ISO 8601 timestamp | `"now \| todate"` |
| Unix epoch (float) | `"now"` |
| Double an existing field | `".count * 2"` |
| Static string | `"\"my-service\""` |

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `field` | string | ✓ | — | JSON field to create or overwrite |
| `expression` | string | ✓ | — | jq expression whose result becomes the field value |

---

### `dedupe`

Drops duplicate messages within a sliding window. The deduplication key is
computed by a jq expression (`by`); the default `"."` uses the whole message
as the key.

```hcl
transform "dedupe" "unique" {
  by     = ".id"
  window = 50000
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `by` | string | — | `"."` | jq expression to compute the deduplication key |
| `window` | int | — | `10000` | number of recent keys to remember |

---

### `gzip-compress`

Compresses each message with gzip. Use with `gz-write` or `base64-encode`
for compressed transport.

```hcl
transform "gzip-compress" "pack" {
  level = 6
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `level` | int | — | `0` | compression level 1–9 (0 = gzip default) |

---

### `gzip-decompress`

Decompresses gzip-encoded message bytes. Errors if the input is not valid
gzip data.

```hcl
transform "gzip-decompress" "unpack" {}
```

No configurable attributes.

---

### `base64-encode`

Base64-encodes each message.

```hcl
transform "base64-encode" "enc" {
  encoding = "std"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `encoding` | string | — | `"std"` | alphabet: `std`, `url`, `raw-std`, or `raw-url` |

> `raw-*` variants omit padding (`=`) characters. `url` uses the
> URL-safe alphabet (`-` and `_` instead of `+` and `/`).

---

### `base64-decode`

Decodes a base64-encoded message. The `encoding` must match the encoding
that was used.

```hcl
transform "base64-decode" "dec" {
  encoding = "std"
}
```

| Attribute | Type | Required | Default | Description |
|---|---|---|---|---|
| `encoding` | string | — | `"std"` | alphabet: `std`, `url`, `raw-std`, or `raw-url` |

---

## Worked examples

### Filter and forward logs over TCP

```hcl
produce "file-lines" "logs" {
  path   = "/var/log/app.log"
  follow = true
}

transform "filter" "errors-only" {
  expression = ".level == \"error\""
}

transform "set-field" "add-host" {
  field      = "host"
  expression = "\"${env.HOSTNAME}\""
}

consume "tcp-write" "logstash" {
  address = "logstash.internal:5044"
}

pipeline "forward-errors" {
  produce    = [produce.file-lines.logs]
  transform  = [transform.filter.errors-only, transform.set-field.add-host]
  consume    = [consume.tcp-write.logstash]
}
```

### HTTP webhook → deduplicated JSON file

```hcl
produce "http-server" "hook" {
  address = ":9000"
  path    = "/events"
}

transform "jq" "normalize" {
  expression = "{id: .eventId, type: .eventType, ts: .createdAt}"
}

transform "dedupe" "no-dups" {
  by     = ".id"
  window = 100000
}

consume "file-write" "sink" {
  path = "/data/events.jsonl"
}

pipeline "ingest" {
  produce   = [produce.http-server.hook]
  transform = [transform.jq.normalize, transform.dedupe.no-dups]
  consume   = [consume.file-write.sink]
}
```

### Archive log lines as gzip tar

```hcl
produce "file-lines" "source" {
  path = "/var/log/access.log"
}

transform "set-field" "add-name" {
  field      = "filename"
  expression = "\"access.log\""
}

consume "tar-write" "archive" {
  path             = "/backup/access.tar.gz"
  compression      = "gz"
  entry-name-field = "filename"
}

pipeline "archive" {
  produce   = [produce.file-lines.source]
  transform = [transform.set-field.add-name]
  consume   = [consume.tar-write.archive]
}
```
