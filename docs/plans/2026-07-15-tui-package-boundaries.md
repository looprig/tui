# TUI Package Boundaries Implementation Plan

**Goal:** Reduce the repository root to a stable public facade while moving the complete coupled
Bubble Tea application into `internal/presentation` and retaining the existing leaf packages.

**Architecture:** Preserve every public root contract through aliases and forwarding functions.
Move the root implementation and behavioral tests together into `internal/presentation`, which
imports the existing internal leaves but never imports the root facade. Treat the work as a
structural refactor with no intended behavior change.

**Reference design:** `docs/plans/2026-07-15-tui-package-boundaries-design.md`

## Task 1: Pin the public facade and dependency rules

- Extend root API compile tests for `Agent`, `OpenAgent`, `AgentBanner`, `Screen`,
  `DisplayProjection`, `FoldDisplay`, and attachment error types.
- Add an import-boundary test that rejects root imports from `internal/input`,
  `internal/model`, and `internal/view`, and rejects model-to-view imports.
- Run the focused tests and verify the new package expectations fail before extraction.

## Task 2: Extract `internal/input`

- Move attachment parsing, content block construction, file completion, and their tests.
- Export only the internal functions required by the root/model callers.
- Preserve existing root attachment error names with aliases.
- Update session submission and composer completion call sites.
- Run focused input, root, and race tests.

## Task 3: Extract independent state into `internal/model`

- Move compaction and collapse projections, which have value semantics and focused operations.
- Preserve root-private aliases where the transcript uses the same stable display identity.
- Keep transcript, interaction, and display restoration at the root while they share the
  Bubble Tea screen state machine and private prompt/entry types.
- Run projection, collapse, root, and race tests.

## Task 4: Extract independent rendering into `internal/view`

- Move rail layout and width wrapping, which depend only on typed arguments and styles.
- Keep transcript rendering, status composition, gradients, loop bar, viewport, and surface
  layout at the root while they consume root-private transcript and screen state.
- Keep reusable generic widgets in `components`; do not duplicate them in the internal layer.
- Run rail, rendering, root, and race tests.

## Task 5: Reduce and document the root

- Add a failing root-layout test that permits only `api.go`, `errors.go`, and facade/boundary
  tests as root Go files.
- Move all coupled Bubble Tea production and behavioral test files into
  `internal/presentation` and change their package declaration from `tui` to `presentation`.
- Create `api.go` with aliases for every current exported type and constants, plus forwarding
  functions for `New`, `FoldDisplay`, `AllLoopsEventFilter`, and `RenderStatusLine`.
- Keep attachment error aliases in `errors.go`.
- Extend the import-boundary test so `internal/presentation` cannot import the root package.
- Update the README and repository guidance with the private presentation boundary.
- Refresh vendored metadata only if module inputs changed.

## Task 6: Verify

- `go test -race ./...`
- `make lint`
- `CGO_ENABLED=0 go build -trimpath ./...`
- `git diff --check`
- audit imports to ensure internal dependency direction and no stale `cli` or `sessionagent`
  vocabulary

Do not commit or push. The user will request that separately.
