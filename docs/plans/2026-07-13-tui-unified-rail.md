# TUI Unified Rail — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render an assistant turn as one continuous vertical `│` timeline — thinking rail head, neon `●` AI-message node, status-tinted hollow-circle (`○`/`◍`) tool and subagent nodes as peers on the rail — with completed tool lists collapsing to a summary node under the existing `ctrl+t` fold.

**Architecture:** View-layer-only change in package `tui`. Introduce a small **rail-gutter primitive** (node rows carry the glyph; connector/detail rows carry `│`). Convert tool cards from indented `⎿` children into peer `○` nodes. Make the rail continuous across `kindAssistant`/`kindTool` entry boundaries by turning the inter-entry blank separator into a `│` connector within a turn. Subagents open a nested secondary rail and close with an `○ done`/`○ failed` node. A folded step's contiguous `kindTool` run collapses to one `○ N tools · names` summary node, aggregated in `Screen.renderFocused`. The domain `content` types are untouched.

**Tech Stack:** Go (stdlib + Bubble Tea v2 stack), `lipgloss` styling, `github.com/looprig/core/content` domain types. Tests are table-driven, `-race`, substring/`stripANSI`-based (no golden files).

**Design doc:** `docs/plans/2026-07-13-tui-unified-rail-design.md`

**Reference — current code anchors (from pipeline map):**
- `tui/render.go`: `cardConnector="⎿ "` (:40), `cardIndent/resultIndent` (:44-47), `glyph*` consts (:51-56), `multipleActionsHeadline` (:70), `toolGlyph` (:138-149), `renderToolCallsGlyph` (:169), `renderToolCard` (:191), `toolHeaderText` (:211), `previewLines`/`previewLineCap=3` (:272, :21), `renderAssistant` (:318), `renderPromotedTool` (:344), `renderSubagentCard` (:382), `subagentHeaderText` (:432), `subagentDoneLine` (:445), `renderThinking`/`thinkingRail="│ "` (:621, :635), `renderLiveAssistant` (:494), `liveToolGlyph` (:553).
- `tui/entryrender.go`: `renderEntry` dispatch (:22-54), `harnessMark="○ "` (:59).
- `tui/transcript.go`: `entry` struct + `headline`/`promoted` fields (:166-209), `entryKind` consts `kindUser`(:107)…`kindHarness`(:128), `flushCalls` (:1004), `commitCall` (:1493-1505).
- `tui/screen.go`: `renderFocused` (:1112), `blankSeparator` (:1150), `suppressSeparator` (:1161-1166), `liveTailLines` (:1179).
- `tui/message.go`: `ToolCallView` (:53-83), `ToolStatus` (:10-15), `subStatus` (:38-43).
- `tui/styles/styles.go`: `LitDot` (:112), `DotColor="#D4F84D"` (:23), `ThinkingStyle/ToolCallStyle/ToolResultStyle/SubagentStyle` all `Faint(true)`.
- `tui/collapse.go`: `collapseState` (`globalCollapsed` default true, `Effective`, `Toggle`, `ToggleAll`).

**Global conventions for every task:** format `make fmt`; test `go test -race ./tui/...`; security gate `make secure` before the final commit. Match surrounding comment density and naming. All new errors typed (none expected here — this is pure rendering).

---

## Task 1: Status-tinted rail node glyphs in the styles package

Add the node vocabulary so every later task styles from one place (DRY).

**Files:**
- Modify: `tui/styles/styles.go` (after `LitDot`, ~:112)
- Test: `tui/styles/styles_test.go` (create if absent)

**Step 1 — Write the failing test.** Add a table-driven test asserting each node style renders the expected glyph and is non-empty, and that widths match (glyph + trailing space = 2 columns, like `LitDot`).

```go
func TestRailNodeGlyphs(t *testing.T) {
	tests := []struct {
		name  string
		got   string
		glyph string
	}{
		{"lit ai node", LitDot, "●"},
		{"tool ok node", ToolNode(NodeOK), "○"},
		{"tool failed node", ToolNode(NodeFailed), "○"},
		{"tool running node", ToolNode(NodeRunning), "◍"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tt.got, tt.glyph) {
				t.Errorf("style = %q, want glyph %q", tt.got, tt.glyph)
			}
			if lipgloss.Width(tt.got) != 2 {
				t.Errorf("width = %d, want 2", lipgloss.Width(tt.got))
			}
		})
	}
}
```

