# TUI Runtime Context and Loop Footer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show focused-loop model, effort, mode, and context occupancy; show rig environment plus primers/live delegates; and change available runtime values through dynamic slash-command trays.

**Architecture:** Harness exposes a narrow read-only mode catalog on live loop handles. CLI owns typed runtime option contracts, pure event projections, dynamic command/value completion, and rendering. SWE adapts its validated model catalog, inference metadata, session security ceiling, and loop/session controllers to those contracts; authoritative events remain the source of displayed current state.

**Tech Stack:** Go, Bubble Tea v2, Bubbles textarea, Lipgloss v2, harness enduring events, SWE model catalog, sandbox security ceiling.

---

## Execution setup

Work in coordinated feature worktrees for `harness`, `cli`, and `swe`; do not edit their `main`
worktrees concurrently. Keep `sandbox.Write` unchanged. During development, use the workspace/local
replaces so downstream modules compile against the local upstream edits. Publish and pin versions
only after all three local worktrees pass their race suites.

Reference design: `cli/docs/plans/2026-07-15-tui-runtime-context-and-loop-footer-design.md`.

### Task 1: Expose selectable mode names from harness loop handles

**Files:**
- Create: `harness/pkg/loop/mode_catalog.go`
- Modify: `harness/internal/sessionruntime/session.go:499-535`
- Test: `harness/internal/sessionruntime/mode_catalog_test.go`

**Step 1: Write the failing table-driven mode-catalog test**

Define a loop with base plus `plan` and `build`, obtain its public `loop.Handle`, assert it also
satisfies `loop.ModeCatalog`, and cover base-only, named modes, order, and defensive-copy cases.

```go
tests := []struct {
    name  string
    modes []loop.Mode
    want  []loop.ModeName
}{
    {name: "base only", want: []loop.ModeName{""}},
    {name: "base then named", modes: []loop.Mode{{Name: "plan"}, {Name: "build"}}, want: []loop.ModeName{"", "plan", "build"}},
}
```

After reading the first result, mutate it and assert a second call still returns the original
names.

**Step 2: Run the focused test and verify failure**

Run: `go test -race ./internal/sessionruntime -run TestLoopHandleModeCatalog -v`

Expected: FAIL because `loop.ModeCatalog` does not exist or the handle does not implement it.

**Step 3: Add the narrow public interface**

```go
package loop

// ModeCatalog is the read-only selectable-mode view implemented by live native handles.
type ModeCatalog interface {
    Modes() []ModeName
}
```

**Step 4: Implement it from the bound definition**

Add `func (h *loopHandle) Modes() []loop.ModeName` that iterates `h.bound.Modes()` in stable order,
copies only names, and returns a fresh slice. Do not expose `BoundMode` tools or instructions.

**Step 5: Run focused and package tests**

Run:

```bash
go test -race ./internal/sessionruntime -run TestLoopHandleModeCatalog -v
go test -race ./pkg/loop ./internal/sessionruntime
```

Expected: PASS.

**Step 6: Commit harness change**

```bash
git add pkg/loop/mode_catalog.go internal/sessionruntime/session.go internal/sessionruntime/mode_catalog_test.go
git commit -m "feat(loop): expose live mode catalog"
```

### Task 2: Define typed CLI runtime discovery and mutation contracts

**Files:**
- Create: `cli/tui/runtime.go`
- Modify: `cli/tui/agent.go:17-105`
- Modify: CLI test fakes in `cli/tui/screen_test.go`, `cli/tui/sessioncore_test.go`, and `cli/cli/run_test.go`
- Test: `cli/tui/runtime_test.go`

**Step 1: Write compile-time contract tests and pure visibility tests**

Cover zero/one/multiple choices and assert commands are enabled only for multiple choices.

```go
tests := []struct {
    name string
    n    int
    want bool
}{
    {name: "none", n: 0},
    {name: "fixed", n: 1},
    {name: "selectable", n: 2, want: true},
}
```

**Step 2: Run the focused test and verify failure**

Run: `go test -race ./tui -run 'TestRuntimeChoiceVisibility|TestAgentRuntimeContracts' -v`

Expected: FAIL because the types/interfaces do not exist.

**Step 3: Add typed option values**

Define `EnvironmentID`, `ModeOption`, `ModelOption`, `EnvironmentOption`,
`LoopRuntimeOptions`, and `EnvironmentRuntimeOptions`. Use `loop.ModeName`,
`inference.ModelKey`, and `inference.Effort`; never represent those domains with untyped strings.

