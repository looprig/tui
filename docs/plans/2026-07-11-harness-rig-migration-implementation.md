# Harness Rig CLI Migration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Update CLI's narrow TUI adapter and active-loop presentation semantics for harness rig sessions, then pin and vendor the first harness release containing the final rig APIs.

**Architecture:** Keep CLI presentation-only. Split its obsolete primary-loop adapter into stable-root and current-active queries, fold `ActiveLoopChanged` into session UI state over the existing all-loop subscription, and leave lifecycle/workspace/mode authority in the embedding application.

**Tech Stack:** Go 1.26.4, `github.com/looprig/harness` v0.10.0, Bubble Tea v2, table-driven tests, Go race detector.

---

### Task 1: Rename the public loop-selection seam

**Files:**
- Modify: `tui/agent.go:20-127`
- Modify: `tui/commands.go:37-56`
- Modify: `tui/sessioncore.go:46-76`
- Modify: `tui/screen.go:152-207,562-575`
- Modify: `tui/agent_test.go:11-130`
- Modify: `tui/commands_test.go:88-130`
- Modify: `tui/sessioncore_test.go:15-25`
- Modify: `tui/screen_test.go` (Agent identity calls; current anchors `456-477,1278-1310`)
- Modify: `tui/fixtures_test.go:26-165`
- Modify: `cli/run_test.go:23-52`

**Step 1: Write the failing contract tests**

Replace the primary-only filter assertions in `tui/agent_test.go` with a table proving the sole production filter admits ephemeral and enduring events from any loop. Update the fake Agent compile contract to require separate root and active queries:

```go
func (f *fakeAgent) RootLoopID() uuid.UUID   { return f.rootLoopID }
func (f *fakeAgent) ActiveLoopID() uuid.UUID { return f.activeLoopID }

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
		{name: "session selection", ev: event.ActiveLoopChanged{Header: event.Header{Coordinates: identity.Coordinates{SessionID: sessionID}}, ActiveLoopID: other}, want: true},
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

Keep the repository's mandatory table-driven form and use the existing event-header helpers rather than introducing duplicate fixtures.

**Step 2: Run the focused tests to verify RED**

Run:

```bash
GOWORK=off go test -race ./tui ./cli -run 'TestAllLoopsEventFilter|TestSubscribe'
```

Expected: FAIL to compile because `fakeAgent.RootLoopID`/`ActiveLoopID`, renamed fixture fields, or the new filter expectations do not match the old `PrimaryLoopID` contract.

**Step 3: Make the minimal public-seam change**

In `tui/agent.go`, rename:

```go
PrimaryLoopID() uuid.UUID
```

to:

```go
// RootLoopID returns the stable loop used for transcript attribution.
RootLoopID() uuid.UUID

