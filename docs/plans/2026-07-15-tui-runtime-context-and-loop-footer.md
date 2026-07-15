# TUI Runtime Status, Controls, Session Browser, and Loop Footer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show authoritative focused-loop runtime state and topology, expose optional typed controls, and resume previous sessions through a two-line TUI tray.

**Architecture:** Harness adds only a read-only mode catalog. TUI keeps its required `Agent` contract stable, folds current values from enduring events, detects optional runtime capabilities, and receives a process-scoped optional session browser. CodeRig maps its validated model, effort, access, and session catalogs into those generic contracts. Delivery is split into an independently releasable visibility phase and a controls/navigation phase.

**Tech Stack:** Go, Bubble Tea, Lip Gloss, Harness enduring events, Harness session catalog, CodeRig session store, race-enabled Go tests.

---

Reference design: `docs/plans/2026-07-15-tui-runtime-context-and-loop-footer-design.md`.

Use the current module names and package boundaries: `harness`, `tui`, and `coderig`. Presentation
lives in `tui/internal/presentation`. Commit steps are checkpoints for later execution and must not
be run until the repository owner authorizes commits.

## Phase 1: authoritative visibility

### Task 1: Add the Harness mode catalog

**Files:**

- Modify: `harness/pkg/loop/controller.go`
- Modify: `harness/internal/sessionruntime/topology.go`
- Test: `harness/internal/sessionruntime/topology_test.go`

**Step 1: Write failing tests**

Prove every native new/restored primer and delegate handle satisfies a separate `loop.ModeCatalog`,
returns base plus named modes in definition order, and returns defensive slices.

**Step 2: Verify failure**

Run:

```bash
cd harness
go test -race ./internal/sessionruntime -run ModeCatalog -v
```

Expected: FAIL because `loop.ModeCatalog` does not exist.

**Step 3: Implement the narrow capability**

```go
type ModeCatalog interface {
    Modes() []ModeName
}
```

Do not expand `loop.Handle` and do not expose prompts, tools, permissions, or mutable definition
state.

**Step 4: Verify**

```bash
cd harness
go test -race ./pkg/loop ./internal/sessionruntime
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C harness add pkg/loop/controller.go internal/sessionruntime/topology.go internal/sessionruntime/topology_test.go
git -C harness commit -m "feat(loop): expose selectable mode catalog"
```

### Task 2: Define optional TUI runtime contracts

**Files:**

- Create: `tui/internal/presentation/runtimecontrol.go`
- Modify: `tui/api.go`
- Modify: `tui/internal/presentation/screen.go`
- Test: `tui/internal/presentation/contract_test.go`
- Test: `tui/internal/presentation/dependency_test.go`

**Step 1: Write failing contract tests**

Prove the existing minimal fake still satisfies `Agent` without runtime methods. Add separate fakes
for discovery-only and discovery-plus-mutation and assert capability detection is independent.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./internal/presentation -run 'RuntimeCapability|AgentContract' -v
```

Expected: FAIL because the optional contracts do not exist.

**Step 3: Add typed choice-only contracts**

```go
type RuntimeCatalog interface {
    LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error)
    AccessOptions(context.Context) (AccessOptions, error)
}

type RuntimeController interface {
    SetMode(context.Context, uuid.UUID, ModeID) error
    SetModel(context.Context, uuid.UUID, ModelID) error
    SetEffort(context.Context, uuid.UUID, EffortID) error
    SetAccess(context.Context, AccessID) error
}
```

Catalogs contain options only, never current values. Keep canonical types in
`internal/presentation` and alias them from root `tui/api.go`. Introduce no public sibling package
and do not import root `tui` from presentation.

**Step 4: Verify**

```bash
cd tui
go test -race . ./internal/presentation
```

Expected: PASS with old agents unchanged.

**Step 5: Commit when authorized**

```bash
git -C tui add api.go internal/presentation/runtimecontrol.go internal/presentation/screen.go internal/presentation/contract_test.go internal/presentation/dependency_test.go
git -C tui commit -m "feat(tui): add optional runtime capabilities"
```

### Task 3: Fold authoritative runtime state

**Files:**

- Create: `tui/internal/presentation/runtimeprojection.go`
- Create: `tui/internal/presentation/runtimeprojection_test.go`
- Modify: `tui/internal/presentation/screen.go`
- Modify: `tui/internal/presentation/restore.go`
- Test: `tui/internal/presentation/restore_test.go`

**Step 1: Write reducer tests**

Cover `LoopStarted`, `LoopModeChanged`, `LoopInferenceChanged`, `ContextMeasured`,
`SecurityLimitChanged`, delayed events, replay/live equivalence, and topology provenance. Assert
`ActiveLoopChanged` never moves presentation focus.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./internal/presentation -run RuntimeProjection -v
```

