# Semantic Tool-Run Summaries Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render collapsed TUI tool runs as grouped semantic activity counts instead of raw tool names.

**Architecture:** Keep concrete `ToolCallView` values unchanged and add a TUI-local mapping from built-in tool names to singular/plural activity phrases. `toolRunSummary` groups calls by mapped activity in first-seen order, aggregates unknown names under `tools used`, and preserves category and node failure state.

**Tech Stack:** Go, table-driven tests, Bubble Tea TUI presentation package.

---

### Task 1: Pin semantic summary behavior

**Files:**
- Modify: `internal/presentation/summary_test.go`

**Step 1: Write the failing tests**

Replace raw-name summary expectations with table-driven coverage for all known
tool mappings, singular/plural grammar, stable first-use ordering, unknown-tool
aggregation, mixed runs, and category failure marking.

**Step 2: Run the focused test to verify it fails**

Run: `go test -race ./internal/presentation -run 'TestTool(Activity|RunSummary)'`

Expected: FAIL because `toolRunSummary` still renders `N tools · Name, Name`.

**Step 3: Commit the red tests**

Run: `git add internal/presentation/summary_test.go && git commit -m 'test(tui): specify semantic tool summaries'`

### Task 2: Implement grouped activity summaries

**Files:**
- Modify: `internal/presentation/summary.go`
- Modify: `internal/presentation/summary_test.go`

**Step 1: Add the minimal formatter implementation**

Define a private activity descriptor containing singular and plural phrases, a
static mapping for the approved built-in names, and an ordered accumulator. Map
unknown names to the shared fallback descriptor. Mark a group failed when any of
its calls fails, then join formatted groups with `, `.

**Step 2: Run the focused tests to verify they pass**

Run: `go test -race ./internal/presentation -run 'TestTool(Activity|RunSummary|CallFailed)'`

Expected: PASS.

**Step 3: Run the complete module checks**

Run: `make fmt-check`

Expected: PASS.

Run: `go test -race ./...`

Expected: PASS.

Run: `CGO_ENABLED=0 go build -trimpath ./...`

Expected: PASS.

**Step 4: Commit the implementation**

Run: `git add internal/presentation/summary.go internal/presentation/summary_test.go docs/plans/2026-08-08-semantic-tool-run-summaries-design.md docs/plans/2026-08-08-semantic-tool-run-summaries.md && git commit -m 'feat(tui): summarize tool runs by activity'`
