# Project: psyduck

## What this is

A Go-based ETL engine implementing:
- A DSL language for describing data pipelines (using YAML)
- A plugin system using go-plugin
- A standard library of common ETL constructs

## About the name

**psyduck — the psychic ETL** is a reference to Psyduck, the psychic Pokémon:

- **Telekinesis under pressure**: Psyduck uses TK to move many items at once under extreme pressure — mirroring how the ETL orchestrates and moves data across many concurrent processes
- **Persistent headache**: Psyduck constantly experiences headaches from psychic activity, representing the ETL always working heavily under the hood with high concurrency, making full use of available hardware
- **Telepathic communication**: Psyduck communicates via telepathy, just as goroutines in this project communicate heavily through channels between active processes — enabling decentralized, brain-to-brain-like information flow

## Overarching goals

- Write declarative code to describe data pipelines
- Implement business logic via plugin system constructs
- (stretch) Server process that receives ETL "jobs" and executes them — similar to how Nomad executes jobs
- (stretch) Distribute jobs across peer nodes for highly concurrent execution

## Worktree workflow

- Use git worktrees for feature work
- When starting feature work, check if we are in a worktree (`git rev-parse --show-toplevel` vs the main repo root)
- If not in a worktree, ask if we should switch to one before proceeding
- The worktree folder name must always match the branch name exactly
- **Claude Code assumption**: Assume you are working in a worktree unless told otherwise

## Key conventions

- Use `go doc` to look up documentation for external libraries — do NOT grep through Go module source directories
- This is a high-concurrency focused application — carefully test all code dealing with concurrency
- **Exercise HIGH CAUTION when editing anything in `./core`** — this is highly concurrent, fundamental code that the rest of the library depends on
- **README must be human-written only** — do not auto-generate or modify README contents

## Pre-commit hooks

Git hooks are configured in `flake.nix` via the `shellHook` in `devShells.default`. They are automatically installed when entering `nix develop`. The hooks only run when staged `.go` files are present, and include:
- Build check (`go build -o /dev/null .`)
- Test suite (full tests if `core/` is modified, short tests otherwise)
- Auto-format with `gofmt` and re-stage files