// ActiveLoopID returns the session's current default input target.
ActiveLoopID() uuid.UUID
```

Keep `Submit`, `SubmitToLoop`, `Subscribe`, `ReplayBacklog`, gate verbs, `Interrupt`, `Close`, and `AcceptsImages` unchanged; they are CLI-owned adapter methods.

Delete the dead primary-only `DefaultEventFilter`, `subscribeCmd`, and
`defaultLoopFilter` seams. Keep `AllLoopsEventFilter` and make
`sessionCore.subscribe`/`subscribeWith` the only production subscription path. Update
fake fields/methods in `tui/fixtures_test.go` and `cli/run_test.go` from the conflated
`primaryLoopID`/`PrimaryLoopID` to `rootLoopID`/`RootLoopID` plus
`activeLoopID`/`ActiveLoopID` (default active to root in test constructors when a test
does not need them to differ).

As a compile-preserving intermediate state, mechanically replace every remaining
`agent.PrimaryLoopID()` call with `agent.RootLoopID()`. Task 2 then changes only the
places that semantically follow current selection (turn status and initial focus) to
`activeLoopID`. Verify the mechanical rename before GREEN:

```bash
! rg -n 'PrimaryLoopID' tui cli --glob '*.go'
```

**Step 4: Run the focused tests to verify GREEN**

Run:

```bash
GOWORK=off go test -race ./tui ./cli -run 'TestAllLoopsEventFilter|TestSubscribe'
```

Expected: PASS.

**Step 5: Commit**

```bash
git add tui cli
git commit -m "refactor(tui): separate root and active loop adapters"
```

### Task 2: Refresh harness and track the durable active loop

**Files:**
- Modify: `tui/sessioncore.go:34-170,231-266`
- Modify: `tui/sessioncore_test.go:15-200`
- Modify: `tui/transcript.go:368-378,607-610,780-805`
- Modify: `tui/transcript_test.go:1391-3571` (root-attribution fixtures/assertions)
- Modify: `tui/displayprojection_test.go:75-93`
- Modify: `go.mod:16,101`
- Modify: `go.sum`
- Modify: `vendor/modules.txt`
- Replace: `vendor/github.com/looprig/harness/**` (only imported harness packages are emitted)

**Step 1: Write the failing state-transition tests**

Add a table-driven `TestSessionCoreActiveLoopChanged` covering:

```go
root, builder, sessionID := callID(0xA1), callID(0xB1), callID(0x91)
tests := []struct {
	name       string
	events     []event.Event
	wantActive uuid.UUID
	wantRoot   uuid.UUID
}{
	{name: "initial", wantActive: root, wantRoot: root},
	{name: "selection changes active only", events: []event.Event{
		event.ActiveLoopChanged{Header: event.Header{Coordinates: identity.Coordinates{SessionID: sessionID}}, PreviousLoopID: root, ActiveLoopID: builder},
	}, wantActive: builder, wantRoot: root},
	{name: "repeated selection is stable", events: []event.Event{
		event.ActiveLoopChanged{Header: event.Header{Coordinates: identity.Coordinates{SessionID: sessionID}}, ActiveLoopID: builder},
		event.ActiveLoopChanged{Header: event.Header{Coordinates: identity.Coordinates{SessionID: sessionID}}, PreviousLoopID: builder, ActiveLoopID: builder},
	}, wantActive: builder, wantRoot: root},
}
```

Also add table rows proving `applyTurnStatus` reacts to `TurnStarted`/terminal events from the current active loop and ignores another loop after a selection change. Add `/clear` coverage proving both fields reset from the replacement Agent.

**Step 2: Run the focused tests to verify RED**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestSessionCoreActiveLoopChanged|TestSessionCoreApplyTurnStatus|TestSessionCoreReopenOrdering'
```

Expected: FAIL because the stale vendor has no `event.ActiveLoopChanged`, and the CLI
state fields/selection fold do not exist.

**Step 3: Pin and vendor the rig-enabled harness release**

Precondition: `v0.10.0` must contain the reviewed rig branch. CLI's vendor tree is
package-pruned, so it will include `pkg/event`, `pkg/gate`, `pkg/identity`, and
`pkg/tool` but not unimported `pkg/rig`, `pkg/session`, or `pkg/serve`.

Run:

```bash
rg -n '^# github.com/looprig/harness v0\.5\.2 => ../harness$' vendor/modules.txt
! rg -n 'type ActiveLoopChanged' vendor/github.com/looprig/harness/pkg/event --glob '*.go'
GOWORK=off go mod edit -require=github.com/looprig/harness@v0.10.0
GOWORK=off go mod tidy
GOWORK=off go mod vendor
rg -n '^# github.com/looprig/harness v0\.10\.0 => ../harness$' vendor/modules.txt
rg -n 'type ActiveLoopChanged' vendor/github.com/looprig/harness/pkg/event --glob '*.go'
```

Expected: the first two checks prove the old vendor state; after refresh,
`modules.txt` records `v0.10.0` and the imported event package contains
`ActiveLoopChanged`. Keep the existing `replace => ../harness` unchanged.

Commit the dependency separately:

```bash
git add go.mod go.sum vendor
git commit -m "build: update harness for rig sessions"
```

**Step 4: Implement the two-field model**

Add current selection to `sessionCore` and initialize it once per opened Agent:

```go
type sessionCore struct {
	agent        Agent
	activeLoopID uuid.UUID
	// existing fields...
}

func newSessionCore(/* existing args */) sessionCore {
	root := agent.RootLoopID()
	active := agent.ActiveLoopID()
	return sessionCore{
		agent:        agent,
		activeLoopID: active,
		transcript:   transcriptModel{rootLoopID: root},
		// existing initialization...
	}
}
```

At the start of `handleEvent`, update `activeLoopID` when `ev` is `event.ActiveLoopChanged`, then apply the existing reducers. Compare turn status to `c.activeLoopID`, never a live adapter query; the durable event order is the authoritative transition.

Rename `transcriptModel.primaryLoopID` to `rootLoopID` and update only terminology/references, not fold behavior. The root is deliberately stable across `ActiveLoopChanged` so historical user/subagent attribution is not rewritten.

In `applyReopenResult`, after swapping Agents, read the replacement's `RootLoopID`
and `ActiveLoopID` independently before re-subscribing. Never derive a restored root
from the latest active selection.

**Step 5: Run focused and transcript tests to verify GREEN**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestSessionCoreActiveLoopChanged|TestSessionCoreApplyTurnStatus|TestSessionCoreReopenOrdering|TestTranscript|TestFoldDisplay'
```

