# Modern Viewport TUI (`--modern`) — Design

Date: 2026-07-09 · Status: **authoritative spec for the follow-up impl plan**

## Goal

Add an opt-in `--modern` TUI mode: a full-screen, app-owned **viewport** that the user can
scroll and select/copy from *at the same time*, with **collapsible** thinking/tool blocks and
a bottom **active-loops list** you can click to focus a subagent loop and view its whole
stream — and, once a small harness seam lands, **message any loop** exactly like the primary.

This is the deliberate opposite of the default **scrollback-first** mode
(`2026-06-15-tui-scrollback-first-design.md`), which the June design explicitly left room for:
*"A future fullscreen viewport mode may exist behind an explicit flag."* This is that mode.
If it proves out in daily use it becomes the default; until then it ships **alongside**
scrollback, which stays untouched and remains the default.

### Why this is now feasible (the two things that changed)

1. **Copy while scrolling — proven.** The `cmd/modernspike` spike showed bubbletea v2 can own
   an alt-screen viewport, scroll on the wheel, AND copy a drag-selection — because v2 makes
   `MouseMode` a **per-frame `View` field**, and because the copy path shells to the local
   `pbcopy` (via the CLAUDE.md-approved `github.com/atotto/clipboard`) plus a tmux-wrapped
   OSC 52, so it lands in Apple Terminal and tmux without relying on terminal OSC-52 support.
   The June premise ("mouse ON breaks native copy, full stop") no longer holds.
2. **Per-loop everything already exists in the event stream.** Every event carries
   `Header.LoopID`; `transcriptModel` already tracks `LoopStarted → agentName` and accumulates
   each subagent's nested stream (`subagentAccum`). Scrollback mode *deliberately deferred*
   showing subagent streams interleaved ("Option A, deferred") **because append-only scrollback
   cannot interleave or retroactively expand.** Owning the buffer removes exactly that
   constraint.

### Feasibility of "message any loop" (confirmed, and why it stages cleanly)

`Session.Submit` calls an internal `submitToLoop(ctx, loopID, blocks, agency)` that looks up
**any** loop via `loopFor(loopID)` (no primary-only guard) and delivers a `command.UserInput`
into that loop's `CommandSink()` — the identical path subagents already receive their task on.
`Session.Submit` merely hardcodes `primaryLoopID`. So messaging a live subagent is mechanically
identical to messaging the primary; it needs only a public loop-targeted submit exposed on the
`tui.Agent` interface. That is a **harness change** (a different module + re-vendor), so it is
isolated as **Stage 2**. Everything else is CLI-only and ships as **Stage 1**.

**Caveat (accepted):** you can message/live-watch only **live** loops. A finished subagent
(`DoneChan` closed) is view-only history — which maps naturally onto the active-loops list.

## Scope

**Stage 1 — CLI-only, this module, no harness change, ships first:**

1. `--modern` mode selection at the composition root (`cli/run.go`), with an env fallback so
   it is runnable without touching any external entry point.
2. A sibling `ModernScreen` model: alt-screen + mouse viewport, reusing `transcriptModel`,
   `interactionModel`, `render.go`, and `commands.go`. `Screen` (scrollback) is untouched.
3. **Per-loop projections** in the transcript layer: each loop's own committed + live entries,
   built from the already-attributed events.
4. Hand-rolled scrollable/selectable viewport (scroll + app-drawn selection + dual-clipboard
   copy, generalized from the spike over real transcript entries).
5. **Retroactive collapse/expand** of thinking and tool blocks (per-entry state + `ctrl+t`
   global toggle + click-to-toggle a block header).
6. **Active-loops bar** replacing the tips row: `loopID · agent · live/idle`, **click to
   focus**, keyboard-cycle. Focus swaps which projection the viewport renders.
7. Prompts (permission / AskUser), composer, status line rendered inside the viewport frame.
   Composer submits to the **primary** loop (Stage 1).

**Stage 2 — needs the harness seam (documented, not executed in the Stage-1 pass):**

