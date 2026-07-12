# Harness Rig CLI Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Update CLI's narrow TUI adapter, active-loop state, focus, and target-model capability handling for harness rig sessions without moving composition or control-plane authority into CLI.

**Architecture:** Refresh the package-pruned harness vendor tree first. Split stable root, mutable active, and focused loop state; subscribe before reading the authoritative active baseline; reconcile queued selection events causally; derive displayed active status from per-loop running state; and query image capability for the actual submission target.

**Tech Stack:** Go 1.26.4, the reviewed harness release containing rig plus loop display metadata, Bubble Tea v2, table-driven tests, Go race detector.

---

### Task 1: Pin and vendor the rig-enabled harness release

**Files:**
- Modify: `go.mod:16,101`
- Modify: `go.sum`
- Modify: `vendor/modules.txt`
- Replace: `vendor/github.com/looprig/harness/**` (only imported packages are emitted)

**Precondition:** select the published harness tag containing the reviewed rig branch plus
`LoopStarted` display metadata (`loop.WithDisplayName`/`WithDescription`). Substitute it for
`<harness-tag>` below. If it is not published, stop; do not invent a release.

**Step 1: Prove the imported event surface is stale**

Run:

```bash
rg -n '^# github.com/looprig/harness v0\.5\.2 => ../harness$' vendor/modules.txt
! rg -n 'type ActiveLoopChanged' vendor/github.com/looprig/harness/pkg/event --glob '*.go'
```

Expected: both commands succeed: `modules.txt` records `v0.5.2`, and the imported event package lacks `ActiveLoopChanged`. This is the RED dependency check; no Go test references the missing type yet.

**Step 2: Update the declared release**

Run:

```bash
GOWORK=off go mod edit -require=github.com/looprig/harness@<harness-tag>
GOWORK=off go mod tidy
```

Expected: `go.mod` requires the recorded reviewed tag; the existing relative replace remains unchanged.

**Step 3: Refresh package-pruned vendor**

Run:

```bash
GOWORK=off go mod vendor
rg -n '^# github.com/looprig/harness <harness-tag> => ../harness$' vendor/modules.txt
rg -n 'type ActiveLoopChanged' vendor/github.com/looprig/harness/pkg/event --glob '*.go'
rg -n 'DisplayName' vendor/github.com/looprig/harness/pkg/event --glob '*.go'
test ! -d vendor/github.com/looprig/harness/pkg/rig
test ! -d vendor/github.com/looprig/harness/pkg/session
```

Expected: version and event checks pass. The negative directory checks also pass because Go vendor deliberately omits unimported `pkg/rig`, `pkg/session`, and `pkg/serve`; their absence is not a stale-vendor signal.

**Step 4: Verify the existing CLI before migration**

Run:

```bash
GOWORK=off go test -race ./tui ./cli
CGO_ENABLED=0 GOWORK=off go build -trimpath ./...
```

Expected: PASS against the refreshed imported harness surface.

**Step 5: Commit**

```bash
git add go.mod go.sum vendor
git commit -m "build: update harness for rig sessions"
```

### Task 2: Split the CLI adapter's root and active queries

**Files:**
- Modify: `tui/agent.go:20-127`
- Modify: `tui/commands.go:36-58`
- Modify: `tui/sessioncore.go:46-101`
- Modify: `tui/agent_test.go:11-130`
- Modify: `tui/commands_test.go:88-130`
- Modify: `tui/sessioncore_test.go:15-25`
- Modify: `tui/fixtures_test.go:26-165`
- Modify: `tui/screen.go:152-207,562-575`
- Modify: `tui/screen_test.go:456-477,1278-1310`
- Modify: `cli/run_test.go:23-52`

**Step 1: Write failing adapter/filter tests**

Replace primary-only filter tests with the repository's all-loop delivery contract:

```go
func TestAllLoopsEventFilter(t *testing.T) {
	t.Parallel()
	active, other, sessionID := loopID(1), loopID(2), loopID(9)
	filter := AllLoopsEventFilter()
	tests := []struct {
		name string
		ev   event.Event
		want bool
	}{
		{name: "active ephemeral", ev: event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: active}}}, want: true},
		{name: "other ephemeral", ev: event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: other}}}, want: true},
		{name: "other enduring", ev: event.StepDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: other}}}, want: true},
		{name: "selection", ev: event.ActiveLoopChanged{Header: event.Header{Coordinates: identity.Coordinates{SessionID: sessionID}}, ActiveLoopID: other}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := event.ShouldDeliver(filter, tt.ev); got != tt.want {
				t.Errorf("ShouldDeliver(%T) = %v, want %v", tt.ev, got, tt.want)
			}
		})
	}
}
```

Change fake Agent contracts to require:

```go
RootLoopID() uuid.UUID
ActiveLoopID() uuid.UUID
```