Expected: FAIL because the reducer does not exist.

**Step 3: Implement one pure sequence-aware reducer**

Keep a map keyed by loop UUID plus session access. Fold current values only from events. Apply the
same reducer from cold restore and live event paths. Seed initial focus from `Agent.ActiveLoopID()`,
then let explicit TUI navigation own it.

**Step 4: Verify**

```bash
cd tui
go test -race ./internal/presentation -run 'RuntimeProjection|Restore' -v
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C tui add internal/presentation/runtimeprojection.go internal/presentation/runtimeprojection_test.go internal/presentation/screen.go internal/presentation/restore.go internal/presentation/restore_test.go
git -C tui commit -m "feat(tui): project authoritative runtime state"
```

### Task 4: Add fresh-session bootstrap replay

**Files:**

- Modify: `tui/sessionadapter/adapter.go`
- Test: `tui/sessionadapter/adapter_test.go`
- Test: `tui/sessionadapter/api_test.go`
- Modify: `coderig/swarm.go`
- Test: `coderig/sessionadapter_test.go`
- Test: `coderig/persistence_integration_test.go`

**Step 1: Write failing boundary tests**

Keep `sessionadapter.New` backlog-free. Add a replay-aware new-session constructor that reads
already-durable public enduring events, folds gates once, releases failed partial adapters, and
does not lose or duplicate events across cold replay and live subscription.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./sessionadapter -run 'NewWithReplay|BootstrapBoundary' -v
```

Expected: FAIL because the constructor does not exist.

**Step 3: Implement the shared cold-replay path**

Add conceptually `NewWithReplay(ctx, sess, store)` and share initialization with `Restore`. CodeRig
uses the replay-aware constructor for new TUI sessions while headless/simple callers may retain
`New`.

**Step 4: Verify TUI and CodeRig**

```bash
cd tui
go test -race ./sessionadapter
cd ../coderig
go test -race ./... -run 'SessionAdapter|FreshSession|ReplayBacklog'
```

Expected: PASS with equivalent fresh/restored topology projections.

**Step 5: Commit when authorized**

```bash
git -C tui add sessionadapter/adapter.go sessionadapter/adapter_test.go sessionadapter/api_test.go
git -C tui commit -m "feat(sessionadapter): replay fresh session bootstrap"
git -C coderig add swarm.go sessionadapter_test.go persistence_integration_test.go
git -C coderig commit -m "feat(coderig): bootstrap fresh TUI sessions"
```

### Task 5: Render model, effort, and context in the status line

**Files:**

- Modify: `tui/internal/presentation/statusline.go`
- Test: `tui/internal/presentation/statusline_test.go`
- Modify: `tui/internal/presentation/surface.go`
- Test: `tui/internal/presentation/surface_test.go`
- Modify: `tui/internal/presentation/screen.go`
- Test: `tui/internal/presentation/screen_test.go`

**Step 1: Write failing format and width tests**

Cover idle/running status, missing effort, elapsed time, heuristic and over-limit context, zero
limit, narrow width, and fixed versus interactive metadata. Lifecycle text must never disappear.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./internal/presentation -run 'StatusLine|ContextMeter' -v
```

**Step 3: Implement pure layout**

Render lifecycle and runtime metadata left, context right. Drop context blocks before percentage,
then effort before model. Animate lifecycle only and derive hit regions from the rendered layout.

**Step 4: Verify**

```bash
cd tui
go test -race ./internal/presentation -run 'StatusLine|ContextMeter|Surface' -v
```

Expected: PASS without implicit wrapping.

**Step 5: Commit when authorized**

```bash
git -C tui add internal/presentation/statusline.go internal/presentation/statusline_test.go internal/presentation/surface.go internal/presentation/surface_test.go internal/presentation/screen.go internal/presentation/screen_test.go
git -C tui commit -m "feat(tui): show focused loop runtime status"
```

### Task 6: Add the composer mode row

**Files:**

- Modify: `tui/internal/presentation/surface.go`
- Test: `tui/internal/presentation/surface_test.go`
- Modify: `tui/internal/presentation/interaction.go`
- Test: `tui/internal/presentation/interaction_test.go`

**Step 1: Write failing tests**

