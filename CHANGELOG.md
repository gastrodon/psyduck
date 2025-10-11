<!-- filepath: /home/eva/Documents/code/psyduck/CHANGELOG.md -->

# v0.3.0 (2025-10-11)

- Migrate configuration format from HCL to YAML.
- Standardize test messages across the test suite.
- Remove the `testify` dependency and migrate tests to the Go standard `testing` package.
- Rename package and module symbols: move/rename `configure` -> `parse` and adjust filenames for consistency.
- Rename plugin descriptor types and related identifiers to improve clarity and consistency.
- Miscellaneous refactors related to parsing and plugin handling.

## Spotlight: YAML configuration (breaking change)

v0.3.0 introduces a migration from HCL to YAML for all configuration files (pipelines, values, and plugin descriptors). This is a major change intended to improve readability and interoperability with other tooling.

Migration notes
- File extensions have changed from `.psy` to `.yaml` (examples below use `.yaml`).
- The parser package has been renamed and some types/labels were renamed — you may need to update imports and configuration keys.
- Plugin descriptors now use the renamed descriptor fields; ensure plugin names and sources are updated accordingly.

Example: pipeline file — before (HCL)

```hcl
pipeline "daily" {
  produce = ["producer_a"]
  consume = ["consumer_b"]
  transform = ["transform_x"]
  stop-after = 100
  exit-on-error = true
}

producer "mover_a" {
  type = "mover"
  path = "/data/a"
}

producer "transformer_b" {
  type = "mover"
  path = "/data/b"
}

producer "transformer_c" {
  type = "mover"
  path = "/data/c"
}
```

After (YAML)

```yaml
pipelines:
  - name: daily
    produce:
      - name: producer_a
        kind: mover
        path: /data/a
    consume:
      - name: consumer_b
        kind: consumer
        path: /data/b
    transform:
      - name: transform_c
        kind: transform
        path: /data/c
    stop-after: 100
    exit-on-error: true
```

Example: values file — before (HCL)

```hcl
values {
  foo = "bar"
  num = 123
}
```

After (YAML)

```yaml
values:
  foo: "bar"
  num: 123
```

Example: plugin descriptor — before (HCL)

```hcl
plugin "csv_producer" {
  source = "./plugins/csv"
  tag    = "v1.0"
}
```

After (YAML)

```yaml
plugins:
  - name: csv_producer
    source: ./plugins/csv
    tag: v1.0
```
---

<!-- Changes listed are derived from commits between tags v0.2.3 and v0.3.0 -->
