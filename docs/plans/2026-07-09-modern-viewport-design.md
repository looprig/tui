# Modern Viewport TUI (`--modern`) — Design

Date: 2026-07-09 · Status: **authoritative spec for the follow-up impl plan** · Rev 2 (fable review folded in)

## Goal

Add an opt-in `--modern` TUI mode: a full-screen, app-owned **viewport** that the user can
scroll and select/copy from *at the same time*, with **collapsible** thinking/tool blocks and
a bottom **active-loops list** you can click to focus a subagent loop and view its whole live
stream — and, once a small harness seam lands, **message any loop** exactly like the primary.

This is the deliberate opposite of the default **scrollback-first** mode
(`2026-06-15-tui-scrollback-first-design.md`), which the June design explicitly reserved room
for: *"A future fullscreen viewport mode may exist behind an explicit flag."* This is that mode.
It ships **alongside** scrollback (which stays the untouched default); if it proves out in daily
use it later becomes the default.

### Why this is now feasible (the two things that changed)

1. **Copy while scrolling — proven.** The `cmd/modernspike` spike showed bubbletea v2 can own an
   alt-screen viewport, scroll on the wheel, AND copy a drag-selection — because v2 makes
   `MouseMode` a **per-frame `View` field**, and because copy can go to the local clipboard
   binary (`pbcopy`) which works in Apple Terminal and tmux regardless of terminal OSC-52
   support. The June premise ("mouse ON breaks native copy, full stop") no longer holds.
2. **Per-loop identity is in the event stream** — every event carries `Header.LoopID`, and
   `transcriptModel` already tracks `LoopStarted → agentName`. Owning the buffer lets us
   interleave and retroactively expand what append-only scrollback deliberately could not.

### The load-bearing correction from review (read this first)

The TUI does **not** currently *see* subagents' live output. `DefaultEventFilter`
(`tui/agent.go:86`) delivers **Ephemeral** events (TokenDelta + tool lifecycle) from the
**primary loop only**; other loops surface only at Enduring granularity (StepDone, gates,
terminals). Therefore, to view a subagent's *whole live stream*, modern mode must **subscribe
with an all-loops Ephemeral scope** — see §Subscription. This is a **CLI-only** change
(`Agent.Subscribe(filter)` is already parameterized and `LoopScope{All:true}` exists), so it is
**Stage 1**, not Stage 2. This one decision drives the projection model, the loop bar, and focus.

## Subscription strategy (decided)

Modern mode opens **one** whole-session subscription at startup and **never re-subscribes**:

```go
event.EventFilter{
    Ephemeral: event.LoopScope{All: true}, // every loop's live tokens + tool spinners
    Enduring:  event.LoopScope{All: true}, // StepDone, gates, terminals — already all-loops
}
```

Rationale: the hub buffer is bounded and has **no replay** (`session.go`), so re-subscribing on
focus-change would drop events in the gap. A single all-loops subscription from startup means
focus is a **pure view filter** over already-received, already-reconstructed state — switching
loops never touches the transport. The accepted cost is higher event volume than the
primary-only default; modern mode wants exactly that (it renders every loop). Scrollback mode
keeps `DefaultEventFilter` unchanged.

## Scope

**Stage 1 — CLI-only, this module, no harness change, ships first:**

1. `--modern` selection at the composition root (`cli/run.go`) via a backward-compatible
   `RunOption` + `LOOPRIG_MODERN` env fallback (runnable without any external entry-point edit).
2. A shared **`sessionCore`** (transport + model update) extracted from `Screen` and embedded by
   both `Screen` and the new `ModernScreen`, so event routing cannot drift between modes.
3. All-loops subscription (above) + **per-loop projections** in the transcript layer.
4. `ModernScreen`: alt-screen + mouse viewport; hand-rolled scrollable/selectable region;
   retroactive collapse/expand; active-loops bar; focus.