8. Public `Submit(ctx, loopID, blocks)` on `Session` + `tui.Agent`; broaden the Ephemeral
   subscription filter to the focused loop; route the composer to the **focused** loop →
   *message any loop, no difference from primary.*

**Out of scope:** changing the scrollback default; the `AskUser` contract; any agent/loop/
session semantics beyond exposing the existing `submitToLoop`.

## Architecture — a sibling model, maximal reuse

The public boundary is unchanged: `cli.Run` is the composition root; `tui.Agent`,
`tui.OpenAgent`, `tui.AgentBanner` stay. A new model is added beside `Screen`.

| File | Action | Owns |
|---|---|---|
| `cli/run.go` | modify | Select `NewModern` vs `New` by mode (option + `LOOPRIG_MODERN` env); teardown reads `Agent()` via a small `agentHolder` interface both models satisfy. |
| `tui/modern.go` | **new** | `ModernScreen`: `Init`/`Update`/`View` for viewport mode. Reuses the agent wiring + single subscription pattern from `Screen`, routes events to `transcript`+`interaction`, owns focus/selection/collapse/viewport state. |
| `tui/viewport.go` | **new** | `viewportModel`: a hand-rolled scroll region over rendered lines — offset, wheel/key scroll, app-drawn selection range, `SelectedText()`. (Hand-rolled, not `bubbles/viewport`, so selection highlight and collapse overlay are ours to control — same choice the spike validated.) |
| `tui/loopbar.go` | **new** | `loopBar`: renders the active-loops list from the loop registry; hit-tests a mouse click to a `loopID`; keyboard cycle order. |
| `tui/clipboard.go` | **new** | `copyToClipboard(text)`: `clipboard.WriteAll` + tmux-wrapped OSC 52 (lifted verbatim from the spike; the one shared copy primitive). |
| `tui/transcript.go` | extend | Add per-loop projections: `projections map[uuid.UUID]*loopProjection`; `ApplyEvent` also routes each event into its `Header.LoopID` projection. Existing folded/root behavior stays for scrollback mode. |
| `tui/loops.go` | **new** | `loopRegistry`: `LoopStarted`→add(live, agentName); `LoopIdle`/terminals→mark idle/done; ordered for the bar. Derived purely from events. |
| `tui/render.go` | reuse | Entry / segment / tool-card / thinking / prompt renderers are consumed by the viewport renderer unchanged. |
| `tui/interaction.go`, `components/input.go` | reuse | Composer, slash, prompt FIFO, per-mode key routing — all reused as-is. |
| `tui/scrollback.go`, `tui/surface.go`, `tui/tips.go` | untouched | Not used by `ModernScreen`; remain the scrollback path. |

**Why a sibling model, not a mode flag on `Screen` (rejected alternative):** the default
scrollback path is proven and must not regress. A sibling isolates all new behavior; the
shared, *pure* pieces (`transcriptModel`, `interactionModel`, `render.go`, `commands.go`) are
reused by composition. The small duplicated glue (subscription plumbing, event dispatch) is
worth the isolation.

## Per-loop projections

```go
// transcript.go (additive)
type loopProjection struct {
    committed []entry      // this loop's finalized rows
    live      liveSegment  // this loop's in-progress output
    agentName string       // attribution label (from LoopStarted)
}

type transcriptModel struct {
    // ...existing fields (folded root view for scrollback) unchanged...
    projections map[uuid.UUID]*loopProjection // keyed by Header.LoopID
    order       []uuid.UUID                    // creation order, for the bar
}
```

`ApplyEvent(ev)` keeps its existing folded-root reconstruction (scrollback mode depends on it)
**and** additionally routes `ev` into `projections[ev.Header.LoopID]` using the same
entry-building helpers (`splitStepGroup`, the live-segment accumulation). The **primary loop's**
projection still shows subagent activity as **collapsed, now-clickable** reference lines
(`kindSubagent`), which bridge to that subagent's own projection when focused. A subagent
projection shows that loop's stream in full.

