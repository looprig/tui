# TUI package boundaries

Date: 2026-07-15
Status: approved design
Scope: `github.com/looprig/tui`

## Goal

Keep the root `tui` package as the small public entry point while giving input handling,
presentation state, and rendering clear internal owners. The split must not turn private screen
state into a public API, introduce import cycles, or change terminal behavior.

The number of files is not itself the boundary. A package exists only where the code has a
separate responsibility and a one-directional dependency.

## Package layout

```text
tui
├── components
├── styles
├── runtime
├── sessionadapter
└── internal
    ├── input
    ├── model
    ├── presentation
    ├── view
    └── ttylog
```

The root package is a small public facade. It preserves `Agent`, `Screen`, `AgentBanner`, `New`,
and the rest of the current API through aliases and forwarding functions. The complete Bubble
Tea state machine lives in `internal/presentation`, which consumers cannot import.

### `internal/presentation`

Owns the coupled terminal application: `Screen`, `sessionCore`, transcript, interaction,
viewport, commands, handoff, rendering, and their tests. These files remain one package because
they share private entry, prompt, selection, layout, and update state.

The package imports `components`, `styles`, `internal/input`, `internal/model`, and
`internal/view`. It never imports the root `tui` facade, preventing an import cycle.

### `internal/input`

Owns turning composer text into trusted content blocks and discovering `@path` completions. It
contains attachment parsing, path validation, size limits, media detection, and input-specific
errors. The root package may retain aliases for existing exported error types so the move does
not break callers.

This package may import `core/content` and `components`. It must not import the root `tui`
package, `internal/model`, or `internal/view`.

### `internal/model`

Owns presentation state that is independent of the Bubble Tea screen state machine. It starts
with compaction and collapse projections. Additional reducers move here only when the root can
consume them through a focused value API rather than direct field access.

The package exposes only the operations the root screen and view layer need. Its exported names
remain module-private because the package is under `internal`.

### `internal/view`

Owns reusable terminal rendering primitives that do not depend on root transcript types. It
starts with rail layout and width wrapping. Additional renderers move here when they accept
typed values rather than `Screen`, `sessionCore`, or private transcript entries.

The package may import `components` and `styles`. It must not call Harness controllers or own
session lifecycle.

## Dependency direction

```text
runtime ───────► tui ◄────── sessionadapter
                  │
                  └────► internal/presentation
                               ├────► internal/input
                               ├────► internal/model
                               └────► internal/view

internal/input ─────► components
internal/model ─────► components
internal/view  ─────► components, styles
```

`internal/input` and `internal/model` never import `internal/view`. No internal package imports
the root `tui` package. Harness remains below the TUI adapter boundary and never imports TUI.

## Public compatibility

The refactor preserves:

- `tui.Agent`, `tui.OpenAgent`, `tui.AgentBanner`, `tui.Screen`, and `tui.New`
- `tui.DisplayProjection` and `tui.FoldDisplay` while they remain useful to replay tests and
  adapters
- existing typed attachment errors through root aliases
- all runtime, sessionadapter, component, and style import paths
- event folding, rendered output, keyboard behavior, mouse behavior, and clipboard behavior

No new public package is introduced. Internal names may be exported only to cross an internal
package boundary.

The root contains only module metadata, documentation, `api.go`, `errors.go`, and facade or
dependency tests. Implementation and behavioral tests live beside each internal owner.

## Deliberate non-boundaries

`Screen`, `sessionCore`, transcript, interaction, prompt presentation, viewport, and their
screen-specific renderers stay together in `internal/presentation`. They coordinate one Bubble
Tea state machine and share private entry, prompt, selection, and layout state. They are moved
as one unit rather than separated by artificial APIs.

Small render helpers do not each become packages. Rail, gradient, status, transcript rows, and
viewport behavior belong to one view layer and change together with the terminal language.

## Migration sequence

1. Extract input handling and preserve root error compatibility.
2. Extract independent presentation projections behind focused methods.
3. Extract reusable rendering primitives that have no root-state dependency.
4. Move the coupled Bubble Tea state machine together into `internal/presentation`.
5. Replace the root implementation with aliases and forwarding functions that preserve the
   existing public API.
6. Run the complete race, lint, build, and stale-reference checks after each dependency boundary.