5. Prompts / composer / status rendered inside the viewport frame; composer submits to the
   **primary** loop (Stage 1), auto-refocusing primary on submit.

**Stage 2 — needs the harness seam (documented, not executed in the Stage-1 pass):**

6. Public `Submit(ctx, loopID, blocks)` on `Session` (a one-method exposure of the existing
   `submitToLoop`) + on `tui.Agent`; composer routes to `focusedLoopID` → *message any loop.*
   Not executed now because it crosses the harness module and requires a re-vendor.

**Out of scope:** changing the scrollback default; the `AskUser` contract; any loop/session
semantics beyond exposing `submitToLoop`.

## Loop lifecycle & liveness (corrected)

- The only loop-lifecycle events are **`LoopStarted`** and **`LoopIdle`** (both Enduring,
  loop-scoped → already delivered). **There is no loop-exited event.** Subagent loops are
  **never deleted**; after handing back a result they **park idle** on their `CommandSink` and
  remain messageable (this is why Stage-2 messaging works for "finished" subagents).
- So loop liveness is **bi-state: live | idle** (a loop is live between `LoopStarted`/turn
  activity and `LoopIdle`). There is no "done"/"exited" state to render.
- `DoneChan` closes only at **session** shutdown/fault — not per subagent. Do not use it for
  per-loop liveness.
- Because loops accumulate for the whole session, the bar applies a **visible cap** (§Loop bar).

## Architecture — shared transport core, two presentation shells

Public boundary unchanged: `cli.Run` is the composition root; `tui.Agent`, `tui.OpenAgent`,
`tui.AgentBanner` stay.

| File | Action | Owns |
|---|---|---|
| `tui/sessioncore.go` | **new (refactor)** | `sessionCore`: agent wiring, the single session subscription + its lifecycle (stale/nil guards), event dispatch into `transcript` + `interaction`, turn-status, `/clear` reopen ordering, submit/interrupt/mapAction. **No presentation.** Extracted from `Screen` behind `Screen`'s existing green tests. |
| `tui/screen.go` | modify | Embeds `sessionCore`; keeps ONLY scrollback presentation (`scrollbackModel` + `surface.go`). Its tests are the refactor's regression guard. |
| `tui/modern.go` | **new** | `ModernScreen`: embeds `sessionCore`; owns viewport/selection/collapse/focus state; `View()` returns alt-screen + mouse. |
| `tui/viewport.go` | **new** | `viewportModel`: hand-rolled scroll region — offset, wheel/key scroll, app-drawn selection (cell-based), `SelectedText()`. Not `bubbles/viewport` (selection/collapse overlay must be ours). |
| `tui/loopbar.go` | **new** | `loopBar`: renders the active-loops list from the transcript's loop table + liveness; `HitTest(x)`→`loopID`; cycle order; visible cap. |
| `tui/clipboard.go` | **new** | `copyCmd(text)`: `tea.SetClipboard` (OSC 52 via the program writer) **and** a stdlib `os/exec` local write (`pbcopy`/`xclip`/`wl-copy`). Errors surfaced, never `_`-swallowed. |
| `tui/transcript.go` | extend | Per-loop projections (below); a **single** shared `nextID` allocator; a liveness bit on the existing loop table; a spawn→loopID reverse index for optional card-click focus. |
| `tui/render.go`, `tui/entryrender.go` | extend | Renderers additionally emit, per line, a **provenance + plaintext** record (below). Existing string output unchanged for scrollback. |
| `tui/interaction.go`, `components/input.go`, `tui/prompt.go` | reuse | Composer, slash, prompt FIFO, per-mode key routing — reused as-is (no Screen/agent coupling). |
| `tui/scrollback.go`, `tui/surface.go`, `tui/tips.go` | untouched | Scrollback path only; not used by `ModernScreen`. |
| `cli/run.go` | modify | `RunOption` + env selects `NewModern` vs `New`; teardown reads `Agent()` via a small `agentHolder` interface both models satisfy. |