```go
type EnvironmentID string

type ModeOption struct {
    Name        loop.ModeName
    Label       string
    Description string
}

type ModelOption struct {
    Key         inference.ModelKey
    Label       string
    Description string
}
```

**Step 4: Add segregated runtime interfaces**

```go
type RuntimeCatalog interface {
    LoopRuntimeOptions(uuid.UUID) LoopRuntimeOptions
    EnvironmentRuntimeOptions() EnvironmentRuntimeOptions
}

type RuntimeController interface {
    SetActiveLoop(context.Context, uuid.UUID) error
    SetLoopMode(context.Context, uuid.UUID, loop.ModeName) error
    SetLoopModel(context.Context, uuid.UUID, inference.ModelKey) error
    SetLoopEffort(context.Context, uuid.UUID, inference.Effort) error
    SetEnvironment(context.Context, EnvironmentID) error
}
```

Embed both focused interfaces in `tui.Agent`. Update fakes with deterministic empty catalogs and
recording mutation methods. Empty choices are the fail-closed default.

**Step 5: Run focused and CLI package tests**

Run:

```bash
go test -race ./tui -run 'TestRuntimeChoiceVisibility|TestAgentRuntimeContracts' -v
go test -race ./tui ./cli
```

Expected: PASS.

**Step 6: Commit CLI contract**

```bash
git add tui/runtime.go tui/runtime_test.go tui/agent.go tui/screen_test.go tui/sessioncore_test.go cli/run_test.go
git commit -m "feat(tui): define runtime control contracts"
```

### Task 3: Fold authoritative loop runtime and context events

**Files:**
- Create: `cli/tui/runtime_state.go`
- Test: `cli/tui/runtime_state_test.go`
- Modify: `cli/tui/screen.go` event and restore paths

**Step 1: Write failing reducer tests**

Use table-driven event sequences covering:

- `LoopStarted` initializes mode/model/limits/effort;
- `LoopModeChanged` replaces mode and resolved runtime;
- `LoopInferenceChanged` replaces model/effort without changing mode;
- `ContextMeasured` replaces the loop's latest measurement;
- `SecurityCeilingChanged` updates environment current state;
- events from two loops never bleed into each other;
- replay and live folds produce identical snapshots.

**Step 2: Run the reducer tests and verify failure**

Run: `go test -race ./tui -run TestRuntimeStateApply -v`

Expected: FAIL because `runtimeState` is undefined.

**Step 3: Implement the pure reducer**

Store a map keyed by loop UUID and a session environment ordinal/value. Clone maps on write if the
surrounding reducer uses value semantics. Expose read-only accessors returning values, not internal
map references.

```go
type loopRuntimeState struct {
    mode        loop.ModeName
    runtime     event.ModelRuntime
    measurement event.ContextMeasurement
    measured    bool
}
```

**Step 4: Wire both backlog and live events through the same reducer**

Update `Screen` so its existing event path applies every event once to `runtimeState` before
rendering. Ensure cold restore uses that same function rather than a second implementation.

**Step 5: Run reducer and restore-equivalence tests**

Run:

```bash
go test -race ./tui -run 'TestRuntimeStateApply|Test.*Restore.*Runtime' -v
go test -race ./tui
```

Expected: PASS.

**Step 6: Commit runtime projection**

```bash
git add tui/runtime_state.go tui/runtime_state_test.go tui/screen.go
git commit -m "feat(tui): project loop runtime events"
```

### Task 4: Generalize completion into dynamic command and value trays

**Files:**
- Modify: `cli/tui/components/slashcomplete.go`
- Modify: `cli/tui/components/slashcomplete_test.go`
- Create: `cli/tui/components/valuecomplete.go`
- Test: `cli/tui/components/valuecomplete_test.go`
- Modify: `cli/tui/interaction.go`
- Modify: `cli/tui/interaction_test.go`

**Step 1: Write failing dynamic-command tests**

Replace assumptions about the global static list with an injected command slice. Cover the base
commands plus runtime commands appearing/disappearing as choice counts change. Preserve existing
related-word matching (`new` → `/clear`).

**Step 2: Write failing value-tray tests**

Cover case-insensitive label/alias filtering, stable order, exact selection, empty result, Up/Down,
Tab completion, Enter selection, windowing, mouse row selection, and selected-background rendering.

**Step 3: Run focused component tests and verify failure**

Run:

```bash
go test -race ./tui/components -run 'Test.*Slash|Test.*ValueComplete' -v
```

