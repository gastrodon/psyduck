# Writing `.psy` files

psyduck pipelines are described in `.psy` files, which use
[HCL](https://github.com/hashicorp/hcl) syntax. This document is the
reference for the language. It focuses on the stdlib — for plugins, see
[plugins.md](plugins.md); for the resources you can put in a pipeline, see
[stdlib.md](stdlib.md); for idiomatic patterns and recipes, see
[patterns.md](patterns.md).

A **workspace** is any directory containing `.psy` files. All `.psy` files in
that directory parse together as a single configuration — resources declared
in one file may be referenced from any other. Each pipeline can be run
independently by name:

```sh
psyduck --chdir path/to/workspace run <pipeline-name>
```

## Top-level blocks

A `.psy` file contains any number of these blocks, in any order:

```hcl
locals    { ... }                        # named constant values
plugin    "name" { ... }                 # external plugin declaration (see plugins.md)
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

Declaring the same `(verb, resource, name)` triple twice anywhere in the
workspace is an error.

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

Exactly one of `produce` / `produce-from` describes the producer set.
Pipeline names must be unique across the workspace.

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
bindings, `consume` only consumers, and so on. `local.*` and `env.*` remain
available; a resource name that collides with a reserved namespace
(`local`, `env`) is an error.

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
blocks across all files in the workspace are merged; duplicate names are an
error. Values are referenced as `local.<name>` and may themselves use
`env.*`:

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

At build time the *seed* producer is run once and its first message (10s
timeout) is parsed as HCL `produce {}` blocks — same syntax, same `env.*`
resolution, same strict attribute checking. The resulting producers then
run as if they had been declared literally.

The seed can be any producer, so the config may come from anywhere a
producer can read: a file, a socket, an HTTP response — anything the stdlib
or a plugin exposes.

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