`ModernScreen.focusedLoopID` selects which projection the viewport renders; it defaults to
`agent.PrimaryLoopID()`.

## Viewport, selection & copy

`viewportModel` renders the focused projection's entries (via `render.go`) into a
`[]string`, then shows the window `lines[offset : offset+height]`. Ported verbatim from the
validated spike:

- **Scroll:** `MouseWheelMsg` ±3; `↑/↓`, `pgup/pgdown`, `g`/`G`. Offset clamped to content.
- **Select:** `MouseClickMsg`→anchor (content coords = `offset+Y`), `MouseMotionMsg` (button
  held)→cursor, `MouseReleaseMsg`→extract + copy. Highlight is app-drawn (`lipgloss` reverse);
  columns indexed by `[]rune`; multi-row rule first/middle/last as in the spike.
- **Copy:** `copyToClipboard` (`clipboard.WriteAll` + tmux-wrapped OSC 52). Confirmation shown
  transiently in the status/bar area (`copied N chars`).
- **Auto-follow:** when the offset is at the bottom, new content keeps it pinned to the tail
  (live streaming stays visible); scrolling up detaches follow until the user returns to
  bottom.

Mouse coordinates are hit-tested by region: content rows → selection; the bottom loop-bar row
→ focus click (below).

## Collapse / expand (retroactive — the payoff of owning the buffer)

Per-entry collapse state, keyed by `displayID`:

```go
collapsed map[displayID]bool
```

- `ctrl+t` toggles a **global** default (all thinking + tool blocks) — and because the whole
  buffer re-renders each frame from the model, the toggle is **retroactive**, unlike
  scrollback. This removes the June "default expanded" constraint.
- **Default collapsed** for thinking (compact summary) and tool results (folded to
  `previewLineCap`), since expansion is now reversible and density is the point. A collapsed
  block renders a `▸`/`▾` affordance; **clicking its header row** (mouse hit-test to the
  entry) toggles just that entry.

## Active-loops bar (replaces tips) + focus

`loopRegistry` is derived from events (no new API): `LoopStarted` adds `{loopID, agentName,
live}`; `LoopIdle` and turn terminals update liveness. `loopBar` renders one row pinned at the
bottom (where tips were):

```
loops:  ▸ main·<id8>  •reviewer·<id8>  ∘tester·<id8>          copied 42 chars
        └ focused      └ live            └ idle
```

- `▸` marks the **focused** loop; `•` live, `∘` idle. `<id8>` is the loop id short form; the
  agent name precedes it.
- **Click** a row segment → `loopBar.HitTest(x)` → `focusedLoopID = that loop` → viewport
  re-renders that projection, offset reset to tail. Clicking is possible because modern mode
  captures the mouse.
- **Keyboard:** `ctrl+]`/`ctrl+[` cycle focus forward/back through `order`; focusing a loop
  never interrupts it — it only changes the view.
- Tips are removed in modern mode (the bar occupies that row). `tips.go` stays for scrollback.

## Prompts, composer, status

Reused wholesale from `interactionModel`:

- Permission / AskUser prompts render as the bottom box (same `prompt.go` view-models); gate
  dispatch already carries `loopID` (`Approve`/`Deny`/`ProvideAnswer`), so a prompt from **any**
  loop resolves correctly today. A prompt from a non-focused loop surfaces a marker in the bar
  and can focus-on-open (design choice: focus the prompting loop when a gate opens).
- Composer (auto-growing `components/input.go`), slash completion, draft save/restore — all
  reused. **Stage 1:** `Enter` submits to the primary loop (`agent.Submit`). **Stage 2:**
  submits to `focusedLoopID`.
- Status line (`RenderStatusLine`) renders above the bar.

## `View()` and mode selection

```go
// ModernScreen.View
v := tea.NewView(body)      // body = viewport + status + loop bar + bottom box
v.AltScreen = true
v.MouseMode = tea.MouseModeCellMotion
return v
```