Expected: FAIL on the new injected catalog/value component cases.

**Step 4: Inject the slash catalog**

Change `NewSlashComplete` to receive a defensive copy of `[]SlashCmd`. Keep the canonical base list
as a constructor input owned by `tui`, not mutable package global state.

**Step 5: Implement a generic value completion component**

Define a typed component-local key and row:

```go
type ValueKey string
type Value struct {
    Key         ValueKey
    Label       string
    Description string
    Aliases     []string
}
```

Reuse the slash tray's renderer/selection helpers so value rows get the same rail, background,
windowing, mouse selection, and three-step animation.

**Step 6: Add interaction state for command arguments**

Teach `interactionModel` to distinguish command completion from runtime-value completion after
`/model `, `/effort `, `/env `, or `/mode `. Emit a typed UI action containing the runtime command
and selected catalog key; do not mutate an agent from the interaction layer.

**Step 7: Run component and interaction tests**

Run:

```bash
go test -race ./tui/components ./tui -run 'Test.*(Slash|Value|RuntimeCommand)' -v
```

Expected: PASS.

**Step 8: Commit tray generalization**

```bash
git add tui/components/slashcomplete.go tui/components/slashcomplete_test.go tui/components/valuecomplete.go tui/components/valuecomplete_test.go tui/interaction.go tui/interaction_test.go
git commit -m "feat(tui): add dynamic runtime value trays"
```

### Task 5: Dispatch runtime slash commands safely

**Files:**
- Create: `cli/tui/runtime_commands.go`
- Test: `cli/tui/runtime_commands_test.go`
- Modify: `cli/tui/messages.go`
- Modify: `cli/tui/screen.go`
- Modify: `cli/tui/sessioncore.go` only if shared result handling belongs there

**Step 1: Write failing dispatch tests**

Cover each command targeting the focused loop captured when the tray opens, `/env` targeting the
session, timeouts, typed errors, stale choices, focus change closing the tray, and no optimistic
runtime projection update.

**Step 2: Run the tests and verify failure**

Run: `go test -race ./tui -run TestRuntimeCommandDispatch -v`

Expected: FAIL because runtime commands/messages are not implemented.

**Step 3: Add bounded Tea commands and typed result messages**

Mirror `compactToLoopCmd`: create a short runtime-change timeout, call the narrow agent mutation,
and return a message carrying command kind, target loop, selected key, and error.

**Step 4: Route actions without blocking Update**

Intercept runtime UI actions in `Screen.routeToInteraction`. Capture `focusedLoopID` at tray open,
resolve the typed selected value from the current option snapshot, and dispatch the Tea command.
On success wait for the enduring event to change displayed state. On failure append one faint typed
notice and retain the old state.

**Step 5: Preserve click-opened drafts**

Add a one-slot saved draft used only when a runtime link opens a tray. Escape and command completion
restore it; manual slash typing keeps existing clear-on-command behavior. Add tests with multiline
drafts.

**Step 6: Run focused and full TUI tests**

Run:

```bash
go test -race ./tui -run 'TestRuntimeCommandDispatch|TestRuntimeDraft' -v
go test -race ./tui
```

Expected: PASS.

**Step 7: Commit runtime dispatch**

```bash
git add tui/runtime_commands.go tui/runtime_commands_test.go tui/messages.go tui/screen.go tui/sessioncore.go
git commit -m "feat(tui): dispatch runtime slash commands"
```

### Task 6: Extend the status line with model, effort, and context occupancy

**Files:**
- Modify: `cli/tui/statusline.go`
- Modify: `cli/tui/statusline_test.go`
- Modify: `cli/tui/screen.go:1681-1725`
- Modify: `cli/tui/surface.go`
- Test: `cli/tui/screen_test.go`

**Step 1: Write failing rendering tables**

Cover idle/running, elapsed/no elapsed, known/unknown model, auto/explicit effort, missing/exact/
heuristic/over-limit context, narrow widths, and option counts controlling link hit regions.

Expected representative plain text:

```text
● streaming… (25s · gpt-5.4 · high)                         ███░░░░░ 42%
```

**Step 2: Run focused tests and verify failure**

Run: `go test -race ./tui -run 'Test.*Status.*Runtime|Test.*ContextMeter' -v`

Expected: FAIL because status metadata and meter are absent.

**Step 3: Implement pure status layout and hit testing**

