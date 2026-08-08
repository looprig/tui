# Unified Tray Selection Glow Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run the same selected-row animation when every completion tray opens and whenever its selection moves.

**Architecture:** Keep the shared renderer, colors, and scheduler. Centralize the active-tray predicate, use it in the animation tick guard, and detect synchronous slash/file tray opening after composer interaction; asynchronous runtime/session handlers retain their existing open-time trigger.

**Tech Stack:** Go, Bubble Tea v2, Lip Gloss v2, standard `testing` package.

---

### Task 1: Pin the cross-tray animation contract

**Files:**
- Test: `internal/presentation/screen_test.go:500-565`

**Step 1: Write the failing tests**

Expand the existing background-transition test with slash, file, runtime-value, and session tray
fixtures. Start the glow for each fixture and assert every tick advances through the shared color
frames. Add a focused case that types `/` into a closed composer and asserts the newly opened tray
starts at frame zero with a non-nil scheduled command.

**Step 2: Run the tests to verify they fail**

Run: `go test -race ./internal/presentation -run TestModernCompletionTraySelectionRunsBackgroundTransition -count=1`

Expected: runtime/session advancement and synchronous open-time ignition fail.

### Task 2: Unify the animation driver

**Files:**
- Modify: `internal/presentation/screen.go:834-851,1398-1408`

**Step 1: Write the minimal implementation**

Add `completionTrayOpen`, covering `sessionTray`, `runtimeTray`, slash completion, and file
completion. Use it in `handleTrayGlow`. In `routeToInteraction`, start the glow when completion
state changes from closed to open, as well as for the existing Up/Down cursor-change condition.

**Step 2: Format and run the focused test**

Run: `gofmt -w internal/presentation/screen.go internal/presentation/screen_test.go`

Run: `go test -race ./internal/presentation -run TestModernCompletionTraySelectionRunsBackgroundTransition -count=1`

Expected: PASS.

### Task 3: Verify and commit

**Files:**
- Modify: `internal/presentation/screen.go`
- Test: `internal/presentation/screen_test.go`
- Create: `docs/plans/2026-08-08-unified-tray-selection-glow.md`

**Step 1: Run module verification**

Run: `go test -race ./...`

Run: `CGO_ENABLED=0 go build -trimpath ./...`

Run: `make secure`

Expected: all tests, build, formatting, static analysis, security analysis, module verification,
and vulnerability checks pass.

**Step 2: Commit**

Run: `git add internal/presentation/screen.go internal/presentation/screen_test.go docs/plans/2026-08-08-unified-tray-selection-glow.md`

Run: `git commit -m "fix(tui): animate selection across all trays"`