```go
// cli/run.go — backward-compatible selection
func Run(ctx, newAgent, banner Banner, opts ...RunOption) int   // variadic: no break to callers
// WithModern(true) OR env LOOPRIG_MODERN=1 → screen = tui.NewModern(...), else tui.New(...)
// teardown: final.(agentHolder).Agent()   // both Screen and ModernScreen satisfy it
```

An external entry point wires `--modern` → `WithModern(true)`; the env var makes it runnable
for testing without any external edit.

## Feature-parity checklist (Stage 1 must not silently drop)

`/clear` (reopen agent), interrupt (`esc`/`ctrl+c`), queued input while a turn runs, restore
backlog repaint (`ReplayBacklog`), image `@path` handling, slash commands, greeting/banner
entries. Each is reused from the existing model or explicitly ported; the plan has a task per
item, and any intentionally-deferred item is logged, never dropped silently.

## Testing

Table-driven, `go test -race ./...`, synthetic events (no live loop), matching existing `tui/`
tests. Grouped by unit:

- **per-loop projections** — an event routes into its `LoopID` projection; a subagent's
  StepDone builds that loop's own entries; the primary projection keeps clickable collapsed
  subagent lines; terminal events reset the right projection's live segment.
- **`loopRegistry`** — `LoopStarted`→live+name; `LoopIdle`/terminal→idle/done; order stable.
- **`viewportModel`** — scroll clamps at both ends; wheel/key deltas; auto-follow pins at
  tail and detaches on scroll-up; selection extraction (same-row, multi-row, rune columns,
  empty selection copies nothing); highlight range math.
- **`loopBar`** — render marks focused/live/idle; `HitTest(x)` maps to the right `loopID`;
  keyboard cycle order.
- **collapse** — default collapsed; `ctrl+t` toggles global and is retroactive; per-entry
  click toggle; collapsed vs expanded render for thinking + tool.
- **`ModernScreen`** — event routing updates transcript+interaction; focus swap re-renders the
  focused projection; mouse region hit-testing (content vs bar); prompt-open focuses the
  prompting loop; `/clear`, interrupt, queued input, restore repaint behave as in `Screen`.
- **`cli/run.go`** — `WithModern`/env selects `NewModern`; teardown resolves `Agent()` via
  `agentHolder` for both models; default (no flag) still builds `Screen`.
- **`copyToClipboard`** — OSC 52 base64 + tmux DCS wrap when `$TMUX` set; `clipboard.WriteAll`
  invoked (seam faked).

**Manual smoke** (post-impl, cannot be unit-tested): `LOOPRIG_MODERN=1 <entry>` in Apple
Terminal and tmux — wheel scroll; drag-select + paste; `ctrl+t` collapse/expand; run an agent
that spawns a subagent, confirm it appears in the bar, click it, watch its stream; a gated tool
prompts and resolves.

## Suggested execution order (for the impl plan — each a TDD task)

1. `copyToClipboard` (port spike copy primitive) + tests.
2. `viewportModel` (scroll + selection + auto-follow) + tests.
3. Per-loop projections in `transcript.go` + tests.
4. `loopRegistry` + tests.
5. `loopBar` (render + hit-test + cycle) + tests.
6. Collapse/expand state + retroactive `ctrl+t` + per-entry click + tests.
7. `ModernScreen` skeleton: subscription, event routing to transcript+interaction, `View`
   (alt-screen + mouse), scroll/select wired + tests.
8. Focus (bar click + keyboard) swapping the rendered projection + tests.
9. Prompts + composer + status inside the viewport; parity items (clear/interrupt/queued/
   restore/images/slash) + tests.
10. `cli/run.go` mode selection (`WithModern` + env) + `agentHolder` teardown + tests.
11. `-race` build (`CGO_ENABLED=0 go build -trimpath`), `make secure`, manual smoke.

**Stage 2 (separate, after Stage 1 lands):** harness `Submit(ctx, loopID, blocks)` + vendor;
`tui.Agent` gains it; broaden Ephemeral filter to the focused loop; composer routes to
`focusedLoopID`. Documented here; not executed in the Stage-1 pass because it crosses the
harness module and requires a re-vendor.