**Step 2: Run tests to verify RED**

Run:

```bash
GOWORK=off go test -race ./tui ./cli -run 'TestAllLoopsEventFilter|TestSubscribe'
```

Expected: FAIL to compile because `Agent` still exposes `PrimaryLoopID` rather than separate root and active queries.

**Step 3: Change the narrow adapter contract**

In `tui/agent.go`, replace `PrimaryLoopID` with:

```go
// RootLoopID returns the stable root used for transcript attribution.
RootLoopID() uuid.UUID

// ActiveLoopID returns the current default input target.
ActiveLoopID() uuid.UUID

```

Keep `Submit`, `SubmitToLoop`, `Subscribe`, `ReplayBacklog`, gate verbs, `Interrupt`, and `Close` as CLI-owned adapter methods. Delete dead `DefaultEventFilter`, `subscribeCmd`, and `defaultLoopFilter`; retain only `AllLoopsEventFilter`, `sessionCore.subscribe`, and `subscribeWith`.

As a compile-preserving intermediate step, replace every remaining `agent.PrimaryLoopID()` call with `agent.RootLoopID()`. Tasks 3-4 then move current-selection behavior to `activeLoopID`/`ActiveLoopID`. Give fake Agents separate `rootLoopID` and `activeLoopID`; default active to root in common test constructors. Keep the existing zero-argument image capability until Task 5 changes its contract and both callers atomically.

**Step 4: Verify GREEN and removal**

Run:

```bash
! rg -n 'PrimaryLoopID|DefaultEventFilter' tui cli --glob '*.go'
GOWORK=off go test -race ./tui ./cli -run 'TestAllLoopsEventFilter|TestSubscribe'
```

Expected: no removal-search output; tests PASS.

**Step 5: Commit**

```bash
git add tui cli
git commit -m "refactor(tui): split root and active loop queries"
```

### Task 3: Handshake active selection and fold per-loop running state

**Files:**
- Modify: `tui/sessioncore.go:34-187,231-266`
- Modify: `tui/sessioncore_test.go:27-200`
- Modify: `tui/fixtures_test.go:26-165` (subscription callback/query sequencing seams)

**Step 1: Write failing subscription/baseline tests**

Extend the fake Agent so tests can enqueue events during `Subscribe` and choose the value returned by `ActiveLoopID`. Add table-driven cases:

```go
tests := []struct {
	name           string
	baseline       uuid.UUID
	queued         []event.ActiveLoopChanged
	wantActive     uuid.UUID
	wantStatus     Status
}{
	{name: "transition after baseline", baseline: planner, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}}, wantActive: builder},
	{name: "baseline already includes transition", baseline: builder, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}}, wantActive: builder},
	{name: "stale event cannot regress baseline", baseline: reviewer, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}}, wantActive: reviewer},
	{name: "queued chain catches up", baseline: planner, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}, {PreviousLoopID: builder, ActiveLoopID: reviewer}}, wantActive: reviewer},
}
```

Stamp every event with a valid session header. Drive the real order: call the subscription command, apply `subscribedMsg` (which reads the baseline only after subscription exists), then drain queued deliveries through `handleEvent`.

Add a successful-reopen setup-window case: the replacement subscription queues a selection before `applySubscribed`; the baseline/event reconciliation chooses the causally newest active ID.

**Step 2: Write failing per-loop running tests**

Add table-driven event sequences proving:

- active planner `TurnStarted`, then planner→idle-builder selection yields `StatusIdle`;
- builder is already running, then idle planner→builder selection yields `StatusRunning`;
- after planner→running-builder selection, a late planner terminal leaves `StatusRunning`;
- the active loop's terminal yields `StatusIdle`.

Assert the internal running bit for every involved loop, not only the displayed status.

**Step 3: Run focused tests to verify RED**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestSessionCoreSubscribeBaseline|TestSessionCoreReopenSetupWindow|TestSessionCoreActiveRunningStatus'
```

Expected: FAIL because `applySubscribed` does not establish a post-subscription baseline, selection events are not reconciled, and status has no per-loop running map.

**Step 4: Implement the handshake and causal fold**

Add `activeLoopID uuid.UUID` and `loopRunning map[uuid.UUID]bool` to `sessionCore`. Do not establish the authoritative active baseline in `newSessionCore`. On successful `applySubscribed`:

```go
c.sub = msg.sub
c.activeLoopID = c.agent.ActiveLoopID() // subscription is already live
c.deriveActiveStatus()
return subNext(c.sub), false
```

Reconcile each `ActiveLoopChanged` against current `c`:

```go
if c.activeLoopID.IsZero() {
	return // no authoritative baseline: fail closed
}
switch c.activeLoopID {
case ev.PreviousLoopID:
	c.activeLoopID = ev.ActiveLoopID
case ev.ActiveLoopID:
	// Baseline already includes this transition.
default:
	// Stale relative to the post-subscription baseline; do not regress.
}
```

For every loop, `TurnStarted` sets `loopRunning[id] = true`; `TurnDone`, `TurnFailed`, and `TurnInterrupted` set it false. After a turn terminal/start or accepted selection, derive ordinary displayed status from `loopRunning[activeLoopID]`. Preserve explicit `StatusResetting` until reopen completes; keep existing interrupt command semantics and let terminal/idle handling resolve it as currently tested.

On successful `applyReopenResult`, close the old subscription, swap Agent, reset root transcript/prompts/running/active state, and return the fresh subscribe command. Do not read the authoritative active baseline until fresh `applySubscribed`.

**Step 5: Run focused tests to verify GREEN**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestSessionCoreSubscribeBaseline|TestSessionCoreReopenSetupWindow|TestSessionCoreActiveRunningStatus|TestSessionCoreReopenOrdering'
```

