# Writing `.psy` files

psyduck pipelines are described in `.psy` files, which use
[HCL](https://github.com/hashicorp/hcl) syntax. This document is the
reference for the language. It focuses on the stdlib — for plugins, see
[plugins.md](plugins.md); for the resources you can put in a pipeline, see
[stdlib.md](stdlib.md); for idiomatic patterns and recipes, see
[patterns.md](patterns.md).

**A `.psy` file is the unit of configuration.** Resources, locals, and
plugins declared in one file are visible only within that file — there's no
implicit directory-wide sharing. To reuse something declared in another
file, `import` it explicitly (see [Imports](#imports) below).

A file is run by pointing the CLI at it directly:

```sh
psyduck run path/to/<file>.psy
```

Every `pipeline{}` block declared *directly* in that file runs: a file with
none is an error, one runs by itself, and more than one run concurrently in
the same process (see [Running a file](#running-a-file)).

## Top-level blocks

A `.psy` file contains any number of these blocks, in any order:

```hcl
locals    { ... }                        # named constant values
plugin    "name" { ... }                 # external plugin declaration (see plugins.md)
import    { ... }                        # reuse resources/pipelines from another file
produce   "resource" "name" { ... }      # producer binding
consume   "resource" "name" { ... }      # consumer binding
transform "resource" "name" { ... }      # transformer binding
pipeline  "name" { ... }                 # pipeline definition
```

Anything else at the top level is a parse error.

## Resource blocks

A resource block binds one plugin resource under a name of your choosing:

```hcl
produce "constant" "greeting" {   # <verb> "<resource>" "<name>"
  value      = "hello"            # attribute from the resource's spec
  stop-after = 30                 # host-owned meta attribute
}
```

- The **verb** (`produce`, `consume`, `transform`) selects the resource kind.
  A plugin resource must support that kind, or binding fails.
- The **resource** label is the plugin resource name (e.g. `constant`).
- The **name** label is your identifier for this configured instance. The
  same resource may be bound many times under different names.

Attributes are checked *strictly* against the resource's spec: an unknown
attribute is an error, missing required attributes are errors, and every
value is evaluated and type-checked at parse time. Expressions may use
`local.*` and `env.*`.

Two **meta attributes** are host-owned and accepted on every resource block
(a plugin never declares them):

| Attribute | Type | Meaning |
|---|---|---|
| `stop-after` | int | stop this resource after n items (0 = unrestricted) |
| `per-minute` | int | rate limit, items per minute (0 = unrestricted) |

Declaring the same `(verb, resource, name)` triple twice in the same file is
an error.

## Pipelines

```hcl
pipeline "hello" {
  produce   = [produce.constant.greeting]   # list of producer refs
  transform = [transform.inspect.inspect]   # list of transformer refs (may be [])
  consume   = [consume.trash.trash]         # list of consumer refs (required)
}
```

| Attribute | Type | Meaning |
|---|---|---|
| `produce` | list of refs | producers to fan in |
| `produce-from` | ref | derive producers from a seed producer's output (see below) |
| `consume` | list of refs | consumers to fan out to (required) |
| `transform` | list of refs | transformers, applied in order |
| `stop-after` | int | pipeline-level stop count |
| `exit-on-error` | bool | stop the pipeline on the first error |
| `produce-parallel` | int | cap on concurrently-running producers, for `produce` and `produce-from` alike (default 1). With a static `produce` list, `0` means "all at once" (resolves to the producer count); with `produce-from` it must be ≥ 1 (`0` is rejected — no fixed count to expand to) |
| `produce-from-timeout` | int | seconds to wait for the `produce-from` seed's first producers (0 = wait indefinitely, default 10) |

Exactly one of `produce` / `produce-from` describes the producer set.
Pipeline names must be unique within the file.

Data flows: every producer's messages are merged (fan-in), passed through
the transformer stack in order, and each resulting message is delivered to
every consumer (fan-out).

### Refs

Inside a `pipeline` block, a resource is referenced by its verb-qualified
path or its short form:

```hcl
produce = [produce.constant.greeting]   # verb-qualified
produce = [constant.greeting]           # short form — verb inferred from the attribute
```

The visible refs are scoped by attribute: `produce` only sees producer
bindings, `consume` only consumers, and so on. `local.*`, `env.*`, and
`imports.*` remain available in every attribute; a resource name that
collides with a reserved namespace (`local`, `env`, `imports`) is an
error.

### Fan shapes

The `produce` and `consume` lists can hold any number of refs. All four fan
shapes are supported without special syntax:

```hcl
produce "constant" "1" { value = "val-1"  stop-after = 30 }
produce "constant" "2" { value = "val-2"  stop-after = 60 }
transform "inspect" "log" {}
consume "trash" "sink" {}

pipeline "1-to-1"       { produce = [produce.constant.1]                       consume = [consume.trash.sink]                          transform = [transform.inspect.log] }
pipeline "1-to-many"    { produce = [produce.constant.1]                       consume = [consume.trash.sink, consume.trash.sink]      transform = [transform.inspect.log] }
pipeline "many-to-1"    { produce = [produce.constant.1, produce.constant.2]   consume = [consume.trash.sink]                          transform = [transform.inspect.log] }
pipeline "many-to-many" { produce = [produce.constant.1, produce.constant.2]   consume = [consume.trash.sink, consume.trash.sink]      transform = [transform.inspect.log] }
```

## `locals {}` and `env.*`

`locals` declares named constant values, terraform-style. All `locals`
blocks within one file are merged; duplicate names are an error. `locals`
are file-scoped — a file never sees another file's `local.*` values, even
one it imports. Values are referenced as `local.<name>` and may themselves
use `env.*`:

```hcl
locals {
  greeting = "hello ${env.USER}"
}

produce "constant" "hi" {
  value      = local.greeting
  stop-after = 3
}
```

`env.NAME` resolves to the value of environment variable `NAME`. The parser
prescans all expressions for `env.*` references before evaluation, so only
the queried names are read — and an *unset* variable resolves to the empty
string `""` rather than erroring.

## `produce-from`

A pipeline may derive its producers from another producer's output rather
than listing them statically:

```hcl
produce "constant" "seed" {
  value = <<-EOF
  produce "constant" "remote" {
    value      = "from-a-seed"
    stop-after = 10
  }
  EOF
  stop-after = 1
}

pipeline "dynamic" {
  produce-from = produce.constant.seed
  consume      = [consume.trash.trash]
}
```

The *seed* producer is started when the pipeline runs. Each message it emits
is parsed as HCL `produce {}` blocks — same syntax, same `env.*` resolution,
same strict attribute checking — and the resulting producers run as if they
had been declared literally. The seed keeps running alongside the pipeline:
every further message yields a fresh batch of producers, so a long-lived seed
(a queue listener, a socket) can keep feeding the pipeline new work for as
long as it runs. Because binding happens at run time, an unknown plugin or a
broken producer config surfaces as a run-time error, not a build failure.

The run waits for the seed's first producers, bounded by
`produce-from-timeout` (in seconds; default 10, `0` waits indefinitely).
Messages that declare no producers don't satisfy that wait. A seed that
closes — or times out — without ever declaring a producer surfaces an
ordinary producer error: with `exit-on-error` set it fails the run, and
without it the error is logged and the pipeline finishes normally, having
delivered nothing.

Once the seed closes or errors, the pipeline finishes when the producers
already delivered are exhausted (with `exit-on-error` set, a seed error
stops the run instead).

The seed can be any producer, so the config may come from anywhere a
producer can read: a file, a socket, an HTTP response — anything the stdlib
or a plugin exposes.

Combine with `produce-parallel` to bound how many producers run at once.
Producers run through a worker pool of that many slots: when one exhausts,
its slot is refilled immediately from the next arrival — there are no waves
and no ordering guarantee across producers. The default of 1 runs producers
one at a time; a large value approximates running everything at once.

## Imports

A file reuses another file's resources or pipelines through `import`:

```hcl
import {
  shared = "shared.psy"          # relative to this file's own directory
  lib    = "${env.LIB_DIR}/lib.psy"
}
```

Each attribute is one import: the name is the local alias, the value is a
path (an ordinary string, so `env.*` interpolation works). More than one
`import{}` block may appear; duplicate aliases are an error, same as
`locals`.

Every import is a *whole-file* import — there's no way to import a single
resource directly. The imported file is exposed under `imports.<alias>`,
shaped the same way the file itself is:

```
imports.<alias>.produce.<kind>.<name>          # a produce "<kind>" "<name>" {} in that file
imports.<alias>.consume.<kind>.<name>
imports.<alias>.transform.<kind>.<name>
imports.<alias>.pipeline.<name>.produce         # that file's pipeline "<name>" {}, as ordered ref lists
imports.<alias>.pipeline.<name>.consume
imports.<alias>.pipeline.<name>.transform
imports.<alias>.pipeline.<name>.stop-after
imports.<alias>.pipeline.<name>.exit-on-error
```

Use it like any other ref, in any attribute where refs are valid:

```hcl
import {
  shared = "shared.psy"
}

pipeline "main" {
  produce = [produce.sequence.s]
  consume = [imports.shared.consume.file.out]     # one resource from shared.psy
}

pipeline "reuse" {
  produce = imports.shared.pipeline.upstream.produce   # a whole producer list, reused verbatim
  consume = [imports.shared.consume.file.out]
}
```

Imports are **not transitive**: importing a file exposes only what *that
file itself* declares, not whatever it in turn imports. Importing the same
file from two different places resolves it once (its resources are shared,
not duplicated), and an import cycle (A imports B imports A) is a parse
error.

## Running a file

`psyduck run <file>.psy` builds and runs every `pipeline{}` block declared
*directly* in that file — not ones only reachable through `imports.*`:

- **Zero** pipelines in the file is an error; nothing runs.
- **One** pipeline runs directly.
- **More than one** run concurrently, each in its own goroutine, sharing the
  process. `run` waits for all of them and exits non-zero if any of them
  returned an error.

`psyduck init`, `list`, and `show` take the same file argument. `init`
resolves `plugin{}` declarations across the file's entire import closure —
not just the file itself — since a plugin an imported resource depends on
still needs to be fetched and built for the importing file to run.

## External plugins

Additional resources come from external plugins, declared with:

```hcl
plugin "name" {
  source = "https://github.com/org/repo"
  tag    = "v0.1.0"   # optional
}
```

The plugin is fetched and compiled by `psyduck init` and then loaded on
`psyduck run`. Everything above — resource blocks, refs, `produce-from` —
applies uniformly to plugin resources; only `plugin {}` itself is
plugin-specific syntax. See [plugins.md](plugins.md) for the authoring
side.

## Comments

HCL comments are supported: `# ...`, `// ...`, and `/* ... */`.

## Heredocs

HCL heredocs are useful for multi-line string values such as templates and
seed-producer bodies:

```hcl
transform "render" "cfg" {
  engine = "template"
  format = <<-EOF
  {"page": {{.}}}
  EOF
}
```

The `<<-` form strips leading whitespace so the heredoc can be indented
naturally in-context.