**Step 2 — Run, expect FAIL.** `go test -race ./tui/styles/ -run TestRailNodeGlyphs` → FAIL (undefined `ToolNode`/`NodeOK`…).

**Step 3 — Implement.** Add to `styles.go`:

```go
// NodeStatus selects a rail node's tint. A hollow-circle tool/subagent node is
// faint when OK, red when failed, and a pulsing filled glyph while running.
type NodeStatus uint8

const (
	NodeOK NodeStatus = iota
	NodeFailed
	NodeRunning
)

// dotNodeHollow is the tool/subagent node glyph; dotNodeRunning is its running form.
const (
	dotNodeHollow  = "○"
	dotNodeRunning = "◍"
)

// FailColor tints a failed rail node and its header. Kept distinct from DotColor so a
// failure reads red against the neon assistant bullet.
var FailColor = lipgloss.Color("#FF6B6B")

// ToolNode returns the COLORED node glyph (glyph + trailing space, 2 columns wide like
// LitDot) for a tool/subagent node at the given status.
func ToolNode(s NodeStatus) string {
	switch s {
	case NodeFailed:
		return lipgloss.NewStyle().Foreground(FailColor).Render(dotNodeHollow) + " "
	case NodeRunning:
		return lipgloss.NewStyle().Foreground(DotColor).Render(dotNodeRunning) + " "
	default: // NodeOK
		return lipgloss.NewStyle().Faint(true).Render(dotNodeHollow) + " "
	}
}
```

**Step 4 — Run, expect PASS.** `go test -race ./tui/styles/ -run TestRailNodeGlyphs`.

**Step 5 — Commit.** `git add tui/styles/ && git commit -m "feat(tui): add status-tinted rail node glyphs"`

---

## Task 2: Rail-gutter primitive

One helper that renders the rail rows: a **node row** (glyph replaces the rail), a **detail row** (`│ ` gutter + wrapped text), and a **connector row** (bare `│`). Every later renderer builds on this.

**Files:**
- Create: `tui/rail.go`
- Test: `tui/rail_test.go`

**Step 1 — Failing test.** Cover: node row begins with the given glyph; detail row begins with the styled `│` gutter and indents its text; connector row is exactly the styled `│`; a nested `depth` shifts the gutter right by `railWidth` per level.

```go
func TestRailRows(t *testing.T) {
	tests := []struct {
		name       string
		got        string
		wantPrefix string
	}{
		{"node row", stripANSI(railNode(styles.LitDot, "hello", 40)), "● hello"},
		{"detail row", stripANSI(railDetail("42 lines", 0, 40)), "│ 42 lines"},
		{"connector row", stripANSI(railConnector(0)), "│"},
		{"nested detail row", stripANSI(railDetail("12 lines", 1, 40)), "│ ○-gutter"}, // adjust to real nested form
	}
	// ... assert strings.HasPrefix(tt.got, tt.wantPrefix)
}
```
(Refine the nested expectation to the concrete gutter you implement — outer `│` spine + nested column, per design.)

**Step 2 — Run, expect FAIL** (undefined helpers).

**Step 3 — Implement `tui/rail.go`.** Constants and helpers:

```go
package tui

import (
	"strings"

	"github.com/looprig/cli/tui/styles" // adjust to real module path
)

// railGlyph is the vertical rail; railWidth is its display width (glyph + space).
const (
	railGlyph = "│ "
	railWidth = 2
)

// railGutter returns the styled rail prefix for a row at the given nesting depth: the
// outer spine plus `depth` nested spines (design: outer │ + nested column).
func railGutter(depth int) string {
	return strings.Repeat(styles.ThinkingStyle.Render(railGlyph), depth+1)
}

// railConnector renders a bare connector row — the rail with no node — at depth.
func railConnector(depth int) string {
	return strings.TrimRight(railGutter(depth), " ")
}

// railNode renders a node row: the node glyph (2 cols, replacing the rail) at the row's
// depth, followed by width-wrapped text hanging under the glyph. glyph is pre-styled
// (styles.LitDot / styles.ToolNode(...)).
func railNode(glyph, text string, width int) []string { /* wrap text to width-railWidth; first line glyph+line, continuations railWidth-indent */ }

// railDetail renders detail rows under a node: the `│ ` gutter at depth+1 (nested one
// level under the node) plus width-wrapped, faint text.
func railDetail(text string, depth, width int) []string { /* gutter := railGutter(depth+1); wrap; prefix each line */ }
```