Expected: PASS, including setup-window races under `-race`.

**Step 6: Commit**

```bash
git add tui/sessioncore.go tui/sessioncore_test.go tui/fixtures_test.go
git commit -m "feat(tui): reconcile active loop after subscription"
```

### Task 4: Separate root attribution, active retention, and focus

**Files:**
- Modify: `tui/transcript.go:368-1600`
- Modify: `tui/transcript_test.go:1391-3571`
- Modify: `tui/displayprojection_test.go:75-93`
- Modify: `tui/loopbar.go:14-202`
- Modify: `tui/loopbar_test.go:1-320`
- Modify: `tui/screen.go:51-61,150-213,373-425,562-575,1041-1080`
- Modify: `tui/screen_test.go:456-625,1278-1310`
- Modify: `tui/restore.go:26-36,163-183`
- Modify: `tui/restore_test.go:47-100`

**Step 1: Write failing focus and bar tests**

Add tests proving:

- `LoopStarted.DisplayName` becomes the displayed/stored label;
- empty display metadata falls back to `Header.AgentName` for older journals;
- display metadata never changes loop ID, root, active, or focus identity;
- `New` initializes `focusedLoopID` from `Agent.ActiveLoopID`, even when root differs;
- successful `/clear` initializes focus from the replacement Agent's `ActiveLoopID`;
- a later `ActiveLoopChanged` updates current active/bar/status but leaves focus unchanged;
- under a visible cap, priority is focused → active → root → live → recent idle.

Use a table where root, active, and focused are all different, plus a row where they coincide.

**Step 2: Run focused tests to verify RED**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestModernNewFocusesActive|TestModernReopenFocusesActive|TestModernSelectionDoesNotStealFocus|TestLoopBar.*ActiveRoot'
```

Expected: FAIL because focus still initializes from the old root/primary concept and the bar has only one privileged loop field.

**Step 3: Implement stable root and dynamic active presentation**

Rename internal `transcriptModel.primaryLoopID` and associated terminology to `rootLoopID` without changing historical fold attribution. `FoldDisplay` and restore repaint receive `Agent.RootLoopID`; they never derive root from the latest active selection.

Initialize `Screen.focusedLoopID` from `Agent.ActiveLoopID` in `New`. On successful `/clear`, set focus from the replacement Agent's `ActiveLoopID` once; do not change it from later selection events.

Rename `loopBar.primary` to `root`, add `active`, and change selection/priority helpers to keep focused, active, and root independently. `Screen.bar` passes `m.focusedLoopID`, `m.activeLoopID`, and `m.transcript.rootLoopID`.

Keep `trackTurnClock` per-loop. Change any “current active turn” status lookup to the reconciled active/running state; root-only transcript notices remain explicitly root-scoped rather than being mislabeled active.

When folding `LoopStarted`, use `DisplayName` when non-empty and otherwise
`Header.AgentName`. Store that presentation label in transcript/loop-bar metadata; every state
map remains keyed by `LoopID`.

**Step 4: Run TUI tests to verify GREEN**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestModernNewFocusesActive|TestModernReopenFocusesActive|TestModernSelectionDoesNotStealFocus|TestLoopBar|TestTranscript|TestFoldDisplay|TestReplay'
```

Expected: PASS. Root attribution is unchanged; focus and active selection are independent.

**Step 5: Commit**

```bash
git add tui/transcript.go tui/transcript_test.go tui/displayprojection_test.go tui/loopbar.go tui/loopbar_test.go tui/screen.go tui/screen_test.go tui/restore.go tui/restore_test.go
git commit -m "feat(tui): separate root active and focused loops"
```

### Task 5: Query image capability for the submission target

**Files:**
- Modify: `tui/agent.go:20-45`
- Modify: `tui/sessioncore.go:311-346`
- Modify: `tui/sessioncore_test.go:226-330`
- Modify: `tui/fixtures_test.go:26-165`
- Modify: `cli/run_test.go:23-52`

