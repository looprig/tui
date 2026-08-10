# TUI Root Facade Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the TUI repository root clean by retaining only its public facade while moving the coupled Bubble Tea implementation and behavioral tests into `internal/presentation`.

**Architecture:** The root `tui` package aliases public types and constants from `internal/presentation` and forwards its four public functions. `internal/presentation` contains the existing state machine unchanged and imports the existing leaf packages, but never imports the root facade. This is a structural migration with no intended API or behavioral change.

**Tech Stack:** Go, Bubble Tea v2, Lipgloss v2, Go internal packages, compile-time API assertions, race tests.

---

### Task 1: Pin root cleanliness and public compatibility

**Files:**
- Modify: `module_api_test.go`
- Modify: `package_boundaries_test.go`
- Create: `root_layout_test.go`

**Step 1: Add a root-layout test**

Read the repository root and reject every root Go file except `api.go`, `errors.go`,
`module_api_test.go`, `package_boundaries_test.go`, and `root_layout_test.go`.

**Step 2: Strengthen the facade compile test**

Keep compile-time references for every current exported root type, constant, and function:
`Agent`, `EventStream`, `OpenAgent`, `AgentBanner`, `AgentHolder`, `TerminalErrorHolder`,
`HandoffFinalizer`, `Screen`, `Status` and its constants, `ToolStatus` and its constants,
`ToolCallView`, `DisplayProjection`, `RestoreBacklogError`, `New`, `FoldDisplay`,
`AllLoopsEventFilter`, `RenderStatusLine`, and all attachment errors.

**Step 3: Add the presentation import rule**

Require `internal/presentation` to exist and reject any import of `github.com/looprig/tui` from
that package.

**Step 4: Run the focused test and verify RED**

Run:

```sh
GOWORK=off go test . -run 'TestRootContainsOnlyFacadeFiles|TestInternalPresentationPackageBoundaries' -count=1
```

Expected: FAIL because implementation files still occupy the root and
`internal/presentation` does not exist.

### Task 2: Move the presentation implementation as one unit

**Files:**
- Create directory: `internal/presentation/`
- Move: all root production `.go` files except `errors.go`
- Move: all root behavioral `*_test.go` files except the three facade/boundary tests

**Step 1: Move production and behavioral test files**

Move the coupled `Screen`, `sessionCore`, transcript, interaction, viewport, commands, handoff,
rendering, contract, fixture, and behavioral test files into `internal/presentation`.

**Step 2: Change package declarations mechanically**

Change `package tui` to `package presentation` in every moved file. Do not change behavior,
identifiers, or test assertions during the move.

**Step 3: Run the internal package test**

Run:

```sh
GOWORK=off go test ./internal/presentation -count=1
```

Expected: PASS once the package declaration move is complete.

### Task 3: Create the root public facade

**Files:**
- Create: `api.go`
- Keep: `errors.go`
- Test: `module_api_test.go`

**Step 1: Alias public types**

Alias every existing public presentation type from `internal/presentation` so consumer method
sets and type identity remain unchanged.

**Step 2: Re-export typed constants**

Re-export `StatusIdle`, `StatusRunning`, `StatusInterrupting`, `StatusResetting`, `ToolRunning`,
`ToolOK`, `ToolError`, and `ToolCancelled` using the aliased types.

**Step 3: Forward public functions**

Forward `New`, `FoldDisplay`, `AllLoopsEventFilter`, and `RenderStatusLine` without adding logic.

**Step 4: Run root and downstream compile tests**

Run:

```sh
GOWORK=off go test . ./runtime ./sessionadapter -count=1
```

Expected: PASS with the same public root API.

### Task 4: Update documentation and package rules

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`
- Modify: `docs/plans/2026-07-15-tui-package-boundaries-design.md`

**Step 1: Describe the facade**

Document the root as the stable consumer import and `internal/presentation` as the private
Bubble Tea implementation.

**Step 2: Document dependency direction**

State that internal packages never import the root facade and consumers never import internal
packages.

### Task 5: Verify the migration

**Step 1: Format and run the root-layout test**

```sh
GOWORK=off make fmt
GOWORK=off go test . -run 'TestRootContainsOnlyFacadeFiles|TestInternalPresentationPackageBoundaries' -count=1
```

Expected: PASS.

**Step 2: Run the complete TUI checks**

```sh
GOWORK=off go test -race ./...
GOWORK=off make lint
GOWORK=off CGO_ENABLED=0 go build -trimpath ./...
```

Expected: PASS.

**Step 3: Run the Carbon downstream race suite**

```sh
cd ../carbon
go test -race ./...
```

Expected: PASS.

**Step 4: Audit the workspace**

```sh
git diff --check
git diff --cached --name-only
```

Expected: no whitespace errors and no staged files.

Do not commit or push. The user will request that separately.