Reuse existing `wrapToWidth`/`indentWrap` from `render.go` rather than re-implementing wrapping (DRY).

**Step 4 — Run, expect PASS.**

**Step 5 — Commit.** `git commit -m "feat(tui): add rail-gutter rendering primitive"`

---

## Task 3: Tool call renders as an `○` peer node

Replace the `⎿`/indent card with a rail node. This is the visible core of the change for a single tool.

**Files:**
- Modify: `tui/render.go` — `renderToolCard` (:191), `toolGlyph` usage, `renderToolCallsGlyph` (:169)
- Test: `tui/render_test.go`

**Step 1 — Failing test.** A committed OK tool card renders `○ Read(config.go)` (not `⎿`), with its result on a `│ ` detail row; a failed card uses the red node; running (live path) uses `◍`.

```go
func TestRenderToolCard_RailNode(t *testing.T) {
	tests := []struct {
		name    string
		call    ToolCallView
		want    []string // substrings, on stripANSI output, in order
		notWant string
	}{
		{"ok node", ToolCallView{ToolName: "Read", Summary: "config.go", Status: ToolOK, Result: []string{"42 lines"}},
			[]string{"○ Read(config.go)", "│ 42 lines"}, "⎿"},
		{"failed node", ToolCallView{ToolName: "Bash", Summary: "go test", Status: ToolError, Result: []string{"FAIL"}},
			[]string{"○ Bash(go test)", "│ FAIL"}, "⎿"},
	}
	// render via renderToolCalls(...) with expand=true; stripANSI; assert Contains all want, NOT Contains notWant
}
```

**Step 2 — Run, expect FAIL** (still emits `⎿`).

**Step 3 — Implement.** Map `ToolStatus` → `styles.NodeStatus` (new small helper `toolNodeStatus(ToolStatus) styles.NodeStatus`: `ToolOK→NodeOK`, `ToolError→NodeFailed`, `ToolRunning→NodeRunning`, `ToolCancelled→NodeFailed`). Rewrite `renderToolCard` to emit `railNode(styles.ToolNode(...), toolHeaderText(c, ""), width)` for the header (drop the glyph-suffix `"  ✓"` from `toolHeaderText` calls at node level — the node glyph now carries status; keep the header text without the trailing glyph, or pass an empty glyph and trim). Emit result lines via `railDetail(rl, 0, width)` looping over `previewLines(c.Result, expand)`. Keep `liveRunning && ToolRunning` header-only behavior.

Adjust `toolHeaderText` (or add a node variant) so it no longer appends `"  " + glyph` when rendering a node — the status is in the node glyph now. Preserve the `verb ToolName(detail)` text.

**Step 4 — Run, expect PASS.** `go test -race ./tui -run TestRenderToolCard_RailNode`. Also run the full `./tui` suite — existing `⎿`-substring assertions will now FAIL; update those in Task 9 (or update the directly-affected ones here and note it). Prefer: update the few directly-broken assertions in `render_test.go`/`entryrender_test.go` in this commit to keep the suite green.

**Step 5 — Commit.** `git commit -m "feat(tui): render tool calls as rail nodes"`

---

## Task 4: AI-message node + node-presence rules (drop the umbrella)

`renderAssistant` emits the thinking rail head, then the `●` node **only when text is present**. Remove `● Multiple actions` and `renderPromotedTool`.

**Files:**
- Modify: `tui/render.go` — `renderAssistant` (:318), delete `renderPromotedTool` (:344) + `multipleActionsHeadline` (:70)
- Modify: `tui/entryrender.go` — `renderEntry` `kindAssistant`/`kindTool` cases (:26-40) drop the `promoted`/`headline` paths
- Modify: `tui/transcript.go` — stop setting `headline`/`promoted` (search `multipleActionsHeadline`, `promoted = true`); commit empty-text steps as plain tool entries
- Test: `tui/render_test.go`, `tui/entryrender_test.go`