Create a status layout value containing rendered text plus model/effort cell spans. Keep only the
lifecycle glyph/label animated. Format `EffortNone` as `auto`. Right-align the context meter; clamp
bar fill at 100%, preserve numeric values over 100%, and prefix heuristic percentages with `~`.

**Step 4: Add responsive degradation**

At narrow widths remove meter blocks first, then effort, while retaining lifecycle label, model,
and numeric percentage as long as possible. Truncate rather than allowing terminal soft-wrap.

**Step 5: Wire mouse hover/click regions**

Extend screen region layout and hover state so model and effort use the existing semantic-link glow.
A left click opens `/model ` or `/effort ` only when multiple options exist.

**Step 6: Run status, width, and mouse tests**

Run:

```bash
go test -race ./tui -run 'Test.*(Status|ContextMeter|RuntimeHover|RuntimeClick)' -v
```

Expected: PASS.

**Step 7: Commit status line**

```bash
git add tui/statusline.go tui/statusline_test.go tui/screen.go tui/screen_test.go tui/surface.go
git commit -m "feat(tui): show focused loop runtime status"
```

### Task 7: Make the composer three rows and show mode at bottom right

**Files:**
- Modify: `cli/tui/components/input.go`
- Modify: `cli/tui/components/input_test.go`
- Modify: `cli/tui/styles/styles.go`
- Modify: `cli/tui/surface.go`
- Modify: `cli/tui/surface_test.go`
- Modify: `cli/tui/screen.go`

**Step 1: Write failing composer layout tests**

Cover one-line minimum height of three physical rows, multiline growth between fixed padding rows,
ten-line editor cap, empty mode, one fixed mode, multiple interactive modes, right alignment, and
width truncation.

**Step 2: Run focused tests and verify failure**

Run:

```bash
go test -race ./tui/components -run TestInputBoxRuntimePadding -v
go test -race ./tui -run TestComposerModeLine -v
```

Expected: FAIL because the composer has no runtime footer row.

**Step 3: Add explicit top/bottom padding rendering**

Use `InputBox.SetVerticalPadding(1)` or a single-purpose equivalent while keeping the textarea
content floor at one. Ensure every physical row carries the same left rail and no full-width border
that could strand on resize.

**Step 4: Render the mode in the bottom padding row**

Pass a pure composer metadata value through `surfaceInputs`. Hide when no named modes exist, render
fixed mode muted, and render multiple-choice mode with link hover/hit region. Clicking opens
`/mode `.

**Step 5: Re-run composer, surface-budget, and resize tests**

Run:

```bash
go test -race ./tui/components ./tui -run 'Test.*(InputBox|Composer|Surface|Resize)' -v
```

Expected: PASS with exact physical-row counts.

**Step 6: Commit composer change**

```bash
git add tui/components/input.go tui/components/input_test.go tui/styles/styles.go tui/surface.go tui/surface_test.go tui/screen.go
git commit -m "feat(tui): add runtime context to composer"
```

### Task 8: Replace the capped loop bar with the rig/environment footer

**Files:**
- Modify: `cli/tui/transcript.go`
- Modify: `cli/tui/transcript_test.go`
- Modify: `cli/tui/loopbar.go`
- Modify: `cli/tui/loopbar_test.go`
- Modify: `cli/tui/screen.go`
- Modify: `cli/tui/screen_test.go`
- Modify: `cli/tui/surface.go`
- Modify: `cli/tui/surface_test.go`

**Step 1: Write failing topology tests**

Cover multiple root `LoopStarted` events becoming primers, delegates identified by spawning cause/
parent tool use, stable creation order, primers remaining visible while idle, live/gated delegates,
and focused-idle delegate retention until focus changes.

**Step 2: Write failing footer layout tests**

Cover rig banner, all five environment labels, root elision, selected/unselected marks, middle-dot
separators, exact one-row layout, deterministic multi-row wrapping, continuation indentation,
hover spans, and hit testing from the same layout.

**Step 3: Run focused tests and verify failure**

Run:

```bash
go test -race ./tui -run 'Test.*(Primer|LoopFooter|ActiveBarEntries)' -v
```

Expected: FAIL because primer provenance and wrapped footer do not exist.

**Step 4: Extend the transcript's single loop table**

Add primer provenance to the existing loop metadata; do not create a second topology registry.
Record it from `LoopStarted`. Return it through `loops()` with defensive value semantics.

**Step 5: Implement wrapped footer layout and hit testing**

Refactor `loopBar` into a footer value that accepts banner name, environment snapshot, loop entries,
focused/active IDs, width, and hover phase. Return rendered physical rows and segment spans. Remove
the fixed visible cap and `… +N`; wrap at segment boundaries.

