# Collapsed Composer Paste Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show multiline composer pastes as `[pasted N chars]` while submitting their exact original content once.

**Architecture:** Extend `components.InputBox` with opaque paste segments keyed by unique visible markers. Keep `Value()` as the expanded submission value and add a compact display accessor for presentation-level change detection so existing routing and submission APIs remain intact.

**Tech Stack:** Go, Bubble Tea v2, Bubbles textarea, standard `testing` package.

---

### Task 1: Pin compact paste behavior in the input component

**Files:**
- Modify: `components/input_test.go`
- Modify: `components/input.go`

**Step 1: Write the failing tests**

Add tests that send a multiline `tea.PasteMsg` to `InputBox`, assert the rendered editor contains `[pasted N chars]`, assert `Value()` equals the original payload byte-for-byte, and assert removing the marker removes the retained payload. Add a single-line case proving ordinary paste stays visible and editable.

**Step 2: Run tests to verify they fail**

Run: `GOWORK=off go test ./components -run 'TestInputBox.*Paste' -count=1`

Expected: FAIL because multiline paste is rendered verbatim.

**Step 3: Implement the minimal component behavior**

Add private paste-segment state to `InputBox`. Intercept multiline `tea.PasteMsg` in `Update`, insert a unique `[pasted N chars]` marker, and retain its payload. Expand live markers in `Value()`. Clear retained segments from `Reset()` and `SetValue()`, and expose the compact textarea value through a presentation-only accessor.

**Step 4: Run the component tests**

Run: `GOWORK=off go test ./components -count=1`

Expected: PASS.

### Task 2: Pin one-turn submission at the presentation boundary

**Files:**
- Modify: `internal/presentation/paste_test.go`
- Modify: `internal/presentation/interaction.go`
- Modify: `internal/presentation/screen.go`

**Step 1: Write the failing regression test**

Extend the multiline paste test to assert the compact marker is displayed, no submit occurs on paste, and one subsequent Enter produces exactly one submission containing the original multiline payload.

**Step 2: Run the test to verify it fails**

Run: `GOWORK=off go test ./internal/presentation -run 'TestMultilinePaste' -count=1`

Expected: FAIL because the composer currently displays the full payload.

**Step 3: Adapt visible-change checks**

Use the compact display accessor anywhere presentation code compares the editor before and after routing a non-key message. Keep all submission sites on expanded `Value()`.

**Step 4: Run focused and full tests**

Run: `GOWORK=off go test ./internal/presentation -count=1`

Expected: PASS.

### Task 3: Verify the module

**Files:**
- Review: `components/input.go`
- Review: `components/input_test.go`
- Review: `internal/presentation/paste_test.go`

**Step 1: Format modified Go files**

Run: `gofmt -w components/input.go components/input_test.go internal/presentation/interaction.go internal/presentation/screen.go internal/presentation/paste_test.go`

**Step 2: Run native checks**

Run the repository's documented native check target.

Expected: PASS.

**Step 3: Run standalone tests**

Run: `GOWORK=off go test ./...`

Expected: PASS.

**Step 4: Review repository-local diff**

Run: `git diff --check` and `git status --short`.

Expected: no whitespace errors and only intended TUI files changed.
