# TUI Unified Rail — Design

**Date:** 2026-07-13
**Status:** Approved (design), pending implementation plan
**Scope:** `tui/` presentation layer only. No changes to `github.com/looprig/core/content` domain types.

## Problem

Today a committed assistant turn renders three visually distinct hierarchies:

- Thinking as a `│ ` rail head (`thinkingRail`), collapsible.
- The AI message as a neon `● ` bullet (`styles.LitDot`).
- Tool calls as **children** of the bullet: `⎿ ` cards (`cardConnector`) indented two
  columns (`cardIndent`), results indented four (`resultIndent`).

The `⎿` cards read as *belonging to* the `●` message. Empty-text steps synthesise a
`● Multiple actions` umbrella, or "promote" a single tool card up to the bullet
(`renderPromotedTool`). The result is three grammars for one linear sequence of events.

## Goal

Render a turn as **one continuous vertical timeline**. A single `│` rail runs unbroken
down the left; each event sits *on* the rail as a node. Tool calls become **peer nodes**
on the same rail rather than indented children — exactly as thinking already flows into
the `●` message.

## Domain vs. presentation

The TUI already consumes the shared `content` package (`content.AIMessage`,
`content.ThinkingBlock`, `content.TextBlock`, `content.ToolUseBlock`,
`content.ToolResultBlock`, `content.Chunk`, …) and `harness/pkg/event` for live events.
It adds a thin presentation layer: the internal `entry`/`Kind` model, `ToolCallView`,
`DisplayProjection`, and `collapseState`. **This change lives entirely in that
presentation layer.** No domain type changes — which is what keeps it SOLID-clean and
low-risk.

## Visual specification

### Glyph vocabulary

| Node | Glyph | Rule |
|------|-------|------|
| Thinking | `│ ` (rail head, no circle) | reasoning block, collapsible |
| AI message | `●` neon (`styles.LitDot` / `DotColor`) | rendered **only when text content is present** |
| Tool call | `○` faint / `○` red / `◍` pulsing | status-derived tint |
| Subagent (open + close) | `○` faint / `○` red / `◍` pulsing | status-derived tint |
| Detail / connector rows | `│ ` gutter | tool results, nested steps; existing collapse rules |

- **Continuous rail:** the `│` is drawn on every connector row between nodes — the turn
  reads as one unbroken timeline. A node row replaces the `│` with its glyph; a
  detail row uses a `│ ` gutter.
- **Status tint** (tools and subagents): `ok` → faint; `failed` → red; `running` → `◍`
  pulsing (reuse the status-line pulse cadence).

### Full turn (expanded)

```
│  thought for 3s
│
●  I'll read the config and run the tests.
│
○  Read(config.go)
│    42 lines
│
○  Subagent(explore) "find the retry logic"
│  ○  Read(backoff.go)
│  │    12 lines
│  ○  Grep("retry")
│  │    3 matches
│  ○  done · 4 steps — "found in backoff.go"
│
○  Bash(go test ./...)
│    ok  0.3s
```

### Node-presence rules ("show only what arrived")

An assistant step renders exactly the nodes it contains, in order:

- Has thinking → thinking rail head.
- Has text content → `●` AI-message node.
- Has tool calls → `○` tool nodes.

No content text ⇒ **no `●` node** and **no `● Multiple actions` umbrella**. A thinking +
tools step renders the thinking rail then the tool nodes directly. A tools-only step
renders just the tool nodes. `renderPromotedTool` and the `Multiple actions` headline
path are **removed**.

### Subagents (nested rail)

A subagent is an `○` node that **opens a nested secondary rail**: the outer `│` continues
as the subagent's spine, and its own tool calls sit on a second column (`│  ○`), details
in the nested `│  │ ` gutter. The subagent's terminal summary is itself the **closing
`○` node** of the nested rail — `○ done · N steps — "<summary>"` (faint), or
`○ failed · N steps — "<error>"` (red). Every point on a rail carries a node glyph; no
line hangs off the rail without one.

Failure example:

```
○  Subagent(explore) "find the retry logic"     ← red
│  ○  Read(backoff.go)
│  ○  Bash(build.sh)                             ← red
│  │    exit 1
│  ○  failed · 2 steps — "build error"           ← red
```

The existing `+M nested subagent steps` fold (depth-2 activity) is preserved as a nested
gutter detail.

### Collapse / expand

Reuse the existing `collapseState` (`globalCollapsed` default true, per-entry `overrides`,
`ToggleAll` on `ctrl+t`, `Toggle(id)` on header click). **The tool-list fold shares
thinking's fold** — one unified state.

- **At rest (folded):** a completed step's tool list collapses to a single summary node
  `○ N tools · Name, Name, Name` on the rail. Only FAILED calls are marked `✗` (successes,
  including subagents, are unmarked — a consistent rule); the node is tinted red if any
  failed. A collapsed subagent appears by its agent name, e.g.
  `○ 3 tools · Read, Subagent(explore), Bash` (or `Subagent(explore) ✗` on failure).
- **Expanded:** click the `○ N tools` node (`Toggle(id)`) or `ctrl+t` (`ToggleAll`) reveals
  the full rail. Same affordance as thinking today.
- **Active step stays expanded:** the in-flight/streaming step renders its full rail so the
  user watches it work; it folds to the summary node on completion.

### Live tail parity

The live-streaming tail and the committed/scrollback path must render **identically** —
the same `│`/`●`/`○`/`◍` rail. Both are updated in this change so an in-flight turn matches
its committed form.

## Affected code (view layer)

- `tui/render.go` — rail gutter primitive; `renderAssistant`, `renderToolCard`,
  `renderToolCalls`, `renderSubagentCard`; remove `renderPromotedTool` + `Multiple actions`
  umbrella; replace `cardConnector`/`cardIndent`/`resultIndent` tool-child layout with
  node-on-rail rendering; nested-rail rendering for subagents.
- `tui/entryrender.go` — `renderEntry` kind dispatch; collapsed summary node; continuous
  rail across `kindAssistant`/`kindTool` entry boundaries (own the inter-node `│` connector
  rows, suppress the blank inter-entry gap within a turn).
- `tui/collapse.go` — extend the shared fold to govern tool-list collapse (no new state).
- `tui/styles/styles.go` — status-tinted hollow-circle glyphs (`○` faint/red, `◍` pulsing);
  nested-rail gutter styles.
- Live-tail rendering path (spill / `liveRunning`) — parity with committed rail.

## Testing

Per CLAUDE.md, table-driven `-race` tests covering, at minimum:

- Node-presence matrix: thinking-only, content-only, tools-only, thinking+tools,
  thinking+content+tools, empty step.
- Tool status tint: ok / failed / running for a tool node and a summary node.
- Collapse: folded summary (count + names, `✗` marking, red tint on any failure);
  expanded rail; `ToggleAll` and per-entry `Toggle` parity with thinking.
- Subagent: nested rail rendering, closing `○ done`/`○ failed` node, `+M nested` fold,
  collapsed subagent summary tuck-in.
- Continuous-rail invariant: no connector row without a `│`, no node row with a stray `│`.
- Live-tail vs. committed parity: same turn renders identically through both paths.
- Boundary/edge: nil blocks, empty tool list, unknown block types (safe placeholder),
  width truncation of long summary node lists.

Golden/snapshot fixtures (existing `fixtures_test.go` / `displayprojection_test.go` style)
updated to the new layout.

## Non-goals

- No changes to `content` domain types or `harness/pkg/event`.
- No new keybinding — expand reuses `ctrl+t` + click.
- No change to user-message (`▌`) or notice (`▌`) rendering.
