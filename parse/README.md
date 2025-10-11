# Writing psyduck pipelines in YAML

Use the `configure_yaml` package to define and parse pipelines in YAML. A top-level `pipelines` array contains one or more named pipelines:

```yaml
pipelines:
  - name: example_pipeline
    produce:
      - kind: constant
        name: my_const
        value: "foo"
        stop-after: 5
    transform:
      - kind: inspect
        name: log_data
        be-string: true
    consume:
      - kind: trash
        name: sink
    stop-after: 10       # optional, stop after N items
    exit-on-error: true  # optional, stop on first error
```

### Defining inline producers / transformers / consumers

Each pipeline part is declared inline under `produce`, `transform`, or `consume`:

- `kind`: the plugin resource name (e.g. `constant`, `inspect`, `trash`, etc.)
- `name`: a unique name within the pipeline
- plugin-specific options (see plugin documentation)

### Reusing definitions with YAML merge (`<<`)

Extract common options into an anchor and merge with overridden fields:

```yaml
common_const: &default_const
    kind: constant
    value: "hello"
    stop-after: 3

pipelines:
  - name: two_consts
    produce:
      - <<: *default_const
        name: const1
      - <<: *default_const
        name: const2
        value: "world"   # overrides merged value
    consume:
      - kind: trash
        name: done
```

Fields specified after `<<` override merged defaults.

### Using remote producers (`produce-from`)

Reference a producer defined in another pipeline instead of inline:

```yaml
pipelines:
  - name: upstream
    produce:
      - kind: constant
        name: shared_const
        value: "xyz"
  - name: downstream
    produce-from: upstream.shared_const
    transform:
      - kind: inspect
        name: dump
    consume:
      - kind: trash
        name: sink
```

`produce-from` takes precedence over inline `produce` if both are present.

### Pipeline keys reference

At the pipeline level:

- `name` (string, required): unique pipeline identifier
- `produce` ([]Part, optional): inline producer definitions
- `produce-from` (string, optional): reference to `<pipeline>.<part>`
- `transform` ([]Part, optional): transformer definitions
- `consume` ([]Part, required): consumer definitions
- `stop-after` (integer, optional): stop after N items
- `exit-on-error` (boolean, optional): stop on first error

At the part level (inline under produce/transform/consume):

- `kind` (string, required)
- `name` (string, required)
- plugin-specific options (see stdlib or custom plugin docs)

---

Once you have your YAML file, call:

```go
pipelines, err := configure_yaml.LoadPipelinesFromYAML(yamlText)
```
