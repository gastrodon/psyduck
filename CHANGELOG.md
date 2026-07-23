# Changelog

All notable user-facing changes to psyduck since the Go rewrite. Versions
before v0.1.0 belong to the archived TypeScript prototype and are not covered
here. Dates are when the work landed on the release commit.

## Unreleased

- Resource blocks accept a host-owned `parallel = n` meta attribute on
  `produce`, `consume`, and `transform`. It materializes n copies of the
  resource, exactly as if it had been listed n times. Defaults to 1; values
  below 1 (and fractional values) are rejected, and it is not allowed on a
  `produce-from` seed.

## v0.13.1 — 2026-07-22

- `dedupe`: `window=0` now means never-evict — keys are deduplicated for the
  whole process lifetime (unbounded). `window<0` still defaults to 10000 and
  `window>0` keeps its bounded FIFO behavior.
- Plugin instance `Close` failures are logged instead of silently dropped.

## v0.13.0 — 2026-07-15

- Plugins now run as gRPC subprocesses instead of `plugin.Open` `.so` linking,
  removing the requirement that plugins be built in lockstep with the host's
  Go toolchain.
- Transformers support streaming, bidirectional data flow via `sdk.Map`.

## v0.12.0 — 2026-07-13

- `produce-from` streams through a single meta-producer backed by a runtime
  worker pool; `produce-from-parallel` caps concurrency and `produce-parallel=0`
  runs every producer at once.
- `stop-after` is now a producer-only flow governor.
- Added a Nix flake for reproducible builds.

## v0.11.0 — 2026-07-09

- Rebuilt the standard library on a continuous/discrete data model: file and
  archive I/O, networking, encoding, jq, templating, and dedupe.
- Reworked pipeline concurrency (context cancellation, correct fan-in/out),
  fixing goroutine leaks and consumer deadlocks.
- Plugins are pinned with per-file content-addressed lock files; execution is
  scoped per file with explicit imports, and `--chdir` was removed.

## v0.10.0 — 2025-10-11

- Decoupled the core from HCL — pipelines are now described in a
  format-agnostic way.
- Renamed the `configure` package/stage to `parse`.

## v0.9.0 — 2024-04-19

- Added the `transpose` transformer; stdlib transformers are sorted and tidied.

## v0.8.0 — 2024-04-14

- The bundled psyduck stdlib plugin is loaded by default.
- Added `exit-on-error` pipeline config.
- `psyduck run` takes its target file positionally.

## v0.7.2 — 2024-04-09

- Config can read environment variables.

## v0.7.1 — 2024-04-09

- Support HTTPS git plugin sources.

## v0.7.0 — 2024-04-09

- Remote (git) producers and plugins; a local plugin now refers to a directory
  of code.
- Added the `psyduck init` command.
- Added `produce-from` and `stop-after` pipeline config; transformers can act
  as filters.
- Switched logging to logrus.

## v0.6.0 — 2024-03-09

- Split the SDK into its own package; cleaned up plugin fetch/build.

## v0.5.2 — 2023-02-20

- Split the Docker image into builder and runtime stages; configurable root
  plugin directory.

## v0.5.1 — 2023-02-19

- Dependency updates (maintenance).

## v0.5.0 — 2023-02-18

- Added a real CLI entrypoint and a plugin loader.

## v0.4.0 — 2022-12-16

- Replaced hcldec with a custom HCL config parser; pipelines run through
  `RunPipeline`.

## v0.3.1 — 2022-07-26

- Providers surface failures through an error channel.

## v0.3.0 — 2022-07-26

- Errors propagate across providers, producers, consumers, and transformers.

## v0.2.1 — 2022-07-26

- Internal helpers (maintenance).

## v0.2.0 — 2022-07-26

- Run multiple producers and consumers per pipeline; select the pipeline to run
  with `-pipeline`.
- Data is passed between pipeline stages as bytes.

## v0.1.0 — 2022-07-24

- Initial Go implementation: HCL-configured producer → transformer → consumer
  pipelines with a plugin-based mover model.
