# Compact Composer Metadata Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render the composer status and footer as `○ idle · <model> · <context>`, then `<workspace> [<PROFILE>]` above a compact agent row whose focused agent is bright and whose other agents remain subtle.

**Architecture:** Keep the change in `internal/presentation`, where the reusable TUI already owns status, session metadata, and loop-bar rendering. Preserve the existing surface spacing and click geometry while changing only the text and styles emitted by those components.

**Tech Stack:** Go 1.26, Bubble Tea v2, Lip Gloss v2, standard Go tests with the race detector.

---

### Task 1: Pin the approved status and workspace metadata

**Files:**
- Modify: `internal/presentation/screen_test.go`
- Modify: `internal/presentation/runtimecontrol_test.go`
- Modify: `internal/presentation/sessionpresentation.go`
- Modify: `internal/presentation/screen.go`

**Step 1: Write the failing tests**

Add assertions that runtime metadata is joined to the lifecycle status with ` · ` and that the footer header is exactly `<workspace> [<UPPERCASE PROFILE>]`, without the agent banner name.

**Step 2: Run tests to verify they fail**

Run: `go test -race ./internal/presentation -run 'TestModernStatusSeparatesRuntimeMetadata|TestSessionPresentationFooterShowsFixedProfileAndWorkspace'`

Expected: FAIL because the current status uses two spaces and the current footer renders `CodeRig · Profile · /workspace`.

**Step 3: Write the minimal implementation**

Format the non-empty profile as an uppercase bracketed badge after the workspace and join it with a single space. Exclude `AgentBanner.Name` from the persistent footer header. Join the lifecycle status and runtime metadata with the same middle-dot separator already used inside runtime metadata.

**Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/presentation -run 'TestModernStatusSeparatesRuntimeMetadata|TestSessionPresentationFooterShowsFixedProfileAndWorkspace'`

Expected: PASS.

### Task 2: Simplify and restyle the agent row

**Files:**
- Modify: `internal/presentation/loopbar_test.go`
- Modify: `internal/presentation/loopbar.go`

**Step 1: Write the failing tests**

Change loop-bar expectations to `● <name>` / `○ <name>` with no mode or short ID. Assert the focused segment uses a bright white foreground without the faint attribute, while unfocused segments retain the subtle status style.

**Step 2: Run tests to verify they fail**

Run: `go test -race ./internal/presentation -run 'TestLoopBarRender|TestLoopBarFocusedStyle'`

Expected: FAIL because loop segments currently include mode/ID metadata and the focused segment is still faint.

**Step 3: Write the minimal implementation**

Reduce each segment body to its mark and name. Render the focused segment bright white and bold; render all other segments with `styles.StatusStyle`. Keep gate marks, ordering, overflow, wrapping, and hit testing unchanged.

**Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/presentation -run 'TestLoopBarRender|TestLoopBarFocusedStyle|TestLoopBarHitTest|TestLoopFooter'`

Expected: PASS.

### Task 3: Verify the complete TUI module

**Files:**
- Modify: formatting only if required by `gofmt`

**Step 1: Format**

Run: `make fmt`

**Step 2: Run the full race suite**

Run: `go test -race ./...`

Expected: PASS.

**Step 3: Run repository quality checks**

Run: `make lint`

Expected: PASS.