**Step 1 — Failing tests.** Table over the node-presence matrix:

| case | thinking | text | tools | expect |
|------|----------|------|-------|--------|
| content only | – | "hi" | – | `●` node, no `│ thought`, no `○` |
| thinking only | yes | – | – | `│ thought`, no `●` |
| thinking+tools | yes | – | 2 | `│ thought`, no `●`, no "Multiple actions" |
| all three | yes | "x" | 1 | `│ thought`, `●`, `○` |

Assert absence of `"Multiple actions"` in every case.

**Step 2 — Run, expect FAIL.**

**Step 3 — Implement.** In `renderAssistant`, keep the thinking-rail emission; render the `●` node via `railNode(styles.LitDot, text, width)` (through `renderMD`) **only when `text != ""`**; delete the `body == "" && headline != ""` branch. Remove `renderPromotedTool`, `multipleActionsHeadline`, and the `renderEntry` branches that call them (:34-39 keep the subagent branch, drop the `promoted` branch). In `transcript.go`, the empty-text single/multi-tool step just commits its calls as ordinary `kindTool` entries (remove `promoted`/`headline` assignment); leave the `entry.headline`/`entry.promoted` fields unused for now (remove in Task 9 cleanup).

**Step 4 — Run, expect PASS.**

**Step 5 — Commit.** `git commit -m "feat(tui): AI-message node presence rules, drop Multiple-actions umbrella"`

---

## Task 5: Continuous rail across entry boundaries

Turn the inter-entry blank separator into a `│` connector **within a turn** so the rail is unbroken. Between turns (before a user row, notice, etc.) keep a real blank.

**Files:**
- Modify: `tui/screen.go` — `renderFocused` (:1112), `suppressSeparator` (:1161), `blankSeparator` (:1150)
- Test: `tui/screen_test.go`

**Step 1 — Failing test.** Build a committed sequence assistant(thinking+text) → tool → tool and assert the rendered focused lines contain a `│` connector row between the `●` node and the first `○` node, and between the two `○` nodes (no fully-blank row inside the turn). Assert a blank row still precedes a following `kindUser` entry.

**Step 2 — Run, expect FAIL** (blank separators inside the turn).

**Step 3 — Implement.** Introduce `intraTurnSeparator(committed, i)`: true when the separator sits between two nodes of the same turn (current is `kindAssistant`/`kindTool`/`kindSubagent` AND next is `kindTool`/`kindSubagent`). When true, emit a `railConnector(0)` renderedLine instead of `blankSeparator`. Otherwise keep `blankSeparator`. Replace the current `suppressSeparator` (which *omits* the blank to glue narration→tools) with this connector-emitting logic so the glue becomes a visible rail segment.

**Step 4 — Run, expect PASS.** Also eyeball via the run harness (Task 10).

**Step 5 — Commit.** `git commit -m "feat(tui): continuous rail connector between turn nodes"`

---

## Task 6: Subagent node + nested secondary rail

`renderSubagentCard` becomes an `○` node opening a nested rail (outer `│` spine + `│  ○` nested column), closing with an `○ done`/`○ failed` node.

**Files:**
- Modify: `tui/render.go` — `renderSubagentCard` (:382), `subagentDoneLine` (:445), `subagentHeaderText` (:432)
- Test: `tui/render_test.go`

**Step 1 — Failing test.** A done subagent with two child tools renders: `○ Subagent(explore)` header node; each child as a nested node `│ ○ Read(...)` (depth 1); a closing `○ done · 2 steps` nested node; failed variant uses red nodes and `failed · N steps`. Assert no `⎿` anywhere and the closing line is a node (starts with `○`, not plain text).

**Step 2 — Run, expect FAIL** (still `⎿` children + plain done line).