**Step 6: Make clicks update focus and harness active loop**

Keep local focus immediate, dispatch bounded `SetActiveLoop`, and revert/surface a notice only if the
controller refuses. Ensure a focus change closes/rebuilds a loop-scoped runtime tray.

**Step 7: Account for variable footer height**

Update surface layout/region hit testing/live-tail capacity so every wrapped footer row is reserved.
Fuzz arbitrary names and widths; assert no produced line exceeds width and reported height equals
physical line count.

**Step 8: Run footer, surface, mouse, and fuzz smoke tests**

Run:

```bash
go test -race ./tui -run 'Test.*(LoopFooter|Primer|BarClick|Surface)' -v
go test ./tui -run '^$' -fuzz=FuzzLoopFooterLayout -fuzztime=30s
```

Expected: PASS and no panic/overflow.

**Step 9: Commit footer**

```bash
git add tui/transcript.go tui/transcript_test.go tui/loopbar.go tui/loopbar_test.go tui/screen.go tui/screen_test.go tui/surface.go tui/surface_test.go
git commit -m "feat(tui): show rig environment and loop footer"
```

### Task 9: Implement SWE runtime catalogs

**Files:**
- Create: `swe/swarms/swe/runtime_options.go`
- Test: `swe/swarms/swe/runtime_options_test.go`
- Modify: `swe/swarms/swe/model_catalog.go`
- Modify: `swe/swarms/swe/confinement.go`
- Modify: `swe/swarms/swe/confinement_test.go`
- Modify: `swe/swarms/swe/agent.go`

**Step 1: Write failing model-choice tests**

Cover Standard-before-Premium ordering, Economy exclusion, deduplication by model key, current-model
insertion, invalid/unknown loop, and trusted key-to-descriptor resolution.

**Step 2: Write failing effort-choice tests**

Cover no thinking capability, unknown API format, OpenAI without Max, Anthropic/Gemini with Max,
and current/default-only fallback.

**Step 3: Write failing environment-ladder tests**

Cover every cap from ZeroTrust through Unconfined, canonical labels (`Writable` for `sandbox.Write`),
`write`/`writable` aliases, and omission of Unconfined unless the composition root acknowledges it.

**Step 4: Run focused SWE tests and verify failure**

Run:

```bash
go test -race ./swarms/swe -run 'Test.*(RuntimeModel|RuntimeEffort|EnvironmentOptions)' -v
```

Expected: FAIL because runtime catalogs do not exist.

**Step 5: Implement an immutable SWE runtime catalog**

Build it once from validated `Config.ModelCatalog`, security cap, and workspace root. Keep a map from
model key to full secret-free descriptor. Return defensive option slices per query and filter by the
current loop handle/model. Use the harness `loop.ModeCatalog` capability for mode names.

**Step 6: Wire catalog dependencies into `sessionAgent`**

Pass a narrow catalog dependency into `newSessionAgent`; do not pass the whole `Config`. Update new,
restore, and test constructors.

**Step 7: Run focused and package tests**

Run:

```bash
go test -race ./swarms/swe -run 'Test.*(Runtime|Environment|ModelCatalog)' -v
go test -race ./swarms/swe
```

Expected: PASS.

**Step 8: Commit SWE catalog**

```bash
git add swarms/swe/runtime_options.go swarms/swe/runtime_options_test.go swarms/swe/model_catalog.go swarms/swe/confinement.go swarms/swe/confinement_test.go swarms/swe/agent.go
git commit -m "feat(swe): expose runtime option catalogs"
```

### Task 10: Implement SWE runtime mutations and fresh-session bootstrap replay

**Files:**
- Modify: `swe/swarms/swe/agent.go`
- Modify: `swe/swarms/swe/agent_test.go`
- Modify: `swe/swarms/swe/persistence.go`
- Modify: `swe/swarms/swe/rig_restore_integration_test.go`

**Step 1: Write failing adapter mutation tests**

Use a recording `session.SessionController` and loop controllers. Cover active-loop selection,
mode/model/effort routing to the requested loop, environment routing to the session, unknown IDs,
model keys absent from the trusted catalog, context cancellation, and unchanged typed errors.

**Step 2: Write failing fresh-bootstrap tests**

Construct a new session whose durable log already contains multiple primer `LoopStarted` events.
Assert `ReplayBacklog` returns those public enduring events before live subscribe. Verify a restored
session still returns its complete public enduring history and live subscribe does not duplicate
bootstrap events.

