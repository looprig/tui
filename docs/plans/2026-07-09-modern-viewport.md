# Modern Viewport TUI (`--modern`) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add an opt-in `--modern` full-screen viewport TUI mode with simultaneous scroll+copy, retroactive collapse/expand of thinking/tool blocks, and a clickable active-loops bar to focus and view any loop's live stream.

**Architecture:** Extract a shared `sessionCore` (transport + model update) from the existing `Screen`; both `Screen` (scrollback) and a new `ModernScreen` (viewport) embed it. Modern mode subscribes to all loops' events, builds per-loop projections in `transcriptModel`, and renders a hand-rolled scrollable/selectable viewport with a bottom loop bar. Stage 1 (this plan) is CLI-only; Stage 2 (messaging any loop) is a documented follow-up needing a harness change.

**Tech Stack:** Go, bubbletea v2 (`charm.land/bubbletea/v2`), lipgloss v2, `github.com/looprig/harness/pkg/event`, stdlib `os/exec`.

**REQUIRED READING for every task:** `docs/plans/2026-07-09-modern-viewport-design.md` (the authoritative spec — Rev 2). Each subagent MUST read it plus the specific existing files named in its task before writing code. Follow CLAUDE.md: table-driven `-race` tests, typed errors, gofmt-clean, stdlib-first, no new deps.

**Global rules:**
- Every task is TDD: failing test → run/see it fail → minimal impl → run/see it pass → `gofmt` → commit.
- All tests run with `-race`. Never weaken an existing test to pass; if `Screen`'s tests break during Task 0, the refactor is wrong — fix the refactor.
- Do NOT touch `tui/scrollback.go`, `tui/surface.go`, `tui/tips.go` except where a task says so.
- Commit after each task with a `feat:`/`refactor:`/`test:` message ending in the Co-Authored-By trailer.

---

### Task 0: Extract `sessionCore` from `Screen` (behavior-preserving refactor)

**Why:** Both modes must share the transport (subscription lifecycle, event dispatch to transcript+interaction, turn-status, `/clear` reopen, submit/interrupt/mapAction) so routing cannot drift. `Screen`'s existing tests are the regression gate.

**Files:**
- Create: `tui/sessioncore.go`
- Modify: `tui/screen.go` (embed `sessionCore`; keep only scrollback presentation)
- Test: existing `tui/screen_test.go` MUST stay green; add `tui/sessioncore_test.go`

**Step 1 — Read first:** `tui/screen.go` (all of it), `tui/interaction.go`, `tui/transcript.go` (types + `ApplyEvent`), `tui/commands.go`, `tui/agent.go`.