**Step 3 — Implement.** Header via `railNode(styles.ToolNode(subagentNodeStatus(c.SubStatus)), subagentHeaderText(c), width)`. Children via a depth-1 render: loop `c.Children`, each `railNode` at depth 1 (add a `depth` param to the node/tool renderers, or a `renderToolNodeAt(c, depth, width)` helper) with `railDetail(..., 1, ...)` results. Closing node: `railNode(styles.ToolNode(subagentNodeStatus(c.SubStatus)), subagentDoneLine(c), width)` at depth 1. Preserve the `+N nested subagent steps` line as a depth-1 detail row. Map `subStatus`→`NodeStatus` (`subDone→NodeOK`, `subFailed→NodeFailed`, `subInterrupted→NodeFailed`, `subRunning→NodeRunning`).

**Step 4 — Run, expect PASS.**

**Step 5 — Commit.** `git commit -m "feat(tui): subagent nested rail with closing done node"`

---

## Task 7: Collapsed step summary node (shared `ctrl+t` fold)

A folded step's contiguous run of `kindTool`/subagent entries collapses to one `○ N tools · names` summary node. Aggregation happens in `renderFocused`. Active (live) step is unaffected (it renders via the live tail, always expanded).

**Files:**
- Modify: `tui/screen.go` — `renderFocused` (:1112) run-grouping; `tui/collapse.go` (reuse `Effective`); new `tui/summary.go` for the summary-node builder
- Test: `tui/screen_test.go`, `tui/summary_test.go`

**Step 1 — Failing tests.**
- `summary_test.go`: `toolRunSummary([]ToolCallView{...})` → `"3 tools · Read, Bash, Grep"`; a failed call marks `✗` (`"Bash ✗"`) and reports `anyFailed==true`; a subagent call shows `Subagent(explore) ✓`. Single call → `"1 tool · Read"`. Long lists width-truncate.
- `screen_test.go`: with fold collapsed (`globalCollapsed` true, default), a committed run of 3 tool entries after an assistant renders **one** `○ 3 tools · …` node and NOT the individual `○ Read(...)` nodes; with the run's fold toggled open (`Toggle(runID)`), it renders each node and not the summary; `ToggleAll()` flips all runs.

**Step 2 — Run, expect FAIL.**

**Step 3 — Implement.**
- `tui/summary.go`: `toolRunSummary(calls []ToolCallView) (text string, anyFailed bool)` building `"N tools · name, name"` (use `hintSeparator`), appending `" ✗"` per failed call, subagent name as `Subagent(agent) ✓/✗`. Node status = `NodeFailed` if `anyFailed` else `NodeOK`.
- In `renderFocused`, when iterating committed entries, detect a maximal contiguous run of `kindTool`/single-call subagent entries that belongs to one step. Determine the run's fold via `collapse.Effective(runID)` where `runID` = the run's first entry ID. If collapsed → emit one `railNode(styles.ToolNode(status), summaryText, width)` (clickable → toggles `runID`); else → emit each entry's node as today. Record the summary node's clickable region against `runID` (mirror how thinking-header clicks map to `Toggle(id)` — find the existing click-region wiring in `interaction.go`/`screen.go` and register the summary row the same way).

**Step 4 — Run, expect PASS.** Verify click + `ctrl+t` in the run harness (Task 10).

**Step 5 — Commit.** `git commit -m "feat(tui): collapse done tool run to a summary node"`

---

## Task 8: Live-tail parity

The streaming tail renders the same rail; a running node is `◍` pulsing. It always renders expanded (no collapse) and folds only once committed.

**Files:**
- Modify: `tui/render.go` — `renderLiveAssistant` (:494), `renderToolCallsGlyph` live path (:169), `liveToolGlyph` (:553)
- Modify: `tui/screen.go` — `liveTailLines` (:1179)
- Test: `tui/render_test.go` (live cases), `tui/screen_test.go`

**Step 1 — Failing test.** `renderLiveAssistant` with a running call renders a `◍` node (pulsing glyph) and a `●`/blinking AI node when text present; the whole live segment uses the `│` rail (no `⎿`); an empty-text live step renders the running tool node directly (no "working…" umbrella unless you keep the working-word as the node label — decide and assert). Assert continuity: connector `│` between the AI node and the first tool node.

**Step 2 — Run, expect FAIL.**