**Step 3: Run focused tests and verify failure**

Run:

```bash
go test -race ./swarms/swe -run 'TestSessionAgent.*(Runtime|Bootstrap)' -v
```

Expected: FAIL because mutations and new-session replay are missing.

**Step 4: Implement mutation methods**

Resolve loop controllers through `sess.LoopController`. Resolve model keys only through the trusted
runtime catalog. Call `SetMode`, `Change(ChangeModel)`, `Change(ChangeEffort)`,
`SetSecurityCeiling`, and `SetActiveLoop` directly; preserve typed errors.

**Step 5: Expand cold replay for new sessions**

Perform the visibility-filtered enduring replay at adapter construction for both new and restored
sessions. For new sessions retain the existing topology/runtime events already committed before the
TUI subscribes. Keep one live subscription and the gate fold ordering unchanged.

**Step 6: Run focused, package, and restore integration tests**

Run:

```bash
go test -race ./swarms/swe -run 'TestSessionAgent.*(Runtime|Bootstrap)' -v
go test -race ./swarms/swe
go test -tags integration -race ./swarms/swe -run 'TestRigRestore.*Runtime' -v
```

Expected: PASS.

**Step 7: Commit SWE adapter**

```bash
git add swarms/swe/agent.go swarms/swe/agent_test.go swarms/swe/persistence.go swarms/swe/rig_restore_integration_test.go
git commit -m "feat(swe): control loop runtime from CLI"
```

### Task 11: Cross-repository integration and dependency pins

**Files:**
- Modify after upstream tags: `cli/go.mod`, `cli/go.sum`, `cli/vendor/modules.txt`, relevant `cli/vendor/github.com/looprig/harness/**`
- Modify after upstream tags: `swe/go.mod`, `swe/go.sum`, `swe/vendor/modules.txt`, relevant `swe/vendor/github.com/looprig/{harness,cli}/**`
- Do not modify generated vendor files by hand

**Step 1: Run local workspace integration before publishing**

Run harness, CLI, and SWE against local worktrees with `go.work`/temporary replaces. Confirm the SWE
binary compiles with the new CLI Agent contract and harness mode catalog.

**Step 2: Run every module's required checks**

Harness:

```bash
make fmt
go test -race ./...
make secure
```

CLI:

```bash
make fmt
go test -race ./...
make secure
CGO_ENABLED=0 go build -trimpath ./...
```

SWE:

```bash
make fmt
make test
make lint
make build
```

Expected: all PASS.

**Step 3: Perform a manual TUI smoke test**

Verify:

- status model/effort and context meter follow focused loop;
- the composer is three rows and grows correctly;
- no named modes hides the mode label and `/mode`;
- runtime commands appear only with multiple choices;
- click-opened trays preserve a draft;
- multiple primers remain visible;
- delegates appear live and wrap to another footer row;
- loop/environment/model/effort hover uses the link animation;
- changing mode/model/effort/environment produces authoritative event-driven updates;
- resize produces no stranded rows or width overflow.

**Step 4: Request code review before publishing**

Use `@superpowers:requesting-code-review` separately in each repository. Resolve findings and rerun
the affected race/security checks.

**Step 5: Publish and pin in dependency order only with user authorization**

Tag/push harness, update CLI to the harness tag, tag/push CLI, then update SWE to both tags. Run
`go mod tidy` and `go mod vendor` in the consuming modules, followed by clean `GOWORK=off` builds.
Do not push or tag merely because local integration is green; obtain the user's release instruction.

**Step 6: Commit dependency updates**

Use repository-specific commits such as:

```bash
git commit -m "chore: bump harness for runtime catalogs"
git commit -m "chore: bump CLI runtime controls"
```

### Task 12: Final verification against the design

**Files:**
- Verify: `cli/docs/plans/2026-07-15-tui-runtime-context-and-loop-footer-design.md`
- Update only if implementation required an approved design correction

**Step 1: Check every design invariant**

Walk the design sections for layout, option counts, provider effort mapping, sandbox cap behavior,
event authority, topology bootstrap, error handling, and deferred shortcuts/modals. Record any
intentional deviation and get user approval before changing the spec.

**Step 2: Confirm clean worktrees**

Run `git status --short` in harness, CLI, and SWE. Expected: empty after intended commits.

**Step 3: Record exact verification evidence**

Report the commands and passing results from Task 11, plus final commit hashes and version pins if a
release was authorized.
