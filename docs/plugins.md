# Writing psyduck plugins

A plugin is a Go module that exports `func Plugin() sdk.Plugin`. psyduck
clones the module at `psyduck init`, compiles it with `go build -buildmode=plugin`,
and opens the resulting `.so` at run time. Once loaded, every resource the
plugin declares becomes usable from `.psy` files just like a stdlib resource.

The public interface is the `github.com/psyduck-etl/sdk` package. Nothing in
`github.com/gastrodon/psyduck` is imported by plugin authors — the host and
the plugin only meet through the SDK.

## The shape of a plugin

```go
package main

import "github.com/psyduck-etl/sdk"

func Plugin() sdk.Plugin {
    return sdk.NewInProc("hello",
        &sdk.Resource{
            Name:            "greet",
            Kinds:           sdk.PRODUCER,
            ProvideProducer: greet,
            Spec: []*sdk.Spec{
                {Name: "name", Description: "who to greet", Type: sdk.TypeString, Default: "world"},
            },
        },
    )
}

type greetConfig struct {
    Name string `psy:"name"`
}

func greet(parse sdk.Parser) (sdk.Producer, error) {
    cfg := new(greetConfig)
    if err := parse(cfg); err != nil {
        return nil, err
    }

    msg := []byte("hello " + cfg.Name)
    return func(send chan<- []byte, errs chan<- error) {
        defer close(send)
        defer close(errs)
        for {
            send <- msg
        }
    }, nil
}
```

Note the producer loops forever. Bounding it is the user's job at the call
site, via the host-owned `stop-after` meta attribute — no plugin support
needed:

```hcl
produce "greet" "hi" {
  name       = "world"
  stop-after = 5      # enforced by the host
}
```

Requirements — enforced by `go build -buildmode=plugin`:

- Package must be `main`.
- The exported symbol must be `Plugin` with signature `func() sdk.Plugin`.
- The plugin's `go.mod` should require the same versions of the SDK, HCL,
  logrus, and any other host-shared modules as the host binary. Mismatched
  versions cause `plugin.Open` to fail at load time.

## The SDK interfaces

### `sdk.Plugin`

```go
type Plugin interface {
    Name() string
    Resources() []ResourceDescriptor
    Bind(kind Kind, resource string, block ConfigBlock) (Instance, error)
}
```

`sdk.NewInProc(name, resources...)` builds an in-process `Plugin` from a name
and a set of `*Resource`s. It handles the `Bind` kind switch on your behalf,
so a plugin author never writes that dispatch by hand.

If a resource declares multiple `Kinds` (e.g. `PRODUCER | CONSUMER`), each
`ProvideProducer` / `ProvideConsumer` / `ProvideTransformer` field must be
set to a corresponding factory. Setting a `ProvideFoo` closure without listing
the matching `Kind` — or vice versa — is a programmer error.

### `sdk.Resource` and `sdk.Spec`

A `Resource` is the closure-carrying struct plugin authors write:

```go
type Resource struct {
    Name               string
    Kinds              Kind
    Spec               []*Spec
    ProvideProducer    Provider[Producer]
    ProvideConsumer    Provider[Consumer]
    ProvideTransformer Provider[Transformer]
}
```

`Spec` describes the configuration fields the resource accepts. It is the
contract the parser type-checks each `.psy` block against. Unknown fields on
the block, missing `Required` fields, and mistyped values are all errors at
parse time — a plugin never has to defend against them.

```go
type Spec struct {
    Name        string
    Description string
    Required    bool
    Type        SpecType   // TypeString | TypeInt | TypeFloat | TypeBool | TypeList | TypeMap | TypeObject
    ElemType    *Spec      // element type for List/Map
    Fields      []*Spec    // attributes for Object
    Default     any        // Go-native; the host format converts it
}
```

Naming convention: use kebab-case in `Spec.Name` and match it in the `psy`
struct tag on the config type (`psy:"stop-after"`). This is consistent with
the rest of the ecosystem and with what users type in `.psy`.

