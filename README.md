# tui

The reusable terminal user interface for
[looprig](https://github.com/looprig/harness). It keeps the Bubble Tea v2
presentation stack outside the core Harness.

The module has four public package boundaries:

- the root `tui` package is the stable public facade for the interactive screen and contract
- `runtime` owns process signals, logging, terminal capture, and Bubble Tea startup
- `sessionadapter` adapts a Harness Session to the TUI contract
- `components` and `styles` are reusable presentation leaves

The dependency is one-directional: TUI imports Harness, never the reverse.

Internal ownership is similarly one-directional:

- `internal/presentation` owns the complete Bubble Tea state machine and behavioral tests
- `internal/input` owns attachment parsing, validation, and file completion
- `internal/model` owns independent value projections such as compaction and collapse state
- `internal/view` owns reusable rendering primitives such as rails and width wrapping
- `internal/ttylog` owns private terminal log capture

The transcript, interaction model, viewport, and `Screen` stay together in
`internal/presentation` because they form one Bubble Tea state machine rather than independent
reusable packages. Consumers continue to import only `github.com/looprig/tui`.

## Building & testing

This module vendors its dependencies. Use the Make targets for the vendored,
reproducible verification path:

```sh
make secure
go build ./...
go test -race ./...
```