Cover the three-row minimum, multiline growth, editor cap, absent/fixed/editable mode, narrow
width, and mode hit regions.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./internal/presentation -run 'Composer.*Mode|ThreeRowComposer' -v
```

**Step 3: Implement render-ready composer metadata**

Keep queries and policy out of rendering. Multiline input grows between fixed padding rows, with
mode right-aligned on the bottom row.

**Step 4: Verify**

```bash
cd tui
go test -race ./internal/presentation -run 'Composer|Surface|Resize' -v
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C tui add internal/presentation/surface.go internal/presentation/surface_test.go internal/presentation/interaction.go internal/presentation/interaction_test.go
git -C tui commit -m "feat(tui): show mode in the composer"
```

### Task 7: Build the wrapped rig footer with local focus

**Files:**

- Modify: `tui/internal/presentation/loopbar.go`
- Test: `tui/internal/presentation/loopbar_test.go`
- Modify: `tui/internal/presentation/surface.go`
- Test: `tui/internal/presentation/surface_test.go`
- Modify: `tui/internal/presentation/screen.go`
- Test: `tui/internal/presentation/screen_test.go`

**Step 1: Write failing footer tests**

Cover rig name, access, root elision, multiple primers, relevant delegates, focused-idle delegate
retention, stable creation order, wrapping, and hit testing. Assert a click changes local
`focusedLoopID` and never calls an active-loop setter.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./internal/presentation -run 'LoopFooter|FooterFocus' -v
```

**Step 3: Implement one render/hit-test layout**

Return physical rows and hit regions together. Use `●` for local focus and `○` otherwise. Wrap only
at segment boundaries. Submission continues through `SubmitToLoop(focusedLoopID)`.

**Step 4: Verify**

```bash
cd tui
go test -race ./internal/presentation -run 'LoopFooter|FooterFocus|Surface|Mouse' -v
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C tui add internal/presentation/loopbar.go internal/presentation/loopbar_test.go internal/presentation/surface.go internal/presentation/surface_test.go internal/presentation/screen.go internal/presentation/screen_test.go
git -C tui commit -m "feat(tui): add wrapped rig and loop footer"
```

### Phase 1 checkpoint

```bash
cd harness
go test -race ./...
cd ../tui
go test -race ./...
cd ../coderig
go test -race ./...
```

Expected: all modules pass before mutations or session navigation begin.

## Phase 2: controls and navigation

### Task 8: Generalize completion trays for runtime values

**Files:**

- Modify: `tui/components/completiontray.go`
- Modify: `tui/components/slashcomplete.go`
- Test: `tui/components/slashcomplete_test.go`
- Create: `tui/components/valuecomplete.go`
- Create: `tui/components/valuecomplete_test.go`
- Modify: `tui/internal/presentation/interaction.go`
- Test: `tui/internal/presentation/interaction_test.go`

**Step 1: Write failing component tests**

Cover injected command catalogs, aliases, zero results, typed value payloads, filtering, Tab,
Enter, Escape, mouse selection, windowing, and arbitrary widths. Static slash behavior must remain
unchanged without dynamic commands.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./components ./internal/presentation -run 'DynamicSlash|ValueComplete' -v
```

**Step 3: Implement immutable command/value injection**

Components render and navigate; presentation owns meaning and async effects. Return the selected
opaque ID instead of re-parsing labels.

**Step 4: Verify**

```bash
cd tui
go test -race ./components ./internal/presentation -run 'Slash|Value|Interaction' -v
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C tui add components/completiontray.go components/slashcomplete.go components/slashcomplete_test.go components/valuecomplete.go components/valuecomplete_test.go internal/presentation/interaction.go internal/presentation/interaction_test.go
git -C tui commit -m "feat(tui): add dynamic runtime value trays"
```

### Task 9: Dispatch runtime queries and mutations

**Files:**

- Modify: `tui/internal/presentation/action.go`
- Modify: `tui/internal/presentation/commands.go`
- Test: `tui/internal/presentation/commands_test.go`
- Modify: `tui/internal/presentation/sessioncore.go`
- Test: `tui/internal/presentation/sessioncore_test.go`
- Modify: `tui/internal/presentation/screen.go`
- Test: `tui/internal/presentation/screen_test.go`

**Step 1: Write failing dispatch tests**

Cover no catalog, discovery-only, zero/one/multiple choices, captured-loop targeting, session-wide
access, stale choices, focus changes, click-preserved drafts, and typed errors.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./internal/presentation -run 'RuntimeCommand|RuntimeClick|StaleChoice' -v
```

**Step 3: Implement bounded async commands**

