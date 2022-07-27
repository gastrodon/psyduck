## psyduck
---
psyduck is a WIP extensible ETL engine, previously written in [typescript](https://github.com/gastrodon/psyduck/tree/archive/psyduck-typescript)
that aims to be extensible and fast

## progress
---
The current feature TODO looks like

- `[x]` some config loading ( yaml works for now! )
- `[x]` core pipeline builder + runner
- `[x]` plugin loader + exposed SDK tools for building plugiins
- `[x]` many producers -> many transformers -> many consumers
- `[.]` a [standard library](https://github.com/gastrodon/psyduck-std)
- `[ ]` a DSL for describing pipelines using HCL
- `[ ]` loading compiled plugins @ runtime
- `[ ]` listen for new pipelines via http ( maybe? )
- `[ ]` psyduck node distributing pipeline work ( also maybe? )
