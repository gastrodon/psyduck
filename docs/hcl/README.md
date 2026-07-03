# The psyduck HCL pipeline language

psyduck pipelines are described in [HCL](https://github.com/hashicorp/hcl) files
with the `.psy` extension. All `.psy` files in a directory are parsed together
as one configuration — resources declared in one file may be referenced from
any other. The files in this directory form one such configuration; each
pipeline can be run independently by name.

## Running the examples

```sh
# once: fetch external plugins declared in plugin {} blocks (here: amqp)
psyduck --chdir docs/hcl init

# then run any pipeline by name
psyduck --chdir docs/hcl run 1-to-1
psyduck --chdir docs/hcl run locals
psyduck --chdir docs/hcl run consume-remote
```

The amqp pipelines (`load-left`, `move-right`, `ready-remote`,
`consume-remote-amqp`) additionally need a rabbitmq at `localhost:5672`.

| File | Demonstrates | Pipelines |
|---|---|---|
| `basics.psy` | resources, pipeline fan shapes | `1-to-1`, `1-to-many`, `many-to-1`, `many-to-many` |
| `locals.psy` | `locals {}`, `local.*`, `env.*` | `locals` |
| `amqp.psy` | external plugins | `load-left`, `move-right` |
| `remote.psy` | `produce-from` | `consume-remote`, `ready-remote`, `consume-remote-amqp` |

## Top-level blocks

A `.psy` file contains any number of these blocks, in any order:

```hcl
locals    { ... }                        # named constant values
plugin    "name" { ... }                 # external plugin declaration
produce   "resource" "name" { ... }      # producer binding
consume   "resource" "name" { ... }      # consumer binding
transform "resource" "name" { ... }      # transformer binding
pipeline  "name" { ... }                 # pipeline definition
```

Anything else at the top level is a parse error.

### `plugin "name" {}`

Declares an external plugin to be fetched and compiled by `psyduck init`.

```hcl
plugin "amqp" {
  source = "https://github.com/psyduck-etl/amqp"  # required: git URL
  tag    = "v0.1.0"                               # optional: git ref to check out
}
```

Plugin blocks are read in a cheap pre-pass, so `init` works before any
plugin is available. The `psyduck` standard library plugin (resources
`constant`, `increment`, `trash`, `inspect`, `sprintf`, `snippet`,
`transpose`, `wait`, `zoom`) is always loaded — no `plugin` block needed.

### `locals {}`

Named constant values, terraform-style. All `locals` blocks across all files
are merged; duplicate names are an error. Values are referenced as
`local.<name>` and may themselves use `env.*`.

```hcl
locals {
  queue-host = "amqp://guest:guest@${env.RABBIT_HOST}:5672/"
}
```

### `env.*`

`env.NAME` resolves to the value of environment variable `NAME`. The parser
prescans all expressions for `env.*` references before evaluation, so only
the queried names are read — and an *unset* variable resolves to the empty
string `""` rather than erroring.

### Resource blocks: `produce` / `consume` / `transform`

A resource block binds a plugin resource under a name:

```hcl
produce "constant" "greeting" {   # <verb> "<resource>" "<name>"
  value      = "hello"            # attribute from the resource's spec
  stop-after = 30                 # host-owned meta attribute
}
```

- The **verb** (`produce`, `consume`, `transform`) selects the resource kind.
  A plugin resource must support that kind, or binding fails.
- The **resource** label is the plugin resource name (e.g. `constant`).
- The **name** label is your identifier for this configured instance.
  The same resource may be bound many times under different names.

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
directory is an error.

### `pipeline "name" {}`

Wires bound resources into a runnable pipeline:

```hcl
pipeline "example" {
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
Pipeline names must be unique across the directory.

Data flows: every producer's messages are merged (fan-in), passed through
the transformer stack in order, and each resulting message is delivered to
every consumer (fan-out).

#### Refs

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

#### `produce-from`

Instead of listing producers statically, a pipeline may derive them from
data:

```hcl
pipeline "consume-remote" {
  produce-from = produce.constant.seed
  consume      = [consume.trash.trash]
}
```

At build time the *seed* producer is run once and its first message (10s
timeout) is parsed as HCL `produce {}` blocks — same syntax, same `env.*`
resolution. The resulting producers then run as if they had been declared
literally. See `remote.psy` for both a stdlib-only and a queue-backed
example.

## Comments

HCL comments are supported: `# ...`, `// ...`, and `/* ... */`.