**Why a shared `sessionCore` (revised from "duplicate the glue"):** the transport glue is *not*
small — subscription lifecycle, `/clear` reopen ordering, status derivation, submit/mapAction —
and two copies **will** drift (a new event type handled in one mode only). One core, two
presentation shells, is the correct factoring and also relieves `Screen`'s current SRP overload.
The refactor is sequenced first and gated on `Screen`'s existing tests staying green.

## Per-loop projections

```go
// transcript.go (additive)
type loopProjection struct {
    committed []entry      // this loop's finalized rows
    live      liveSegment  // this loop's in-progress output
}
type transcriptModel struct {
    // ...existing fields unchanged (the folded root view scrollback depends on)...
    nextID      displayID                        // SINGLE allocator — globally unique IDs
    projections map[uuid.UUID]*loopProjection     // non-primary loops only (see alias rule)
}
```

Rules (each a distinct review fix):

- **Primary is an alias, not a copy.** `projection(primaryLoopID)` returns a view backed by the
  *existing* `committed`/`live` fold — it is **not** re-folded. Only **non-primary** loops get a
  separate `loopProjection`. This avoids double-folding, double allocation, and duplicate IDs,
  and keeps scrollback mode zero-**read**-cost. Projections are folded unconditionally (a small
  additive write per non-primary loop); scrollback simply never *reads* them. This is deliberate
  over gating on a build flag — a flag a later task forgot to set would silently empty every
  focused-loop view; the write cost is negligible.
- **Globally-unique IDs.** All entries (every projection) draw from the one `nextID`, so the
  ModernScreen-level `collapsed map[displayID]bool` can key on `displayID` without cross-loop
  collision.
- **Non-primary reconstruction uses `storedStepToolCard` only** — never the primary
  live-card path (`stepToolCard` consults `m.live.Calls`, which would steal same-index primary
  cards; see the §3a comment in `transcript.go`). Each projection scopes its own user/task-row
  rule to its own `LoopID`.
- **Zero-LoopID guard.** Session-scoped events (SessionStarted/Idle/…) have a zero `LoopID`;
  they are handled at the model level and are **never** routed into a projection.

`ModernScreen.focusedLoopID` (default `agent.PrimaryLoopID()`) selects which projection the
viewport renders.

## Renderer provenance contract (new — powers selection, copy, collapse-click, stable scroll)

Styled transcript lines are saturated with glamour/lipgloss ANSI and may contain wide runes, so
mouse-cell math and clipboard text cannot operate on the styled string. The viewport renderer
therefore produces, per visible line, a record:

```go
type renderedLine struct {
    styled string     // what is drawn (ANSI)
    plain  string     // ANSI-free text — the ONLY thing selection extracts / copies
    entry  displayID  // provenance: which entry this line belongs to
    sub    int        // intra-entry line index (for stable anchoring + header-click)
}
```

- **Cell↔rune mapping** uses `lipgloss.Width` (already-approved dep) — mouse X is a **cell**
  column; map it to a rune index by accumulating `lipgloss.Width` over `plain`'s rune prefixes
  (handles CJK/emoji = 2 cells; no ANSI in `plain`). Highlight is applied by re-styling the
  `plain` slice, never by splicing into `styled` (which would corrupt escapes).
- **Copy** joins the selected `plain` spans → no escape sequences ever reach the clipboard.
- **Collapse-click** hit-tests a click row → `renderedLine.entry` → toggle that entry.
- **Stable anchoring** (below) uses `(entry, sub)` rather than a raw content-row int.

## Viewport, selection & copy

`viewportModel` renders the focused projection into `[]renderedLine`, shows the window
`[offset : offset+height]`, and:

- **Scroll:** wheel ±3; **PageUp/PageDown/Home/End** (non-printable — no composer conflict);
  offset clamped to content. Line-scroll uses the wheel or Page keys, **not** `↑/↓/g/G` (those
  belong to the composer/prompts — §Keys).