**Step 3 — Implement.** Port `renderLiveAssistant` to the rail primitive exactly as the committed path (Tasks 3–4), passing the animated node glyph for the running card (`styles.ToolNode(NodeRunning)`, optionally phase-varied via existing `a.frame`/`liveToolGlyph` for the pulse). Keep the `liveCallCap`/"… N earlier calls" behavior as a rail detail/node. Ensure `liveTailLines` seams onto the committed rail (the last committed connector flows into the live nodes).

**Step 4 — Run, expect PASS.**

**Step 5 — Commit.** `git commit -m "feat(tui): live tail uses the unified rail with pulsing running node"`

---

## Task 9: Remove dead code + reconcile remaining tests

**Files:**
- Modify: `tui/transcript.go` — remove now-unused `entry.headline` and `entry.promoted` fields and their doc comments; adjust `EqualTranscript` if it references them
- Modify: `tui/render.go` — remove `cardConnector`, `cardIndent`, `resultIndent`, `multipleActionsHeadline` if fully unused (verify with grep); remove `renderPromotedTool` if not already
- Modify: any remaining `⎿`/`Multiple actions`/`promoted`-referencing tests across `tui/*_test.go`
- Test: whole `tui` suite

**Step 1 — Find stragglers.** `grep -rn "⎿\|cardConnector\|cardIndent\|resultIndent\|multipleActionsHeadline\|renderPromotedTool\|\.promoted\|\.headline" tui/ | grep -v _test`. Then the `_test.go` equivalents.

**Step 2 — Delete/adjust** each, updating comments that describe the old `⎿` layout (e.g. `entryrender.go` header doc, `entry` struct docs).

**Step 3 — Run full suite.** `go test -race ./tui/...` → PASS. `go vet ./tui/...` clean.

**Step 4 — Commit.** `git commit -m "refactor(tui): drop dead card-child layout and umbrella fields"`

---

## Task 10: End-to-end verification, fuzz, and security gate

**Step 1 — Drive the real TUI.** Use the `/run` skill (or `verify` skill) to launch the app and exercise: a turn with thinking + text + multiple tools (see continuous rail); a failing tool (red node); a subagent (nested rail + `○ done`); collapse/expand via `ctrl+t` and a click on a summary node; a live streaming turn (pulsing `◍`, seams to committed). Confirm visually against the design doc mockups.

**Step 2 — Fuzz the summary builder.** If `toolRunSummary` parses/truncates arbitrary tool names, add/extend a fuzz target near `blocks_fuzz_test.go`: `go test -fuzz=FuzzToolRunSummary ./tui -fuzztime=30s`.

**Step 3 — Full gate.** `make fmt-check`, `go test -race ./...`, `make secure`. All green.

**Step 4 — Final commit + open PR.** `git commit` any doc touch-ups, then (only if the user asks) open the PR against `main` summarizing the unified-rail change and linking the design doc.

---

## Ordering rationale & green-build invariant

Tasks 1–2 are additive (nothing breaks). Task 3 flips tool rendering and updates its directly-affected assertions in the same commit. Task 4 removes the umbrella. Task 5 makes the rail continuous. Task 6 does subagents. Task 7 adds collapse (the one screen.go-level aggregation — the trickiest task; keep its click-region wiring aligned with the existing thinking-header click). Task 8 brings the live path to parity. Task 9 sweeps dead code so no `⎿` remains. Task 10 verifies end-to-end and runs the security gate. The suite is substring/`stripANSI`-based with no golden files, so assertion churn is localized to glyph/prefix substrings.

## Open risks to watch during execution
- **Click-region wiring for the summary node** (Task 7): the summary row must map to `Toggle(runID)` exactly like a thinking header maps to its entry — locate and mirror that wiring; do not invent a parallel path.
- **Run-grouping boundaries** (Task 7): a subagent entry (`Agent != ""`, single call) is part of the step run; a `kindNotice`/`kindUser`/`kindInterrupted` ends it. Cover with a table test.
- **Nested-rail width math** (Task 6): each depth costs `railWidth` columns; wrap tool detail to `width - railWidth*(depth+1)` so nested text never overruns.
- **`EqualTranscript` / restore-equivalence**: removing `headline`/`promoted` must not break the projection-equality tests (`displayprojection_test.go`); adjust the normalization if those fields were compared.