Query on tray open, capture loop ID, return typed Bubble Tea messages, and wait for events before
changing current display. Register `/access`, not `/env`.

**Step 4: Verify**

```bash
cd tui
go test -race ./internal/presentation -run 'RuntimeCommand|RuntimeClick|StaleChoice|Draft' -v
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C tui add internal/presentation/action.go internal/presentation/commands.go internal/presentation/commands_test.go internal/presentation/sessioncore.go internal/presentation/sessioncore_test.go internal/presentation/screen.go internal/presentation/screen_test.go
git -C tui commit -m "feat(tui): control optional loop runtime"
```

### Task 10: Implement CodeRig runtime catalogs and controllers

**Files:**

- Create: `coderig/runtime_controls.go`
- Create: `coderig/runtime_controls_test.go`
- Modify: `coderig/models.go`
- Test: `coderig/models_test.go`
- Modify: `coderig/persistence.go`
- Modify: `coderig/swarm.go`
- Test: `coderig/rig_restore_integration_test.go`

**Step 1: Write failing catalog tests**

Cover model order/deduplication/filtering, effort by capability/API format, modes through
`loop.ModeCatalog`, defensive secret-free values, and every access cap.

**Step 2: Write failing controller tests**

Cover typed ID resolution, stale/unknown IDs, loop exit, model descriptor lookup, `SetMode`,
`loop.ChangeModel`, `loop.ChangeEffort`, and session `SetSecurityLimit`.

**Step 3: Verify failure**

```bash
cd coderig
go test -race ./... -run 'RuntimeCatalog|RuntimeMutation|AccessOptions' -v
```

**Step 4: Implement and verify**

Keep provider and policy knowledge in CodeRig. Map ordinals to `Untrusted`, `Read Only`,
`Writable`, `Trusted`, and `Unconfined`. Do not use the existing `runtime_context.go` filename.

```bash
cd coderig
go test -race ./... -run 'RuntimeCatalog|RuntimeMutation|AccessOptions|Restore' -v
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C coderig add runtime_controls.go runtime_controls_test.go models.go models_test.go persistence.go swarm.go rig_restore_integration_test.go
git -C coderig commit -m "feat(coderig): expose runtime controls to TUI"
```

### Task 11: Add the optional session browser and clean two-line tray

**Files:**

- Create: `tui/internal/presentation/sessionbrowser.go`
- Modify: `tui/api.go`
- Create: `tui/components/sessioncomplete.go`
- Create: `tui/components/sessioncomplete_test.go`
- Modify: `tui/internal/presentation/screen.go`
- Test: `tui/internal/presentation/screen_test.go`
- Test: `tui/internal/presentation/surface_test.go`

**Step 1: Write failing capability tests**

Existing `tui.New` callers remain source-compatible without a browser. With one, `/sessions` is
visible and `/resume` is an exact synonym. Exclude the current session.

**Step 2: Write failing tray tests**

For every record assert:

- two content rows;
- one blank padding row between records;
- one uninterrupted accent rail across content and padding;
- no boxes or corner glyphs;
- title/state/relative activity on row one;
- agent kind/loop count/created date/short ID on row two;
- selected background on both content rows but not padding;
- record-based keyboard and mouse selection;
- deterministic metadata dropping without implicit wrap.

**Step 3: Verify failure**

```bash
cd tui
go test -race ./components ./internal/presentation -run 'SessionBrowser|SessionTray' -v
```

**Step 4: Implement the process-scoped option**

```go
type SessionBrowser interface {
    ListSessions(context.Context) ([]SessionSummary, error)
    ResumeSession(context.Context, SessionID) (Agent, error)
}
```

Supply it through a backwards-compatible TUI option, never through `Agent`. Filter title, full or
short ID, state, and rig name. Show `No previous sessions` for empty results and a non-fatal notice
for list failures.

**Step 5: Verify and commit when authorized**

```bash
cd tui
go test -race ./components ./internal/presentation -run 'SessionBrowser|SessionTray|Surface' -v
git -C tui add api.go components/sessioncomplete.go components/sessioncomplete_test.go internal/presentation/sessionbrowser.go internal/presentation/screen.go internal/presentation/screen_test.go internal/presentation/surface_test.go
git -C tui commit -m "feat(tui): add previous session tray"
```

### Task 12: Implement interrupt-then-resume handoff

**Files:**

