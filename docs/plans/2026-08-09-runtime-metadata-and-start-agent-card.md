# Runtime Metadata and StartAgent Card Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render `model · effort · context` with explicit `none`, and keep one canonical `StartAgent(<agent>)` card across either parent/child event order.

**Architecture:** Keep runtime formatting in `Screen.focusedRuntimeStatus`. Extend the transcript accumulator/correlation state so a child arriving after the parent tool commit can promote that existing card in place; provisional cards use the current canonical tool name while committed legacy cards retain their stored name.

**Tech Stack:** Go, Bubble Tea presentation model, table/focused tests, Go race detector.

---

### Task 1: Runtime metadata

**Files:**
- Modify: `internal/presentation/screen_test.go`
- Modify: `internal/presentation/screen.go`

1. Add a failing focused test asserting `model · none · context` for an event runtime whose effort is the zero value.
2. Run the focused test and confirm it fails because `none` is absent.
3. Make `focusedRuntimeStatus` append `none` for a present model with empty effort, without adding mode metadata.
4. Run the focused test and existing runtime projection/status tests.

### Task 2: Canonical provisional StartAgent label

**Files:**
- Modify: `internal/presentation/transcript_test.go`
- Modify: `internal/presentation/transcript.go`

1. Add a failing test asserting an unreconciled current child renders a pending `StartAgent(generic)` card.
2. Run it and confirm the current `Subagent(generic)` label fails.
3. Change only provisional card construction to use `StartAgent`; preserve the tool name already stored on committed cards.
4. Run the focused transcript tests.

### Task 3: Parent-first reconciliation

**Files:**
- Modify: `internal/presentation/transcript_test.go`
- Modify: `internal/presentation/transcript.go`

1. Add a failing regression test that commits the parent `StartAgent` tool before child `LoopStarted`, then asserts exactly one promoted `StartAgent(generic)` card and no pending card.
2. Run it and confirm it fails with one raw committed card plus one pending card.
3. Record unresolved committed spawn-card identity and promote it in place when the matching child starts. Fail closed on absent or ambiguous correlation.
4. Run the focused transcript tests and ensure legacy `Subagent` tests still pass.

### Task 4: Verification

**Files:**
- No production changes expected.

1. Run `make fmt`.
2. Run `go test -race ./...`.
3. Run `make secure`.
4. Review the diff for unrelated changes and commit the implementation.
