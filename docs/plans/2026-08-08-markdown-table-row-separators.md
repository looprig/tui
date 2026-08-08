# Markdown Table Row Separators Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make wrapped Markdown table rows visually distinct by drawing a horizontal separator between logical body rows.

**Architecture:** Keep Glamour's current Markdown parsing and Lip Gloss wrapping pipeline. Enable Lip Gloss body-row borders in the vendored Glamour table adapter, then pin the user-visible behavior through the public TUI Markdown renderer.

**Tech Stack:** Go, Glamour v2, Lip Gloss v2, standard `testing` package

---

### Task 1: Render separators between wrapped table rows

**Files:**
- Modify: `styles/styles_test.go`
- Modify: `vendor/charm.land/glamour/v2/ansi/table.go`

**Step 1: Write the failing test**

Add `TestMarkdownTableSeparatesWrappedBodyRows` to `styles/styles_test.go`. Render a narrow Markdown table with two body rows and a long first-row description. Strip ANSI styling, locate the wrapped continuation and second-row text, and assert that a horizontal row containing `─` occurs between them.

**Step 2: Run the focused test and verify it fails**

Run: `go test ./styles -run TestMarkdownTableSeparatesWrappedBodyRows -count=1`

Expected: FAIL because the current adapter enables header and column borders but not body-row borders.

**Step 3: Implement the minimal renderer change**

In `TableElement.setBorders`, add:

```go
ctx.table.lipgloss.BorderRow(true)
```

Keep the existing disabled outer borders and enabled header/column behavior unchanged.

**Step 4: Run focused tests and verify they pass**

Run: `go test ./styles -count=1`

Expected: PASS.

**Step 5: Run the full regression suite**

Run: `go test ./...`

Expected: PASS.

**Step 6: Commit**

```bash
git add styles/styles_test.go vendor/charm.land/glamour/v2/ansi/table.go docs/plans/2026-08-08-markdown-table-row-separators.md
git commit -m "fix(tui): separate wrapped markdown table rows"
```