Two attribute names are **reserved**: `stop-after` and `per-minute`. The host
strips these off the block before your `Parser` runs (they become the
resource's `sdk.BlockMeta`), and it enforces both behaviors independently of
the plugin. Do not declare them in your `Spec`, and do not try to read them
from your config struct — they will not be there.

### `sdk.Provider[T]`

```go
type Provider[T Producer | Consumer | Transformer] func(parse Parser) (T, error)
type Parser func(dst any) error
```

Your factory receives a `Parser` — a single closure that decodes the block's
attributes into a Go value. Point it at a pointer to a struct with `psy` tags
and it will fill it in. The signature intentionally matches
`sdk.ConfigBlock.Decode`, so hosts can hand the bound method through directly.

Return the fully configured `Producer` / `Consumer` / `Transformer`. That
returned value must not do more configuration work — by the time the host
starts pumping data through, it should be a live pipeline stage.

### The three roles

```go
type Producer    func(ctx context.Context, send chan<- []byte, errs chan<- error)
type Consumer    func(ctx context.Context, recv <-chan []byte, errs chan<- error, done chan<- struct{})
type Transformer func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error)
```

All three receive the pipeline's `ctx` and are contractually required to
select on `ctx.Done()` alongside their channel sends/receives — a plugin that
only ever does bare sends/receives will park forever if the host abandons it
mid-run (e.g. `stop-after` cutting the stream short). The host itself never
hangs on an abandoned plugin, but a non-conforming one leaks its own
goroutine.

**Producer.** Emit bytes on `send`. Close `send` and `errs` when done. If you
have nothing else to say, closing `send` is the only signal the host needs —
it will not read more. Errors go on `errs` before you stop.

**Consumer.** Read from `recv` until it closes. Close `errs` and `done` on
exit. `done` is the host's cue that draining finished; do not close it early
or the host will race you.

**Transformer.** Reads from `in`, writes results to `out`, and is responsible
for closing `out` when done — typically when `in` closes and any buffered
state has been flushed. To drop a message, simply don't write it to `out`;
there is no `nil`-return sentinel to reason about. To signal an error, send
it on `errs` and keep going — the host decides whether that terminates the
pipeline based on the pipeline's `exit-on-error`, but *your* loop must not
stop just because one message failed. A Transformer must not close `errs`.

Because the host calls your `Transformer` exactly once per run — with `in`
staying open for the whole stream, not once per message — this shape now
supports 1-to-many (explode one message into several `out` sends), many-to-1
(accumulate across messages, flush when `in` closes), and everything in
between, not just the old 1-to-1/filter mapping. A minimal passthrough:

```go
func(ctx context.Context, in <-chan []byte, out chan<- []byte, errs chan<- error) {
    defer close(out)
    for {
        select {
        case msg, ok := <-in:
            if !ok {
                return
            }
            transformed, err := transformData(msg)
            if err != nil {
                select {
                case errs <- err:
                case <-ctx.Done():
                    return
                }
                continue
            }
            select {
            case out <- transformed:
            case <-ctx.Done():
                return
            }
        case <-ctx.Done():
            return
        }
    }
}
```

Most stdlib transformers are plain per-message mappings, but there is no
shared helper anywhere in stdlib — not even a stdlib-internal one — that
adapts a `func(in []byte) ([]byte, error)` into this contract. Every
transformer, in stdlib and out, writes the raw loop above (or its own
equivalent skeleton) directly: see `stdlib/transform/codec.go`'s
`codecTransformer` or `stdlib/flow/flow.go`'s `Wait`/`Head`/`Tail` for
examples of the plain per-message shape written out in full. The SDK
deliberately ships no Map/Filter adapter, and stdlib holds itself to the same
rule, so reading any stdlib transformer shows exactly the loop a plugin
author is expected to write. For a stateful example that flushes on stream
end, see `stdlib/transform/keyed.go`'s `Batch`, which buffers messages into
fixed-size groups and emits a final partial group when `in` closes.

### `sdk.BlockMeta`

```go
type BlockMeta struct {
    PerMinute int `psy:"per-minute"`
    StopAfter int `psy:"stop-after"`
}
```

These are host-owned. They are decoded before `Bind` runs and enforced by the
host wrapping your `Producer`/`Consumer`/`Transformer`. Nothing plugin-side
touches them.

## Data model

Every message on the wire is a `[]byte`. The stdlib layers a codec / framing
model on top of that (`decode = "gzip|json"`, `sep = "\n"`, jq/path
selection). See [stdlib.md](stdlib.md) for the model. If your plugin
produces or consumes structured data, it is up to you whether to serialize
it (e.g. JSON) at the boundary or expose it as opaque bytes and let a
downstream `recode` handle it — the second is usually cheaper and more
composable.

## Publishing and versioning

psyduck fetches plugins via `git clone` and builds them with the host's Go
toolchain. Two consequences:

- `plugin.source` can be any `git clone`-able URL (`https://`, `git@`,
  or a local path or `.so`).
- `plugin.tag` selects a git ref. Omit it to build from the default branch
  each time `psyduck init` runs. Pin it in shared workspaces.

Because `plugin.Open` requires matching module graphs, a plugin published
without careful version pins will start failing when the host's SDK version
moves. The safest release surface is: `main` tracks a specific psyduck
version and pins its `go.mod` accordingly; new psyduck releases get a
matching plugin tag.

## Reference: the stdlib as an example

Every stdlib resource is a working plugin implementation. Small, focused
examples worth reading:

| File | Shows |
|---|---|
| `stdlib/produce/constant.go` | Minimal producer + config struct. |
| `stdlib/consume/trash.go` | Minimal consumer that ignores its config. |
| `stdlib/transform/dev.go` (`Count`) | Transformer with per-instance mutable state (closed over, not mutex-guarded — the channel loop is single-threaded). |
| `stdlib/transform/keyed.go` (`Batch`) | Stateful transformer with a raw channel loop that flushes buffered output when `in` closes. |
| `stdlib/plugin.go` | Assembling many `Resource`s under one `sdk.NewInProc` plugin. |

The one thing the stdlib does *not* demonstrate is being an external
`plugin.Open` target: it is linked in directly by `main.go`. Any Git-based
plugin is otherwise structurally identical.
