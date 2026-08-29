# Current Tray Choice Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show the active mode, model, effort, and session in blue and initially scroll each tray to that active choice.

**Architecture:** Runtime catalogs mark the option corresponding to the live loop, avoiding unsafe inference from model aliases. TUI carries that marker into the shared tray engine, which independently renders active state and cursor selection and initially selects the active row; sessions are marked by comparing listed IDs with the current agent ID.

**Tech Stack:** Go, Bubble Tea v2, Bubbles list v2, Lip Gloss v2, table/focused Go tests with the race detector.

---

### Task 1: Teach the shared tray to represent an active row

**Files:**
- Modify: `components/completiontray.go`
- Modify: `components/traylist.go`
- Modify: `components/traylist_test.go`
- Modify: `styles/selection.go`
- Modify: `styles/selection_test.go`

**Step 1: Write the failing tests**

Add focused tests that construct rows with one current row and assert:

```go
tray := newTrayList([]completionTrayRow{
    {primary: "one"},
    {primary: "two", current: true},
    {primary: "three"},
}, 0, trayLayout{})
if got := tray.Selected().primary; got != "two" { /* fail */ }
```

After moving to `three`, render the tray and assert the unselected `two` text
uses the semantic current-choice blue while `three` retains the selected band.
Also construct enough rows to exceed a small window and assert the current row
is present in `ViewWindow` immediately.

**Step 2: Run tests to verify they fail**

Run: `go test -race ./components ./styles -run 'TestTrayList.*Current|TestCurrentChoice'`

Expected: FAIL because `completionTrayRow` has no current marker and the engine
still selects the first row.

**Step 3: Implement the minimal shared behavior**

Add a `current bool` field to the internal row/item representation. Add a
semantic current-choice text helper in `styles` using `CardBorderColor`. During
construction select the first selectable current row, falling back to the first
selectable row. In the delegate, style only an unselected current row's primary
text; selected-row styling retains precedence.

**Step 4: Run tests to verify they pass**

Run: `go test -race ./components ./styles -run 'TestTrayList.*Current|TestCurrentChoice'`

Expected: PASS.

**Step 5: Commit**

```bash
git add components/completiontray.go components/traylist.go components/traylist_test.go styles/selection.go styles/selection_test.go
git commit -m "feat(components): distinguish current tray choices"
```

### Task 2: Carry current state through value and session trays

**Files:**
- Modify: `components/valuecomplete.go`
- Modify: `components/valuecomplete_test.go`
- Modify: `components/sessioncomplete.go`
- Modify: `components/sessioncomplete_test.go`

**Step 1: Write the failing tests**

Add `Current bool` to test fixtures before adding it to production structs.
Assert a late current value is selected initially in plain and grouped trays,
and a late current session is selected initially.

**Step 2: Run tests to verify they fail**

Run: `go test -race ./components -run 'Test(Value|Model|Session)Complete.*Current'`

Expected: FAIL because the public component items cannot carry current state.

**Step 3: Implement the minimal propagation**

Add `Current bool` to `ValueItem` and `SessionItem` and copy it into each
`completionTrayRow`. Do not change filtering or payload lookup.

**Step 4: Run tests to verify they pass**

Run: `go test -race ./components -run 'Test(Value|Model|Session)Complete.*Current'`

Expected: PASS.

**Step 5: Commit**

```bash
git add components/valuecomplete.go components/valuecomplete_test.go components/sessioncomplete.go components/sessioncomplete_test.go
git commit -m "feat(components): surface current picker values"
```

### Task 3: Propagate current runtime and session choices in TUI

**Files:**
- Modify: `internal/presentation/runtimecontrol.go`
- Modify: `internal/presentation/runtimecontrol_test.go`
- Modify: `internal/presentation/screen.go`
- Modify: `internal/presentation/sessionbrowser_test.go`
- Modify: `api.go`

**Step 1: Write the failing tests**

Extend runtime catalog fixtures with current options and assert
`queryRuntimeChoices` produces `ValueItem.Current`. Add a session-browser test
whose current agent is not the first result and assert the tray initially
selects the agent's session.

**Step 2: Run tests to verify they fail**

Run: `go test -race ./internal/presentation -run 'TestRuntime.*Current|TestSessionsListed.*Current'`

Expected: FAIL because runtime option records and session item construction do
not carry active state.

**Step 3: Implement the minimal propagation**

Add `Current bool` to `ModeOption`, `ModelOption`, and `EffortOption`; ensure the
root facade aliases expose the compatible structs as they already do. Copy the
field in `queryRuntimeChoices`, and set `SessionItem.Current` in
`handleSessionsListed` by comparing with `m.agent.SessionID()`.

**Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/presentation -run 'TestRuntime.*Current|TestSessionsListed.*Current'`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/presentation/runtimecontrol.go internal/presentation/runtimecontrol_test.go internal/presentation/screen.go internal/presentation/sessionbrowser_test.go api.go
git commit -m "feat(presentation): open trays on current choices"
```

### Task 4: Mark Carbon's live runtime options

**Files:**
- Modify: `../carbon/internal/app/runtime_controls.go`
- Modify: `../carbon/internal/app/runtime_controls_test.go`

**Step 1: Write the failing tests**

In the existing runtime-option tests, assert exactly one returned mode, model,
and effort has `Current` set and that each corresponds to `handle.Mode()`, the
aliased current model candidate, and `handle.Model().Sampling.Effort`.

**Step 2: Run tests to verify they fail**

Run from the workspace root with the local TUI module active:
`go test -race ./carbon/internal/app -run 'TestRuntimeOptions.*Current'`

Expected: FAIL because Carbon never marks returned options.

**Step 3: Implement exact current matching**

Capture `selectedMode := handle.Mode()`, `selectedModel := handle.Model()`, and
the model's effort. Mark modes and efforts by typed equality. Mark models using
`runtimeModelKeyFor`, the same full provider/format/base/name identity already
used by `currentPrimerCandidate`, so aliases and duplicate display labels do not
affect correctness.

**Step 4: Run tests to verify they pass**

Run: `go test -race ./carbon/internal/app -run 'TestRuntimeOptions.*Current'`

Expected: PASS.

**Step 5: Commit**

```bash
git -C ../carbon add internal/app/runtime_controls.go internal/app/runtime_controls_test.go
git -C ../carbon commit -m "feat(app): identify current runtime choices"
```

### Task 5: Verify both repositories

**Files:**
- Verify only

**Step 1: Format changed Go files**

Run from `tui`: `gofmt -w components/*.go internal/presentation/*.go styles/*.go`

Run from `carbon`: `gofmt -w internal/app/runtime_controls.go internal/app/runtime_controls_test.go`

**Step 2: Run TUI's required checks**

Run from `tui`: `go test -race ./...`

Run from `tui`: `GOWORK=off go test ./...`

Run from `tui`: `CGO_ENABLED=0 go build -trimpath ./...`

Expected: all commands exit 0.

**Step 3: Run Carbon's workspace checks**

Run from the workspace root: `go test -race ./carbon/...`

Expected: exit 0 using the local TUI API.

**Step 4: Inspect repository-local diffs and status**

Run: `git -C tui diff --check && git -C tui status --short`

Run: `git -C carbon diff --check && git -C carbon status --short`

Expected: no whitespace errors; only intentional files are changed or committed.

