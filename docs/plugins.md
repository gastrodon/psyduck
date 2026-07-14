# Writing psyduck plugins

A plugin is a Go `main` module whose `main` serves an `sdk.Plugin` over
gRPC via `rpc.Serve`. psyduck clones the module at `psyduck init`, compiles
it with a plain `go build`, and launches the resulting executable as a
subprocess at run time (hashicorp/go-plugin style — the same model
Terraform providers use). Once loaded, every resource the plugin declares
becomes usable from `.psy` files just like a stdlib resource.

The public interface is the `github.com/psyduck-etl/sdk` package. Nothing in
`github.com/gastrodon/psyduck` is imported by plugin authors — the host and
the plugin only meet through the SDK.

## The shape of a plugin

```go
package main

import (
    "context"

    "github.com/psyduck-etl/sdk"
    "github.com/psyduck-etl/sdk/rpc"
)

func main() { rpc.Serve(Plugin()) }

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
    return func(ctx context.Context, send chan<- []byte, errs chan<- error) {
        defer close(send)
        defer close(errs)
        for {
            select {
            case send <- msg:
            case <-ctx.Done():
                return
            }
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

Requirements:

- Package must be `main`, and `main` must call `rpc.Serve` with the
  `sdk.Plugin` — that's the whole entrypoint.
- Because the plugin is a separate process that only shares the wire
  contract with the host, its `go.mod` does **not** need to match the
  host's dependency graph, toolchain, or even SDK patch version. The only
  compatibility surface is `rpc.Handshake.ProtocolVersion`, which the SDK
  bumps when the wire contract changes incompatibly — a mismatch fails
  loudly at launch.

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

Both are verb-restricted: `stop-after` is accepted only on `produce`
resources (it's a producer-only flow governor); `per-minute` is accepted on
`produce` and `consume` resources. Declaring either on a `transform` block,
or `stop-after` on a `consume` block, is a parse-time error — the host
rejects it as an unknown attribute before your plugin is ever bound.

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
type Transformer func(in []byte) ([]byte, error)
```

**Producer.** Emit bytes on `send`. Close `send` and `errs` when done. If you
have nothing else to say, closing `send` is the only signal the host needs —
it will not read more. Errors go on `errs` before you stop. `ctx` is the
pipeline's run context: select on `ctx.Done()` alongside every send, or an
abandoned producer (the host stopped reading — cancellation, `exit-on-error`,
or a consumer finishing early) leaks its own goroutine, parked forever on a
channel nobody drains.

**Consumer.** Read from `recv` until it closes. Close `errs` and `done` on
exit. `done` is the host's cue that draining finished. Closing `done` while
`recv` could still receive is fine — it's how a consumer finishes early on
its own (e.g. a count cutoff); the host stops sending to it from that point
on, at the cost of at most one message already in flight landing after
`done` closes. Select on `ctx.Done()` here too, for the same reason as
`Producer`.

**Transformer.** One in, one out. If you need to drop a message, return
`(nil, nil)`. If you need to signal an error, return `(nil, err)` — the host
decides whether that terminates the pipeline based on the pipeline's
`exit-on-error`. Transformers may be called concurrently from more than one
goroutine; guard mutable state (see `stdlib/transform/dev.go`'s `Count` for a
mutex example).

### `sdk.BlockMeta`

```go
type BlockMeta struct {
    PerMinute int `psy:"per-minute"`
    StopAfter int `psy:"stop-after"`
}
```

These are host-owned. They are decoded before `Bind` runs and enforced by the
host wrapping your `Producer`/`Consumer`/`Transformer`. Nothing plugin-side
touches them. `StopAfter` only ever bounds a `Producer` — the host has no
`Consumer`/`Transformer` gate for it, so a config that tried to set it
there would never have reached your plugin in the first place (rejected at
parse time). `PerMinute` bounds both `Producer` and `Consumer`.

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
  or a local path — a source directory to build, or a prebuilt plugin
  executable to store as-is).
- `plugin.tag` selects a git ref. Omit it to build from the default branch
  each time `psyduck init` runs. Pin it in shared workspaces.

Plugins are separate processes, so the host's SDK version moving does not
break them — host and plugin only meet at the gRPC wire contract, which is
versioned independently by `rpc.Handshake.ProtocolVersion`. Pin
`plugin.tag` for reproducibility, not for toolchain parity.

## Reference: the stdlib as an example

Every stdlib resource is a working plugin implementation. Small, focused
examples worth reading:

| File | Shows |
|---|---|
| `stdlib/produce/constant.go` | Minimal producer + config struct. |
| `stdlib/consume/trash.go` | Minimal consumer that ignores its config. |
| `stdlib/transform/dev.go` (`Count`) | Transformer with per-instance mutable state. |
| `stdlib/plugin.go` | Assembling many `Resource`s under one `sdk.NewInProc` plugin. |

The one thing the stdlib does *not* demonstrate is running as a
subprocess: it is linked in directly by `main.go` and never crosses the
gRPC boundary. `cmd/example-plugin` is the minimal external plugin — an
`rpc.Serve` main around one producer resource. Any Git-based plugin is
structurally identical.