- **Auto-follow:** if pinned at the tail, new content keeps it pinned (live streaming stays
  visible); scrolling up detaches; returning to bottom re-pins. "At tail" is tracked by a
  boolean the scroll handlers set, **not** recomputed from line counts (which reflow).
- **Selection is anchored to `(entry displayID, sub, cellCol)`**, not `offset+Y`, so it survives
  reflow (StepDone snap, collapse toggle, width change). During an **active drag the content
  buffer is frozen** (rendered once at mouse-down, not re-rendered until release) — the simplest
  robust guard against mid-drag height changes.
- **Copy** on release → `copyCmd(SelectedText())`. `copyCmd` runs `tea.SetClipboard(text)`
  (OSC 52 through the program's writer — reaches the terminal even though process stdout is
  redirected to the log) **and** a stdlib `os/exec` write to the platform clipboard
  (`pbcopy` on darwin — the path proven in Apple Terminal + tmux). A copy error surfaces a
  transient status notice; it is not swallowed.
- **Transient `copied N chars`** clears via a `tea.Tick` (~2s), not left stuck.

## Collapse / expand (retroactive)

Per-entry state `collapsed map[displayID]bool`:

- `ctrl+t` toggles a **global** default; because the buffer re-renders each frame, it is
  **retroactive** (the June "must default expanded" constraint does not apply here). **Default
  collapsed** since expansion is now reversible and density is the goal.
- **Click a block header** (hit-test → `renderedLine.entry`) toggles just that entry. A collapsed
  block shows a `▸`/`▾` affordance.

> **Stage-1 scope (implemented):** the collapse state (`collapseState`, Task 6) drives the
> **thinking** fold via `renderEntryLines(collapsed)` — thinking starts folded and `ctrl+t`/click
> expand it. **Tool-result expansion is deferred:** the shared `renderEntry` hard-caps tool
> previews at `previewLineCap` regardless of the expand flag (a deliberate scrollback-safety rule
> — a huge tool result must not strand a screen-height gap), and un-capping it in modern mode
> without regressing scrollback needs an additive renderer change out of Task 6's scope. So in
> Stage 1 tool results stay capped in both modes; the toggle visibly affects thinking, which is
> the user's stated need. A follow-up can add an `expandTools` renderer path for modern mode.

## Active-loops bar (replaces tips) + focus

Derived from a **single source** — the transcript's existing loop table (`loopAgents`) plus a
liveness bit set by `LoopStarted`/`LoopIdle`/turn activity — **not** a second registry. One row
pinned where tips were:

```
loops  ▸main·a1b2  •reviewer·c3d4!  ∘tester·e5f6   … +3           copied 42 chars
       └focused     └live +gate      └idle          └visible cap
```

- `▸` focused · `•` live · `∘` idle · `!` a pending gate on that loop. `<id4>` short id, agent
  name first. **Visible cap** with a `… +N` overflow marker (most-recent / live-first ordering);
  the bar never grows unbounded.
- **Focus is the bar's job** (it has real `loopID`s): **click** a segment → `HitTest(x)` →
  `focusedLoopID = loopID` → viewport re-renders that projection, offset reset to tail. Optional:
  clicking an inline Subagent *card* focuses it too — this needs a `LoopID` on the reconciled
  card via the new spawn→loopID reverse index; if that plumbing slips, the bar remains the
  canonical focus path.
- **Keyboard focus cycle:** `ctrl+n` / `ctrl+p` (NOT `ctrl+[`/`ctrl+]` — `ctrl+[` is byte-identical
  to Esc). Focusing never interrupts a loop; it only changes the view.
- Tips removed in modern mode (`tips.go` stays for scrollback).

## Prompts, composer, status, key routing

