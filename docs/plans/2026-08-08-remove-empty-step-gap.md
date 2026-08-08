# Remove Empty Step Gap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove fully empty TUI rows between an assistant step's bare rail spacer and its following tool node or collapsed tool summary.

**Architecture:** Normalize the final focused transcript composition at the committed/live seam, where entry-local renderers have already emitted their intended railed spacing. Remove only unstyled empty rows owned by a committed assistant entry and bracketed by its bare `│` row and a tool-node row; provenance preserves turn-boundary spacing and visually identical bare rails emitted by tool output.

**Tech Stack:** Go, Bubble Tea v2, Lip Gloss v2, standard `testing` package.

---

### Task 1: Pin empty-step-gap normalization

**Files:**
- Modify: `internal/presentation/screen_test.go`

1. Add a failing table test for collapsed, expanded, already-correct, and real turn-boundary shapes.
2. Run `GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test ./internal/presentation -run TestRemoveEmptyStepGaps -count=1 -v` and confirm it fails because `removeEmptyStepGaps` is undefined.

### Task 2: Normalize the focused transcript seam

**Files:**
- Modify: `internal/presentation/screen.go`
- Test: `internal/presentation/screen_test.go`

1. Add `removeEmptyStepGaps`, which removes an unstyled empty-row run only when its provenance belongs to a committed assistant, it is preceded by that assistant's bare rail, and it is followed by a `○`/`◍` tool node.
2. Apply it after committed and live lines are composed, before queued inputs are appended.
3. Add an integration test for the committed-thinking to live-tool seam.
4. Run the focused presentation tests.

### Task 3: Verify the TUI

1. Run `GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test ./internal/presentation -count=1`.
2. Run `GOWORK=off GOCACHE=/private/tmp/looprig-tui-gocache go test ./... -count=1`.
3. Review the diff for scope and formatting.
