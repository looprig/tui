# Post-Compaction Context Status Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the TUI status line immediately display the harness-authoritative post-compaction context percentage.

**Architecture:** Keep all token counting in the inference/harness path. Extend the TUI's immutable per-loop runtime event projection to fold `CompactionCommitted.PostContext`, then let the existing status formatter render that updated measurement without new rendering logic.

**Tech Stack:** Go, harness public events, Bubble Tea presentation state, standard `testing` package.

---

### Task 1: Pin the post-compaction projection behavior

**Files:**
- Modify: `internal/presentation/runtimeprojection_test.go`

**Step 1: Write the failing test**

Add a focused test that seeds two loops with distinct `ContextMeasured` values, applies a
`CompactionCommitted` event to one loop, and asserts:

```go
state, _ := projection.loop(compactedLoopID)
if !state.hasContext || state.context != postContext {

other, _ := projection.loop(otherLoopID)
if !other.hasContext || other.context != otherContext {
```

**Step 2: Run the test to verify it fails**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test -race ./internal/presentation -run TestRuntimeProjectionUsesCommittedPostCompactionContext -count=1 -v
```

Expected: FAIL because the compacted loop still contains its earlier `ContextMeasured` value.

### Task 2: Fold the authoritative post-compaction measurement

**Files:**
- Modify: `internal/presentation/runtimeprojection.go`
- Test: `internal/presentation/runtimeprojection_test.go`

**Step 1: Write the minimal implementation**

Add this event arm to `runtimeProjection.ApplyEvent`:

```go
case event.CompactionCommitted:
	state.id = loopID
	state.context = value.PostContext
	state.hasContext = true
	changed = true
```

**Step 2: Run the focused projection test**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test -race ./internal/presentation -run TestRuntimeProjectionUsesCommittedPostCompactionContext -count=1 -v
```

Expected: PASS.

### Task 3: Pin the visible status-line correction

**Files:**
- Modify: `internal/presentation/screen_test.go`

**Step 1: Add a screen regression test**

Create a screen focused on one loop, deliver a pre-compaction `ContextMeasured` value of
80/100, verify the plain status line contains `~80% context`, then deliver
`CompactionCommitted{PostContext: ...20/100...}` and verify it contains `~20% context` and no
longer contains `~80% context`.

**Step 2: Run the regression test**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test -race ./internal/presentation -run TestStatusLineUsesCommittedPostCompactionContext -count=1 -v
```

Expected: PASS with the projection fix and FAIL without it.

### Task 4: Verify and commit the fix

**Files:**
- Modify: `internal/presentation/runtimeprojection.go`
- Modify: `internal/presentation/runtimeprojection_test.go`
- Modify: `internal/presentation/screen_test.go`

**Step 1: Format the changed Go files**

Run:

```bash
gofmt -w internal/presentation/runtimeprojection.go internal/presentation/runtimeprojection_test.go internal/presentation/screen_test.go
```

**Step 2: Run focused package verification**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test -race ./internal/presentation -count=1
```

Expected: PASS.

**Step 3: Run the complete TUI test suite**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test -race ./... -count=1
```

Expected: PASS.

**Step 4: Review and commit**

```bash
git diff --check
git diff -- internal/presentation/runtimeprojection.go internal/presentation/runtimeprojection_test.go internal/presentation/screen_test.go
git add internal/presentation/runtimeprojection.go internal/presentation/runtimeprojection_test.go internal/presentation/screen_test.go
git commit -m "fix(tui): refresh context after compaction"
```