- **Prompts** reuse `interactionModel` + `prompt.go` wholesale. Gate dispatch already carries
  `loopID` (`Approve`/`Deny`/`ProvideAnswer`), so a gate from any loop resolves correctly today.
  The bottom box shows the head gate (global FIFO). **Prompt-open does NOT steal focus** (it must
  not interrupt reading or a drag); instead the bar marks the gated loop with `!`. Focus is the
  user's to change.
- **Composer** (`components/input.go`), slash, draft save/restore — reused. **Stage 1:** `Enter`
  submits to the **primary** loop and **auto-refocuses primary**, so the user's message is
  visibly committed (never lands in an unseen projection). **Stage 2:** submits to `focusedLoopID`.
- **Status line** shows the **focused** loop's activity (derived from its projection's live/turn
  state), above the bar.
- **Key-routing precedence** (explicit): (1) active prompt mode consumes its keys; (2) the
  composer consumes printable input and its editing/`↑↓` keys; (3) the viewport consumes only
  non-conflicting navigation — **wheel, PageUp/PageDown/Home/End**, and the focus chords
  `ctrl+n`/`ctrl+p`, `ctrl+t`. Globals (`ctrl+c`, `esc`) keep their existing meaning.

## `View()` and mode selection

```go
// ModernScreen.View — alt-screen + mouse are per-frame View fields (v2)
v := tea.NewView(body)         // body = viewport + status + loop bar + bottom box
v.AltScreen = true
v.MouseMode = tea.MouseModeCellMotion
return v
```

```go
// cli/run.go — backward-compatible
func Run(ctx, newAgent, banner Banner, opts ...RunOption) int   // variadic: existing callers unbroken
// WithModern(true) OR env LOOPRIG_MODERN=1 → tui.NewModern(...), else tui.New(...)
// teardown: final.(agentHolder).Agent()   // both Screen and ModernScreen satisfy agentHolder
```

## Feature-parity checklist (Stage 1 must not silently drop)

`/clear`, interrupt (`esc`/`ctrl+c`), queued input during a turn, restore backlog repaint
(`ReplayBacklog`), image `@path` handling, slash commands, greeting/banner entries. Each is
owned by `sessionCore` (so both modes share it) or explicitly ported; the plan has a task per
item, and any deferral is logged, never silent.

## Testing

Table-driven, `go test -race ./...`, synthetic events (no live loop), matching existing `tui/`
tests.

- **`sessionCore` refactor** — `Screen`'s existing suite stays green (the regression gate);
  add core-level tests for subscription lifecycle, `/clear` reopen ordering, event dispatch.
- **per-loop projections** — non-primary events build that loop's own entries via
  `storedStepToolCard`; primary projection aliases the existing fold (no duplicate IDs/entries);
  zero-LoopID events never route to a projection; single `nextID` yields globally-unique IDs.
- **loop table + liveness** — `LoopStarted`→live+name; `LoopIdle`→idle; bi-state only; visible
  cap + overflow marker; single source (no duplicate registry).
- **`viewportModel`** — scroll clamps both ends; wheel/Page deltas; auto-follow pins/detaches via
  the flag (not line-count); selection anchored to `(entry,sub,cell)` survives a reflow; frozen
  buffer during drag; empty selection copies nothing.
- **renderer provenance** — each `renderedLine` carries correct `plain`/`entry`/`sub`;
  cell↔rune mapping via `lipgloss.Width` handles wide runes; `plain` is ANSI-free; highlight
  re-styles `plain`, never splices `styled`.
- **`copyCmd`** — `tea.SetClipboard` invoked; `os/exec` local write invoked (seam faked);
  error surfaces a notice, not `_`.
- **`loopBar`** — marks focused/live/idle/gate; `HitTest(x)`→right loopID; cycle order.
- **collapse** — default collapsed; retroactive `ctrl+t`; per-entry header-click via provenance.
- **`ModernScreen`** — focus swap re-renders the focused projection; region hit-testing (content
  vs bar); prompt-open marks the bar, does not steal focus; submit auto-refocuses primary;
  key-routing precedence; parity items via `sessionCore`.