Expected: PASS; root-attribution behavior remains byte-for-byte equivalent except for field/method names.

**Step 6: Commit**

```bash
git add tui/sessioncore.go tui/sessioncore_test.go tui/transcript.go tui/transcript_test.go tui/displayprojection_test.go
git commit -m "feat(tui): follow durable active loop selection"
```

### Task 3: Make the loop bar retain the active selection

**Files:**
- Modify: `tui/loopbar.go:14-202`
- Modify: `tui/loopbar_test.go:1-320`
- Modify: `tui/screen.go:51-61,152-161,196-207,400-420,562-575,1041-1080`
- Modify: `tui/screen_test.go:456-477,495-625,1270-1310`
- Modify: `tui/restore.go:26-36,163-183`
- Modify: `tui/restore_test.go:47-100`

**Step 1: Write the failing bar and screen tests**

Add table rows where the visible cap is smaller than the number of loops and assert:

```go
tests := []struct {
	name    string
	focused uuid.UUID
	active  uuid.UUID
	root    uuid.UUID
	wantIDs []uuid.UUID
}{
	{name: "active and root retained when idle and unfocused", focused: reviewer, active: builder, root: planner, wantIDs: []uuid.UUID{planner, builder, reviewer}},
	{name: "focus active and root may coincide", focused: builder, active: builder, root: builder, wantIDs: []uuid.UUID{builder}},
}
```

Add a screen test that delivers `ActiveLoopChanged`, then proves `bar().active` and retained entries use the new ID without changing `focusedLoopID` or `transcript.rootLoopID`.

**Step 2: Run focused tests to verify RED**

Run:

```bash
GOWORK=off go test -race ./tui -run 'TestLoopBar.*Active|TestModern.*ActiveLoopChanged|TestModernReopen'
```

Expected: FAIL because `loopBar` still has a `primary` field and the screen does not thread current active selection into it.

**Step 3: Implement active retention without focus stealing**

Rename `loopBar.primary` to `loopBar.root`, add `loopBar.active`, and update
priority bands to focused → active → root → live → idle. Change `Screen.bar` and
`activeBarEntries` to pass both `m.activeLoopID` and `m.transcript.rootLoopID`.

Do not assign `focusedLoopID` in response to `ActiveLoopChanged`. The composer continues to dispatch with:

```go
submitToLoop(m.focusedLoopID, action.Text)
```

Rename restore parameters/comments from primary to root. `restoreBacklogCmd` receives `m.transcript.rootLoopID`; selection events in the backlog are folded for transcript display but do not change the root used for attribution. The live `sessionCore` owns current active selection.

**Step 4: Run the TUI package to verify GREEN**

Run:

```bash
GOWORK=off go test -race ./tui
```

Expected: PASS.

**Step 5: Commit**

```bash
git add tui/loopbar.go tui/loopbar_test.go tui/screen.go tui/screen_test.go tui/restore.go tui/restore_test.go
git commit -m "feat(tui): retain active loop without stealing focus"
```

### Task 4: Audit the package-pruned vendor closure

**Files:**
- Modify: only dependency files if the reproducibility check finds drift

**Step 1: Verify the declared and imported harness surface**

Run:

```bash
rg -n 'github.com/looprig/harness v0\.10\.0' go.mod vendor/modules.txt
rg -n 'type ActiveLoopChanged' vendor/github.com/looprig/harness/pkg/event --glob '*.go'
test ! -d vendor/github.com/looprig/harness/pkg/rig
test ! -d vendor/github.com/looprig/harness/pkg/session
```

Expected: version and event checks pass. The two negative directory checks also pass:
Go vendor intentionally omits unimported lifecycle packages, so their absence is not a
stale-vendor signal.

**Step 2: Reproduce the dependency files**

Run:

```bash
GOWORK=off go mod tidy
GOWORK=off go mod vendor
git diff --exit-code -- go.mod go.sum vendor
```

Expected: PASS with no diff. If the release tag resolves to different content, stop and
fix the version/source mismatch; do not accept an unexplained vendor rewrite.

**Step 3: Run the dependency-sensitive build**

Run:

```bash
GOWORK=off go test -race ./tui ./cli
CGO_ENABLED=0 GOWORK=off go build -trimpath ./...
```

Expected: PASS against the refreshed vendor tree. No commit is created when the audit
is clean.

### Task 5: Remove stale terminology and run all CLI gates

**Files:**
- Modify: only files required by failures or stale comments found below

**Step 1: Search production and tests for removed vocabulary/surfaces**

Run:

```bash
! rg -n 'PrimaryLoopID|primaryLoopID|DefaultEventFilter|session\.Compile|session\.Runner|session\.New|session\.Restore|serve\.Runner|CheckpointWorkspace|WithWorkspace|checkpoint watcher' . --glob '*.go' --glob '!vendor/**'
! rg -n 'session\.Compile|session\.Runner|WithCompile|serve\.Runner' vendor/github.com/looprig/harness --glob '*.go'
```

Expected: no output. Rename stale comments/helpers to “root” or “active” according to the design; do not mechanically call the stable transcript root active.

**Step 2: Run formatting and security gates**

Run:

```bash
GOWORK=off make fmt
GOWORK=off make secure
```

Expected: PASS and no formatting diff after the gate.

**Step 3: Run all unit and integration tests**

Run:

```bash
GOWORK=off go test -race ./...
GOWORK=off go test -tags integration -race ./...
CGO_ENABLED=0 GOWORK=off go build -trimpath ./...
```

Expected: PASS.

If a downstream adapter composition test would force CLI to import SWE or another application module, place that test in the sibling `tests` integration repository instead. Do not reverse CLI's dependency direction to make the test convenient.

**Step 4: Review the final diff**

Run:

```bash
git diff --check
git status --short
git log --oneline -5
```

Expected: `git diff --check` prints nothing; status contains only intentional migration changes, or is clean after the task commits.

**Step 5: Commit any final cleanup**

```bash
git add tui cli
git commit -m "chore: finish harness rig cli migration"
```

Skip this commit when Step 1 found no cleanup and the worktree is already clean.
