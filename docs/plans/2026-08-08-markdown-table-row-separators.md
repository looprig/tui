# Markdown Table Row Separators Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make wrapped Markdown table rows visually distinct by drawing a horizontal separator between logical body rows.

**Architecture:** Keep Glamour's current Markdown parsing and Lip Gloss wrapping pipeline. Wrap the renderer in `styles`, insert collision-free marker rows between Markdown body rows before rendering, and translate those rendered marker rows into horizontal separators afterward.

**Tech Stack:** Go, Goldmark GFM parser, Glamour v2, Lip Gloss v2, standard `testing` package

---

### Task 1: Render separators between wrapped table rows

**Files:**
- Modify: `styles/styles_test.go`
- Modify: `styles/styles.go`
- Modify: `internal/presentation/render.go`
- Modify: `go.mod`
- Create: `styles/markdown_table_fuzz_test.go`

**Step 1: Write the failing test**

Add `TestMarkdownTableSeparatesWrappedBodyRows` to `styles/styles_test.go`. Render a narrow Markdown table with two body rows and a long first-row description. Strip ANSI styling, locate the wrapped continuation and second-row text, and assert that a horizontal row containing `─` occurs between them.

**Step 2: Run the focused test and verify it fails**

Run: `go test ./styles -run TestMarkdownTableSeparatesWrappedBodyRows -count=1`

Expected: FAIL because Glamour enables header and column borders but not body-row borders.

**Step 3: Implement the local renderer adapter**

Add a TUI-owned `RenderMarkdown` adapter around the existing `glamour.TermRenderer`. Before rendering, use Goldmark's GFM AST positions to insert a private-use marker row between adjacent body rows. After rendering, replace each marker row with a horizontal separator while preserving its column junctions. Bypass preprocessing for documents without multi-row tables, and retain `NewMarkdownRenderer`'s existing return type.

**Step 4: Run focused tests and verify they pass**

Run: `go test ./styles -count=1`

Expected: PASS.

**Step 5: Run the full regression suite**

Run: `go test ./...`

Expected: PASS.

**Step 6: Commit**

```bash
git add go.mod internal/presentation/render.go styles/styles.go styles/styles_test.go styles/markdown_table_fuzz_test.go docs/plans/2026-08-08-markdown-table-row-separators-design.md docs/plans/2026-08-08-markdown-table-row-separators.md
git commit -m "fix(tui): separate wrapped markdown table rows"
```