- **`cli/run.go`** — `WithModern`/env selects `NewModern`; `agentHolder` teardown for both models;
  default builds `Screen`.

**Manual smoke:** `LOOPRIG_MODERN=1 <entry>` in Apple Terminal and tmux — wheel scroll;
drag-select + paste (no escapes, wide runes aligned); `ctrl+t` collapse/expand; spawn a subagent,
see it in the bar, click it, watch its **live** stream; a gated tool marks the bar and resolves.

## Suggested execution order (each a TDD task; `-race` throughout)

0. Extract `sessionCore` from `Screen` (behavior-preserving; `Screen` tests stay green).
1. `copyCmd` (`tea.SetClipboard` + `os/exec` local; errors surfaced) + tests.
2. Renderer provenance contract (`renderedLine`: styled/plain/entry/sub; `lipgloss.Width` map) + tests.
3. `viewportModel` (scroll + auto-follow flag + `(entry,sub,cell)` selection + frozen-during-drag) + tests.
4. All-loops subscription for modern mode + per-loop projections (primary alias; `storedStepToolCard`; zero-LoopID guard; single `nextID`) + tests.
5. Loop table liveness (bi-state) + `loopBar` (render + `HitTest` + cap + cycle) + tests.
6. Collapse/expand (default collapsed; retroactive `ctrl+t`; header-click via provenance) + tests.
7. `ModernScreen` shell: embed `sessionCore`, `View` (alt-screen+mouse), wire viewport/scroll/select, key-routing precedence + tests.
8. Focus (bar click + `ctrl+n`/`ctrl+p`) swapping the rendered projection; status = focused loop + tests.
9. Prompts/composer/status inside the frame; prompt-open marks bar (no steal); submit auto-refocus primary; parity (clear/interrupt/queued/restore/images/slash) + tests.
10. `cli/run.go` mode selection (`WithModern` + env) + `agentHolder` teardown + tests.
11. `-race` build (`CGO_ENABLED=0 go build -trimpath`), `make secure`, manual smoke.

**Stage 2 (separate pass, after Stage 1 lands):** harness `Submit(ctx, loopID, blocks)` +
re-vendor; `tui.Agent` gains it; composer routes to `focusedLoopID`. Documented; not executed
now (crosses the harness module).

## Appendix — review findings → resolutions

- B1 selection on ANSI/wide lines → §Renderer provenance (`plain` + `lipgloss.Width`).
- B2 OSC 52 to redirected stdout → `tea.SetClipboard` (program writer) + `os/exec` local; §clipboard.
- B3 subagent Ephemeral filtered out / mis-staged → §Subscription (all-loops, Stage 1).
- B4 liveness/`DoneChan`/no loop-exit → §Loop lifecycle (bi-state; idle-but-messageable; cap).
- S5 primary "clickable kindSubagent" wrong model → bar is canonical focus; card-click optional via spawn→loopID index.
- S6 double-fold → primary projection is an alias; non-primary only.
- S7 displayID collision → single `nextID`.
- S8 §3a card-steal → `storedStepToolCard` for non-primary; per-projection user-row rule.
- S9 key conflicts / `ctrl+[`==Esc → §Keys precedence; Page/Home/End + `ctrl+n`/`ctrl+p`.
- S10 stale anchors on reflow → `(entry,sub,cell)` anchoring + frozen-during-drag + provenance.
- S11 glue drift → shared `sessionCore`.
- S12 atotto direct/`_`-swallow → stdlib `os/exec` (no direct atotto dep) + surfaced errors.
- S13 submit-while-focused-elsewhere → auto-refocus primary on submit (Stage 1).
- S14 focus-on-prompt → no steal; bar `!` marker; status = focused loop.
- N15 zero-LoopID projection → guard. N16 duplicate registry → single loop table. N17 transient → `tea.Tick` clear.