**Step 2 — Identify the seam.** `sessionCore` owns these current `Screen` fields/behaviors (move, don't rewrite): `agent`, `openAgent`, `appCtx`, the single subscription (`sub`/`subNext` + stale/nil guards), `transcript transcriptModel`, `interaction interactionModel`, turn status (`applyTurnStatus`), `/clear` reopen ordering, and the submit/interrupt/mapAction command wiring. It exposes methods `Screen` and `ModernScreen` both call, e.g.:

```go
// tui/sessioncore.go
type sessionCore struct {
    agent     Agent
    openAgent OpenAgent
    appCtx    context.Context
    banner    AgentBanner
    sub       EventStream
    transcript  transcriptModel
    interaction interactionModel
    status      sessionStatus // whatever Screen uses today
}

// handleEvent applies an event to transcript+interaction and returns any follow-up cmd.
// (Presentation — scrollback flush vs viewport render — stays in the embedding model.)
func (c *sessionCore) handleEvent(ev event.Event) (sessionCore, tea.Cmd) { ... }
// subNext, submit, interrupt, reopen (/clear), mapAction move here unchanged in behavior.
```

`Screen` becomes `struct { sessionCore; scrollback scrollbackModel; heldLines ...; /* scrollback-only presentation */ }` and its `Update` delegates transport to the core, then does its scrollback flush; its `View` is unchanged.

**Step 3 — TDD:** This is a refactor, so the failing test is a NEW `sessionCore` unit test asserting the moved behavior directly (e.g. an event routes into `core.transcript`; `/clear` closes the old sub before swapping agent). Write it against the not-yet-existing `sessionCore`, watch it fail to compile, then extract.

**Step 4 — Regression gate:** `go test -race ./tui/ -run TestScreen` and the full `go test -race ./tui/` MUST pass unchanged. If any `Screen` test needs editing beyond mechanical field-access renames, STOP — the extraction changed behavior.

**Step 5 — Commit:** `refactor: extract sessionCore transport from Screen`

---

### Task 1: `copyCmd` clipboard primitive

**Files:** Create `tui/clipboard.go`, `tui/clipboard_test.go`.

**Step 1 — Read:** `cmd/modernspike/main.go` (the OSC 52 + tmux-wrap logic), `vendor/charm.land/bubbletea/v2/clipboard.go` (`tea.SetClipboard`), `cli/run.go:162-183` (why raw stdout is redirected — do NOT write OSC 52 to `os.Stdout`).

**Step 2 — Failing test** (`tui/clipboard_test.go`): table-driven over `localClipboardArgv(goos string) []string` — `darwin`→`["pbcopy"]`, `linux`+`$WAYLAND_DISPLAY`→`["wl-copy"]`, `linux`→`["xclip","-selection","clipboard"]`, unknown→nil. And a test that `copyCmd("hi")` returns a non-nil `tea.Cmd` and that the injected exec seam receives `"hi"` on stdin and that a non-zero exit surfaces a typed `clipboardError` (not `_`-swallowed).

**Step 3 — Implement:**
```go
// copyCmd sets the system clipboard two ways: tea.SetClipboard (OSC 52 through the
// program's own writer, so it reaches the terminal despite the stdout->log redirect)
// and a local clipboard binary via os/exec (works in Apple Terminal + tmux, which
// ignore OSC 52). Errors are surfaced as clipboardCopiedMsg{err}, never swallowed.
func copyCmd(text string) tea.Cmd {
    return tea.Batch(
        tea.SetClipboard(text),
        func() tea.Msg { return clipboardCopiedMsg{n: len([]rune(text)), err: writeLocalClipboard(text)} },
    )
}
```
`writeLocalClipboard` selects argv via `localClipboardArgv(runtime.GOOS)`, `exec.Command(argv[0], argv[1:]...)`, pipes `text` to stdin (no shell string — safe). Define typed `clipboardError`. Make the exec runner a package var seam for the test.

**Step 4:** `go test -race ./tui/ -run Clipboard` → PASS. **Step 5:** commit `feat: add copyCmd clipboard primitive`.

---

### Task 2: Renderer provenance contract (`renderedLine`)

**Files:** Modify `tui/render.go` (or new `tui/rendered.go`), `tui/entryrender.go`; Test `tui/rendered_test.go`.

**Step 1 — Read:** `tui/render.go`, `tui/entryrender.go` (how entries → `[]string` today).

**Step 2 — Define:**
```go
type renderedLine struct {
    styled string    // drawn (ANSI)
    plain  string    // ANSI-free; the ONLY text selection extracts/copies
    entry  displayID  // provenance
    sub    int        // intra-entry line index
}
```
Add a function `renderEntryLines(e entry, width int, collapsed bool) []renderedLine` that wraps the existing entry renderer and, per output line, records `plain` (build from the source content, NOT by stripping — the renderer has the plain text before styling) and provenance.

**Step 3 — Failing tests:** a thinking entry yields lines whose `plain` has no ESC bytes (`!strings.ContainsRune(plain, 0x1b)`); `entry` matches the source id; `sub` increments 0..n; a wide-rune line's `lipgloss.Width(plain)` equals the intended cell width; collapsed vs expanded produce different line counts.

**Step 4 — Implement** minimally, reusing existing renderers for `styled`. **Step 5:** commit `feat: renderer provenance (plain text + ids) for selection`.

---

### Task 3: `viewportModel` (scroll + selection + copy)

**Files:** Create `tui/viewport.go`, `tui/viewport_test.go`.

**Step 1 — Read:** `cmd/modernspike/main.go` (scroll + drag-select + highlight math), design §Viewport, Task 2's `renderedLine`.

**Step 2 — Type:**
```go
type viewportModel struct {
    lines   []renderedLine
    offset  int
    height  int
    atTail  bool          // auto-follow flag (NOT recomputed from counts)
    sel     *selection    // nil when none; anchored to (entry,sub,cell)
    frozen  []renderedLine // snapshot taken at mouse-down; used until release
}
type selection struct{ anchor, cursor selPoint }
type selPoint struct{ entry displayID; sub, cell int }
```

**Step 3 — Failing tests (table-driven):**
- `scrollBy` clamps at 0 and at `max(0,len-height)`; wheel ±3; Page keys ± (height-1); Home/End.
- `atTail`: appending lines while `atTail` keeps offset pinned to bottom; a scroll-up sets `atTail=false`; scrolling back to bottom sets it true.
- `SelectedText`: same-row substring by **cell** columns; multi-row first/mid/last; empty selection (anchor==cursor) → `""`; a line containing a wide rune selects the right runes (map cell→rune via `lipgloss.Width`).
- during a drag the model reads from `frozen`, so mutating `lines` mid-drag does not move the selection.

**Step 4 — Implement:** cell↔rune mapping helper `cellToRune(plain string, cell int) int` accumulating `lipgloss.Width` over rune prefixes; highlight by re-styling the `plain` slice with `lipgloss.NewStyle().Reverse(true)`; never splice `styled`. Mouse handlers: `MouseWheelMsg`→scroll; `MouseClickMsg`→snapshot `frozen`, set anchor; `MouseMotionMsg`(button held)→cursor; `MouseReleaseMsg`→keep selection, return `copyCmd(SelectedText())`.

**Step 5:** `go test -race ./tui/ -run Viewport` PASS. Commit `feat: viewportModel scroll+select+copy`.

---

### Task 4: All-loops subscription + per-loop projections

**Files:** Modify `tui/transcript.go`; Modify `tui/agent.go` (add an all-loops filter helper); Test `tui/transcript_test.go` (add cases), `tui/agent_test.go`.

**Step 1 — Read:** `tui/transcript.go` (esp. `ApplyEvent`, `stepDone`/`stepToolCard`/`storedStepToolCard`, the §3a comment, `startTurnUser`, `loopAgents`, `nextID`), `tui/agent.go` (`DefaultEventFilter`), `vendor/github.com/looprig/harness/pkg/event/filter.go` (`LoopScope{All:true}`).

**Step 2 — Add `AllLoopsEventFilter()`** in `agent.go`: same as `DefaultEventFilter` but `Ephemeral: event.LoopScope{All: true}`. Test: its `Ephemeral.Matches(anyID)==true`.

**Step 3 — Projections.** Add `projections map[uuid.UUID]*loopProjection` + rules from design §Per-loop projections:
- `projectionFor(loopID)`: for `primaryLoopID` return an alias backed by existing `committed`/`live` (do NOT re-fold); else the map entry (create on first non-primary event).
- In `ApplyEvent`, after the existing folded reconstruction, if `ev.Header.LoopID != zero && != primaryLoopID`, route into that projection using `storedStepToolCard` exclusively and the projection's own user/task-row rule.
- Zero-LoopID events never route to a projection.
- All entries use the single `nextID`.

**Step 4 — Failing tests:** a subagent StepDone builds that loop's projection entries (not the root); the primary projection is the existing fold (same entries/ids — no duplication); a zero-LoopID event touches no projection; ids are globally unique across projections; a concurrent primary live card is NOT stolen into a subagent projection (§3a guard).

**Step 5:** commit `feat: all-loops subscription + per-loop projections`.

---

### Task 5: Loop liveness + `loopBar`

**Files:** Modify `tui/transcript.go` (liveness bit on the loop table); Create `tui/loopbar.go`, `tui/loopbar_test.go`.

**Step 1 — Read:** design §Loop lifecycle + §Active-loops bar; `event/event.go` `LoopStarted`/`LoopIdle`; the existing `loopAgents` table.

**Step 2 — Liveness:** extend the loop table entry with `live bool` (and keep `agentName`, creation order). `LoopStarted`/turn-start → live; `LoopIdle` → idle. Bi-state only (no "done"). Single source of truth — the bar reads this table, no separate registry.

**Step 3 — `loopBar`:**
```go
type loopBar struct { entries []loopBarEntry; focused uuid.UUID; cap int }
type loopBarEntry struct{ id uuid.UUID; name string; live, gate bool }
func (b loopBar) Render(width int) string          // ▸focused •live ∘idle !gate, "… +N" overflow
func (b loopBar) HitTest(x int) (uuid.UUID, bool)  // click column -> loopID
func (b loopBar) cycle(dir int) uuid.UUID          // ctrl+n / ctrl+p order
```

**Step 4 — Failing tests (table-driven):** render marks focused/live/idle/gate correctly; visible cap truncates with `… +N`; `HitTest` maps x-ranges to the right ids and returns false past the last segment; `cycle` wraps forward/back over the visible order.

**Step 5:** commit `feat: loop liveness + loopBar (render/hit-test/cycle)`.

---

### Task 6: Collapse / expand

**Files:** Create `tui/collapse.go` (state + helpers) or fold into `viewport.go`; Test `tui/collapse_test.go`.

**Step 1 — Read:** design §Collapse; Task 2 provenance; `render.go` thinking/tool collapse rendering (`previewLineCap`).

**Step 2 — State:** `collapsed map[displayID]bool` + a `globalCollapsed bool` default **true**. Effective state per entry = per-entry override if present else global. `ctrl+t` flips `globalCollapsed` and clears per-entry overrides (retroactive). A header-click hit-test (`renderedLine.entry`) sets a per-entry override.

**Step 3 — Failing tests:** default renders thinking/tool collapsed; `ctrl+t` expands all (retroactive — an already-built entry now renders expanded); a header-click toggles exactly one entry; effective-state precedence (override > global).

**Step 4 — Implement.** **Step 5:** commit `feat: retroactive collapse/expand of thinking+tool blocks`.

---

### Task 7: `ModernScreen` shell

**Files:** Create `tui/modern.go`, `tui/modern_test.go`.

**Step 1 — Read:** `tui/screen.go` (Init/Update/View shape), Task 0 `sessionCore`, Tasks 3/5/6 components, design §View + §Key routing.

**Step 2 — Type:** `ModernScreen struct { sessionCore; viewport viewportModel; bar loopBar; collapse collapseState; focusedLoopID uuid.UUID; term dims }`. Implements `tea.Model` and `agentHolder` (`Agent() Agent`).

**Step 3 — `View()`:** compose `body` = viewport render (focused projection, with collapse) + status line (focused loop) + loop bar + bottom box (composer/prompt); return `tea.View{AltScreen:true, MouseMode:tea.MouseModeCellMotion}`.

**Step 4 — `Update()`:** delegate transport to `sessionCore.handleEvent`; then route input by precedence (design §Key routing): prompt mode → composer printable/editing → viewport nav (wheel, Page/Home/End, `ctrl+t`, `ctrl+n`/`ctrl+p`). Mouse region hit-test: content rows → viewport select/scroll + header-click collapse; bar row → `HitTest`→focus.

**Step 5 — Failing tests:** an event routed through Update updates `sessionCore.transcript`; `View()` returns `AltScreen==true && MouseMode==CellMotion`; a wheel msg scrolls; a bar click changes `focusedLoopID`; key precedence (a printable char goes to composer, not viewport). **Step 6:** commit `feat: ModernScreen viewport shell`.

---

### Task 8: Focus swapping

**Files:** Modify `tui/modern.go`; Test `tui/modern_test.go`.

**Step 1:** Focus via bar click (Task 7) and `ctrl+n`/`ctrl+p` (`bar.cycle`). On focus change: `viewport.lines = renderProjection(focusedLoopID)`, reset offset to tail (`atTail=true`), clear selection.

**Step 2 — Failing tests:** focusing loop B renders B's projection; `ctrl+n`/`ctrl+p` move focus in bar order; focusing never emits a submit/interrupt cmd (view-only); status line reflects the focused loop. **Step 3:** commit `feat: focus swap between loops in modern mode`.

---

### Task 9: Prompts, composer, status, parity

**Files:** Modify `tui/modern.go`; Test `tui/modern_test.go`.

**Step 1 — Read:** `tui/interaction.go`, `tui/prompt.go`, `tui/commands.go`; design §Prompts/composer/status.

**Step 2 — Wire:** bottom box renders the interaction model (composer/prompt) unchanged; gate keys dispatch via existing `Approve`/`Deny`/`ProvideAnswer` (carry `loopID`). Prompt-open marks the bar (`gate=true` for that loop) and does NOT change `focusedLoopID`. `Enter` submits to primary and sets `focusedLoopID=primary` (auto-refocus). Status = focused loop's live/turn state.

**Step 3 — Parity tests (table-driven, reuse `sessionCore`):** `/clear` reopens (sub closed→agent swapped→resubscribe, all-loops filter); interrupt via `esc`/`ctrl+c`; queued input during a turn; restore backlog repaint (`ReplayBacklog`) populates projections; image `@path` handling; slash completion. Prompt-open bar-mark + no-focus-steal; submit auto-refocus.

**Step 4:** commit `feat: prompts/composer/status + parity in modern mode`.

---

### Task 10: `cli/run.go` mode selection + `agentHolder`

**Files:** Modify `cli/run.go`; Test `cli/run_test.go`.

**Step 1 — Read:** all of `cli/run.go` (esp. `newProgram`, `tui.New`, teardown `final.(tui.Screen)`).

**Step 2 — Add:**
```go
type RunOption func(*runConfig)
func WithModern(on bool) RunOption
// modern = on OR os.Getenv("LOOPRIG_MODERN") != ""
type agentHolder interface{ Agent() Agent }   // in tui; both models satisfy it
```
Select `screen := tui.NewModern(...)` when modern else `tui.New(...)`. Replace `final.(tui.Screen)` teardown with `final.(agentHolder)`.

**Step 3 — Failing tests:** `WithModern(true)` (and `LOOPRIG_MODERN=1` via a seam) build the modern model; default builds `Screen`; teardown resolves `Agent()` for both; existing `Run` callers still compile (variadic). **Step 4:** commit `feat: --modern mode selection in cli.Run`.

---

### Task 11: Integration verification

**Steps:**
1. `gofmt -l $(go list -f '{{.Dir}}' ./...)` → empty.
2. `CGO_ENABLED=0 go build -trimpath ./...` → succeeds.
3. `go test -race ./...` → all pass.
4. `make test-integration` → all pass (process-boundary tests behind `//go:build integration`, e.g. the clipboard exec seam).
5. `make secure` → clean (lint + vet + staticcheck + gosec + govulncheck).
6. Manual smoke (document results in the commit): `LOOPRIG_MODERN=1 <entry>` in Apple Terminal + tmux — wheel scroll; drag-select+paste (no escapes, wide runes aligned); `ctrl+t`; spawn a subagent, click it in the bar, watch its live stream; a gated tool marks the bar and resolves.
7. Commit `chore: modern viewport Stage 1 verification`.

**Stage 2 (separate plan, needs harness change):** expose `Session.Submit(ctx, loopID, blocks)` + re-vendor; add to `tui.Agent`; route composer to `focusedLoopID`.