**Step 1: Write failing heterogeneous-loop tests**

First change the fake Agent to implement
`AcceptsImages(loopID uuid.UUID) bool` over an
`acceptsImages map[uuid.UUID]bool`, making the production call sites fail to compile.
Then add table-driven cases:

- focused image-capable builder accepts `@image.png`;
- focused text-only planner rejects the same attachment;
- unknown loop fails closed;
- changing builder from image-capable to text-only between two submissions changes the second result without rebuilding Agent;
- the generic `submit` path queries reconciled `activeLoopID`, while `submitToLoop` queries its explicit target.

Use a temporary PNG fixture through existing attachment helpers; assert the fake records the queried loop ID and that rejected inputs never call Submit/SubmitToLoop.

**Step 2: Run focused tests to verify RED**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestSessionCore.*TargetImageCapability'
```

Expected: FAIL because `submit` and `submitToLoop` still call a zero-argument/session-wide capability.

**Step 3: Pass the target into the capability query**

Change `tui.Agent` from `AcceptsImages() bool` to:

```go
// AcceptsImages reports the current model capability for loopID.
AcceptsImages(loopID uuid.UUID) bool
```

Implement:

```go
func (c *sessionCore) submit(text string) (tea.Cmd, bool) {
	target := c.activeLoopID
	blocks, err := buildBlocks(text, c.agent.AcceptsImages(target))
	// existing error/submit behavior
}

func (c *sessionCore) submitToLoop(loopID uuid.UUID, text string) (tea.Cmd, bool) {
	blocks, err := buildBlocks(text, c.agent.AcceptsImages(loopID))
	// existing error/submit behavior
}
```

The embedding adapter implementation is:

```go
func (a *sessionAgent) AcceptsImages(id uuid.UUID) bool {
	h, ok := a.session.Loop(id)
	return ok && h.Model().Caps.AcceptsImages
}
```

That concrete adapter is not implemented in CLI. Its heterogeneous-loop and runtime-model-change acceptance belongs to the embedding migration or sibling `tests` repository; CLI unit tests verify the dynamic contract with fakes.

**Step 4: Run focused and package tests to verify GREEN**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestSessionCore.*TargetImageCapability|TestBuildBlocks'
GOWORK=off go test -race ./tui ./cli
```

Expected: PASS.

**Step 5: Commit**

```bash
git add tui/agent.go tui/sessioncore.go tui/sessioncore_test.go tui/fixtures_test.go cli/run_test.go
git commit -m "fix(tui): check image capability per target loop"
```

### Task 6: Run removal, dependency, and full verification gates

**Files:**
- Modify: only files required by failures or stale comments found below

**Step 1: Run architectural removal searches**

Run:

```bash
! rg -n 'PrimaryLoopID|primaryLoopID|DefaultEventFilter|session\.Compile|session\.Runner|session\.New|session\.Restore|serve\.Runner|CheckpointWorkspace|WithWorkspace|checkpoint watcher' . --glob '*.go' --glob '!vendor/**'
! rg -n 'AcceptsImages\(\)' tui cli --glob '*.go'
rg -n 'github.com/looprig/harness <harness-tag>' go.mod vendor/modules.txt
rg -n 'type ActiveLoopChanged' vendor/github.com/looprig/harness/pkg/event --glob '*.go'
```

Expected: negative searches print nothing; positive searches find the release and event type.

**Step 2: Reproduce dependency files**

Run:

```bash
GOWORK=off go mod tidy
GOWORK=off go mod vendor
git diff --exit-code -- go.mod go.sum vendor
```

Expected: PASS with no dependency diff.

**Step 3: Run formatting and security gates**

Run:

```bash
GOWORK=off make fmt
GOWORK=off make secure
```

Expected: PASS with no formatting diff after the gate.

**Step 4: Run all tests and the static build**

Run:

```bash
GOWORK=off go test -race ./...
GOWORK=off go test -tags integration -race ./...
CGO_ENABLED=0 GOWORK=off go build -trimpath ./...
```

Expected: PASS.

Deferred acceptance criterion, owned by the embedding-adapter migration: its
fresh-session and switched-then-restored acceptance tests must prove `RootLoopID` uses
the durably first zero-parent `LoopStarted` while `ActiveLoopID` uses the restored
current selection. Those tests may live in the sibling `tests` repository, but creating
them is not an action or completion gate in this CLI plan.

**Step 5: Review and commit only necessary cleanup**

Run:

```bash
git diff --check
git status --short
git log --oneline -6
```

Expected: diff check prints nothing. If removal searches required comment/helper cleanup, commit only those files:

```bash
git add tui cli
git commit -m "chore: finish harness rig cli migration"
```

Skip the cleanup commit when the worktree is already clean.