- Modify: `tui/internal/presentation/handoff.go`
- Test: `tui/internal/presentation/handoff_test.go`
- Modify: `tui/internal/presentation/commands.go`
- Test: `tui/internal/presentation/commands_test.go`
- Modify: `tui/internal/presentation/sessioncore.go`
- Test: `tui/internal/presentation/sessioncore_test.go`
- Modify: `tui/internal/presentation/screen.go`
- Test: `tui/internal/presentation/screen_test.go`
- Modify: `tui/internal/presentation/statusline.go`

**Step 1: Write state-machine tests**

Cover idle immediate handoff; running, compacting, and gate-blocked interrupt; wait for the
authoritative terminal event; interrupt refusal; duplicate Enter; close failure; resume failure;
quit during handoff; and stale generation results.

**Step 2: Verify failure**

```bash
cd tui
go test -race ./internal/presentation -run 'SessionResume|ResumeHandoff' -v
```

**Step 3: Generalize the existing handoff coordinator**

Reuse `/clear` close-before-open ownership. Track the selected session and show `interrupting…`,
then `resuming…`. Never mutate Harness active loop.

**Step 4: Verify**

```bash
cd tui
go test -race ./internal/presentation ./runtime -run 'SessionResume|ResumeHandoff|Finalize|Teardown' -v
```

Expected: PASS without double close or leaked replacements.

**Step 5: Commit when authorized**

```bash
git -C tui add internal/presentation/handoff.go internal/presentation/handoff_test.go internal/presentation/commands.go internal/presentation/commands_test.go internal/presentation/sessioncore.go internal/presentation/sessioncore_test.go internal/presentation/screen.go internal/presentation/screen_test.go internal/presentation/statusline.go
git -C tui commit -m "feat(tui): resume sessions through safe handoff"
```

### Task 13: Wire CodeRig session browsing

**Files:**

- Create: `coderig/session_browser.go`
- Create: `coderig/session_browser_test.go`
- Modify: `coderig/persistence.go`
- Test: `coderig/persistence_test.go`
- Modify: `coderig/cmd/coderig/main.go`
- Test: `coderig/cmd/coderig/main_test.go`

**Step 1: Write failing mapping and wiring tests**

Cover recent-first order, current exclusion, title fallback, state, short ID, agent kind, loop
count, timestamps, list failure, unavailable session, and config mismatch. Prove `/clear` opens new
while `/sessions` resumes the selected ID.

**Step 2: Verify failure**

```bash
cd coderig
go test -race ./... -run 'SessionBrowser|ResumeFromTUI' -v
```

**Step 3: Implement over `SessionStoreFactory`**

Map `sessionstore.SessionMeta` without replay. Capture CodeRig config in the resume opener but never
silently set `AllowConfigMismatch`. Keep the shared store open across agent handoffs.

**Step 4: Verify**

```bash
cd coderig
go test -race ./... -run 'SessionBrowser|ResumeFromTUI|Persistence|Command' -v
```

Expected: PASS.

**Step 5: Commit when authorized**

```bash
git -C coderig add session_browser.go session_browser_test.go persistence.go persistence_test.go cmd/coderig/main.go cmd/coderig/main_test.go
git -C coderig commit -m "feat(coderig): browse and resume stored sessions"
```

### Task 14: Final verification and dependency delivery

**Step 1: Format touched Go code**

```bash
gofmt -w harness/pkg/loop harness/internal/sessionruntime
gofmt -w tui/api.go tui/components tui/internal/presentation tui/sessionadapter
gofmt -w coderig
```

Expected: a second run produces no diff.

**Step 2: Run all race suites**

```bash
cd harness
go test -race ./...
cd ../tui
go test -race ./...
cd ../coderig
go test -race ./...
```

Expected: PASS in all three modules.

**Step 3: Run repository policy checks**

```bash
make -C harness fmt lint test security
make -C tui fmt lint test security
make -C coderig fmt lint test security
```

If a repository exposes different targets, use the equivalent targets from its current
`Makefile` rather than inventing one.

**Step 4: Perform manual TUI smoke checks**

Verify fresh and restored bootstrap, local footer focus, event-confirmed runtime changes, the
unboxed two-line session tray, `/resume` aliasing, immediate idle resume, interrupt-then-resume for
running/gated work, and narrow-width render/hit-test alignment.

**Step 5: Publish only when authorized**

Release in dependency order: Harness, TUI against the Harness release, then CodeRig against both.
Re-vendor with the module toolchain; never edit vendor files by hand. Repeat race and policy suites
against the released versions before tagging CodeRig.

**Step 6: Verify intended Git state**

```bash
git -C harness status --short
git -C tui status --short
git -C coderig status --short
```

Expected: only intended changes before authorized commits, then clean worktrees after delivery.
