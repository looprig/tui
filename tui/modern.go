package tui

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"

	"github.com/looprig/cli/tui/styles"
)

// ModernScreen is the MODERN VIEWPORT presentation shell over the shared sessionCore
// transport (embedded). Where the scrollback-first Screen lets the terminal own history,
// ModernScreen owns an alt-screen VIEWPORT the user can scroll and select/copy from while
// content streams: it renders the FOCUSED loop's projection into a hand-rolled
// viewportModel (scroll + drag-select + copy), applies a RETROACTIVE collapse fold
// (ctrl+t + header-click), draws a bottom active-loops bar and the reused composer, and
// subscribes to EVERY loop's live stream (allLoopsFilter) so any focused subagent's tokens
// render live rather than freezing at Enduring StepDone granularity.
//
// The core owns event routing exactly as it does for Screen; ModernScreen adds ONLY the
// viewport presentation. Update delegates transport to the core then re-renders the focused
// projection into the viewport (keeping the auto-follow tail pinned); View composes, top to
// bottom, the viewport content, one status line, the loop bar, and the bottom box, and
// returns a per-frame View with AltScreen + cell-motion mouse (the v2 fields the copy-while-
// scrolling design turns on). Agent() is promoted from the embedded sessionCore, so
// ModernScreen satisfies the composition root's agentHolder through that single definition.
//
// Focus switching (Task 8): ctrl+n / ctrl+p cycle focus over the bar's loops and a bar-region
// click focuses the clicked loop, both repointing focusedLoopID and re-rendering that loop's
// projection (focusLoop). Focus is VIEW-ONLY — it never submits/interrupts a loop.
//
// Prompts + feature parity (Task 9): prompt keys route to the reused interaction model
// (handleKey precedence (2)) and the bottom box renders the head gate via the shared surface;
// a pending gate marks its loop with "!" in the bar WITHOUT stealing focus (bar() reads
// pendingGateLoops). A composer submit targets the FOCUSED loop (routeToInteraction — Stage 2:
// submitting while focused on a subagent runs a new turn on THAT loop and stays focused there;
// on the primary it is the ordinary primary submit).
// A cold-restore session repaints its history: Init batches restoreBacklogCmd and handleRestored
// folds the backlog into the transcript + projections + loop table and re-renders. The remaining
// parity items (/clear reopen, esc/ctrl+c interrupt, queued input, image @path rejection) all
// flow through the shared sessionCore, so they are shared with Screen rather than re-implemented.
type ModernScreen struct {
	sessionCore

	viewport viewportModel // the scrollable/selectable content window
	collapse collapseState // the retroactive thinking-fold state (ctrl+t + header-click)

	// focusedLoopID selects which loop's projection the viewport renders
	// (projectionFor(focusedLoopID)). It defaults to the agent's primary loop and is
	// repointed by focusLoop on a bar click or a ctrl+n/ctrl+p focus chord (Task 8). The
	// whole viewport is a pure VIEW over already-received, already-projected state, so a
	// focus change is a re-render, never a re-subscribe.
	focusedLoopID uuid.UUID

	width, height int
	ready         bool // true once the first WindowSizeMsg sizes the frame

	// startupPending/startupCommitted coordinate the opening banner with the first sized
	// frame: systemReadyMsg may arrive before WindowSizeMsg, so the banner commit is
	// deferred until the frame has a width to render into.
	startupPending   bool
	startupCommitted bool

	// mouseDragging distinguishes a drag (a text selection) from a plain click (a
	// header/body collapse toggle): a MouseMotion with the button held sets it, and a
	// release with it UNSET is treated as a click. It is reset on each fresh press.
	mouseDragging bool

	// turnStartedAt records each running loop's current-turn start time — set from the
	// loop's TurnStarted event CreatedAt, deleted on its terminal (TurnDone/Failed/
	// Interrupted). The focused loop's entry drives the live status-line elapsed timer
	// (turnElapsed): elapsed is now - turnStartedAt[focused], measured from event/tick
	// timestamps, never a render-time wall clock.
	turnStartedAt map[uuid.UUID]time.Time

	// now is the timer's CLOCK: the latest tickMsg time (seeded from a TurnStarted's
	// CreatedAt so the first frame reads a sane elapsed before the first tick lands). The
	// status line reads it as "now" so elapsed advances only on the 1s tick, never from a
	// per-render time.Now().
	now time.Time

	// ticking guards the 1s tick chain so at most ONE is in flight: a turn becoming active
	// starts it (maybeStartTick); each tick reschedules only while a turn is still active
	// and otherwise stops, so the timer never ticks forever once the session goes idle.
	ticking bool
}

// modernTickInterval is the status-line timer's cadence: one tick per second while a turn
// is active, matching the whole-second granularity of formatElapsed.
const modernTickInterval = time.Second

// tickMsg is one 1s timer tick carrying the tick's own time — the clock the status-line
// elapsed timer advances on (never a render-time wall clock).
type tickMsg struct{ at time.Time }

// tickCmd schedules the next 1s timer tick. It is re-issued from handleTick only while a
// turn is active, so the chain self-terminates when the session goes idle.
func tickCmd() tea.Cmd {
	return tea.Tick(modernTickInterval, func(t time.Time) tea.Msg { return tickMsg{at: t} })
}

// modernLoopBarCap is the visible-cap the modern active-loops bar renders under: at most
// this many loop segments show, the rest folding into a "… +N" overflow marker so the bar
// never grows unbounded across a long session's accumulated loops.
const modernLoopBarCap = 8

// liveTailEntryID is the reserved provenance id every live-tail line carries. The live tail
// is NOT a committed entry, and the first committed entry allocates displayID 1, so the zero
// id can never collide with a committed entry: selection/copy still work by (entry, sub)
// identity, and a stray collapse-click on the live tail toggles an id no committed entry owns
// (a harmless no-op).
const liveTailEntryID displayID = 0

// NewModern constructs an idle ModernScreen driving agent, with open as the /clear thunk and
// banner the agent name/description shown as the opening info notice. It injects
// allLoopsFilter so the session subscription delivers EVERY loop's live Ephemeral stream
// (see AllLoopsEventFilter) — the modern mode renders any focused loop's whole live output.
// The viewport starts pinned to the tail (atTail) so streaming content auto-follows, the
// collapse state starts folded (dense; ctrl+t expands), and focus starts on the primary loop.
func NewModern(ctx context.Context, agent Agent, open OpenAgent, banner AgentBanner) ModernScreen {
	m := ModernScreen{
		sessionCore:   newSessionCore(ctx, agent, open, banner, allLoopsFilter),
		viewport:      viewportModel{atTail: true},
		collapse:      newCollapseState(),
		focusedLoopID: agent.PrimaryLoopID(),
		turnStartedAt: make(map[uuid.UUID]time.Time),
	}
	m.interaction = modernizeComposer(m.interaction)
	return m
}

// modernComposerMinLines is the modern composer's default visible height — two rows
// instead of the scrollback composer's one, so the input reads as a roomier panel while
// still auto-growing to maxInputLines.
const modernComposerMinLines = 2

// modernizeComposer applies the MODERN composer treatment to in's input box — the 2-line
// default height and the gray panel fill (styles.ModernPanelBg) — and returns the updated
// model. It runs at every site that installs a FRESH (default 1-line, background-free)
// interaction for a ModernScreen: initial construction (NewModern) and the cold-restore
// install (handleRestored replaces the whole interaction with a freshly-built one). The
// /clear reopen keeps the same input (ClearPrompts preserves it), so it needs no re-apply.
// Scrollback's Screen never calls this, so its composer stays byte-identical.
func modernizeComposer(in interactionModel) interactionModel {
	in.input.SetMinLines(modernComposerMinLines)
	in.input.SetBackground(styles.ModernPanelBg)
	return in
}

// Init focuses the composer (starting the cursor blink), schedules the opening banner
// (systemReadyMsg), schedules the cold-restore repaint, and attaches the session-lifetime
// ALL-LOOPS subscription (m.subscribe resolves the injected allLoopsFilter). restoreBacklogCmd
// folds a RESTORED session's historical Enduring backlog OFF the update loop and repaints it
// once (restoredMsg) before any live event drives the transcript — reusing the SAME shell-
// agnostic command Screen batches, so both modes fold restore identically. A NEW session's
// empty backlog makes it a no-op (the viewport comes up idle, driven by live events after the
// first Submit); a restored session comes up idle too, so there is no backlog/live overlap.
func (m ModernScreen) Init() tea.Cmd {
	return tea.Batch(
		m.interaction.input.Focus(),
		func() tea.Msg { return systemReadyMsg{} },
		restoreBacklogCmd(m.appCtx, m.agent, m.agent.PrimaryLoopID()),
		m.subscribe(),
	)
}

// Update advances the model. It is a value receiver so ModernScreen satisfies tea.Model;
// the mutating handlers take a pointer to the addressable receiver and Update returns the
// updated value. Note the two-statement pattern for the pointer-receiver handlers (cmd :=
// …; return m, cmd): a `return m, m.handle(...)` would evaluate the first result (the OLD
// m) before the handler mutates it, stranding the mutation.
func (m ModernScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		cmd := m.handleMouse(msg)
		return m, cmd
	case eventMsg:
		cmd := m.handleEventModern(msg.ev)
		return m, cmd
	case restoredMsg:
		cmd := m.handleRestored(msg)
		return m, cmd
	case subscribedMsg:
		cmd := m.handleSubscribed(msg)
		return m, cmd
	case subClosedMsg:
		cmd := m.handleSubClosed(msg)
		return m, cmd
	case submitResultMsg:
		cmd := m.handleSubmitResult(msg)
		return m, cmd
	case interruptResultMsg:
		cmd := m.handleInterruptResult(msg)
		return m, cmd
	case reopenResultMsg:
		cmd := m.handleReopenResult(msg)
		return m, cmd
	case promptResultMsg:
		cmd := m.handlePromptResult(msg)
		return m, cmd
	case systemReadyMsg:
		cmd := m.handleSystemReady()
		return m, cmd
	case tickMsg:
		cmd := m.handleTick(msg)
		return m, cmd
	}
	return m, nil
}

// handleResize stores the terminal dimensions, resizes the composer, and commits a deferred
// opening banner (if systemReadyMsg arrived first). A WIDTH change reflows every rendered
// line (they were laid out at the old width) and a deferred banner commit changed the buffer
// — either needs a full rerender; a PURE height change only needs a size sync (the lines are
// unchanged, so re-rendering the transcript is wasted work).
func (m ModernScreen) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	widthChanged := msg.Width != m.width
	m.width, m.height = msg.Width, msg.Height
	m.ready = true
	m.interaction.input.Resize(msg.Width)
	bannerCommitted := false
	if m.startupPending && !m.startupCommitted {
		m.commitStartup()
		bannerCommitted = true
	}
	if widthChanged || bannerCommitted {
		m.rerender()
	} else {
		m.resize()
	}
	return m, nil
}

// handleSystemReady commits the opening transcript once the frame has its first size. If
// systemReadyMsg arrives before WindowSizeMsg the commit is deferred (startupPending) so the
// banner renders in a real frame instead of at width 0.
func (m *ModernScreen) handleSystemReady() tea.Cmd {
	if m.startupCommitted {
		return nil
	}
	if !m.ready {
		m.startupPending = true
		return nil
	}
	m.commitStartup()
	m.rerender()
	return nil
}

// commitStartup commits the opening entries once per session: the startup banner (always)
// and the OPTIONAL greeting (only when banner.Greeting is non-blank). Both go through the
// plain info-notice path — they are rendered opening entries, NOT turns or commands (no
// Submit, no loop, never in the model's context). Unlike Screen's held-tail dance, the
// viewport simply renders these committed entries directly, so there is no startup surface
// to manage.
func (m *ModernScreen) commitStartup() {
	if m.startupCommitted {
		return
	}
	m.startupPending = false
	m.startupCommitted = true
	m.transcript = m.transcript.CommitNotice(noticeInfo, m.banner.bannerText())
	if greeting := strings.TrimSpace(m.banner.Greeting); greeting != "" {
		m.transcript = m.transcript.CommitNotice(noticeInfo, greeting)
	}
}

// handleEventModern delegates one subscription event to the shared core (which routes it
// through BOTH reducers, derives the primary turn status, and re-arms the reader) and then
// re-renders the FOCUSED projection into the viewport, keeping the auto-follow tail pinned.
// The turn-phase cue is unused in Stage 1 (a static status line; Task 9 threads the blink).
func (m *ModernScreen) handleEventModern(ev event.Event) tea.Cmd {
	rearm, _ := m.sessionCore.handleEvent(ev)
	m.trackTurnClock(ev)
	m.rerender()
	// A turn becoming active (re)starts the 1s status-line tick if none is in flight; the
	// tick self-terminates once every turn has ended (handleTick), so it never leaks past
	// an idle session.
	return tea.Batch(rearm, m.maybeStartTick())
}

// trackTurnClock maintains turnStartedAt from a loop's turn-lifecycle events: a TurnStarted
// records its start time (and seeds the clock so the first pre-tick frame reads a sane
// elapsed); any terminal (TurnDone/TurnFailed/TurnInterrupted) clears it. Both the primary
// loop's turn events (via the core) and every subagent's (via the all-loops stream) reach
// here, so every focusable loop's timer is tracked. Non-turn events are ignored.
func (m *ModernScreen) trackTurnClock(ev event.Event) {
	h := ev.EventHeader()
	switch ev.(type) {
	case event.TurnStarted:
		if m.turnStartedAt == nil {
			m.turnStartedAt = make(map[uuid.UUID]time.Time)
		}
		m.turnStartedAt[h.LoopID] = h.CreatedAt
		if m.now.Before(h.CreatedAt) {
			m.now = h.CreatedAt
		}
	case event.TurnDone, event.TurnFailed, event.TurnInterrupted:
		delete(m.turnStartedAt, h.LoopID)
	}
}

// maybeStartTick launches the 1s status-line tick chain, but only when a turn is active
// AND no tick is already in flight — so a second concurrent TurnStarted never spawns a
// second chain, and an idle session never ticks. Returns nil when a tick is unnecessary.
func (m *ModernScreen) maybeStartTick() tea.Cmd {
	if m.ticking || len(m.turnStartedAt) == 0 {
		return nil
	}
	m.ticking = true
	return tickCmd()
}

// handleTick advances the timer clock to the tick's time and reschedules the tick ONLY
// while a turn is still active, so the chain stops the moment the session goes idle. It
// does NOT re-render the viewport: the transcript is unchanged, and the status line (which
// carries the live elapsed) is recomposed by View every frame — so advancing the clock and
// letting View redraw is the whole update.
func (m *ModernScreen) handleTick(msg tickMsg) tea.Cmd {
	m.ticking = false
	if msg.at.After(m.now) {
		m.now = msg.at
	}
	return m.maybeStartTick()
}

// handleRestored applies the cold-restore backlog fold's result ONCE, MIRRORING Screen's
// handleRestored but repainting the VIEWPORT (rerender) instead of flushing scrollback. The
// backlog was folded OFF the update loop inside restoreBacklogCmd (FoldDisplay →
// transcript.ApplyEvent + interaction.ApplyEvent), so the arriving state already carries the
// rebuilt committed entries, per-loop projections, loop table, and pending gates — installing
// it and re-rendering repaints the whole history and populates loops()/collapse from it.
//
// On a non-nil err it commits a faint, NON-FATAL restore-error notice the viewport re-renders
// (the live stream is unaffected; history simply did not repaint). A NEW session folds to an
// EMPTY backlog and is a strict no-op: installing it would DISCARD commitStartup's banner (and
// optional greeting) and reset the transcript's displayID counter, so the live transcript is
// left untouched and a fresh session comes up idle exactly as today. For a RESTORED session it
// installs the rebuilt transcript + interaction wholesale (no per-event work here) and
// re-renders; the live Subscribe path attaches separately (handleSubscribed) and, since cold
// restore comes up idle, live events only follow a user Submit — no backlog/live overlap, no
// dedup. The startup flags are reset so a deferred banner does not later overwrite the counter.
//
// KEEP IN SYNC WITH Screen.handleRestored (tui/restore.go) — the restore install
// (transcript+interaction+startup-flag reset, empty→no-op, err→notice) must stay identical;
// only the presentation differs (rerender here vs flush there). An eventual shared
// sessionCore.applyRestored is a deferred follow-up; until then, a fix here must land in both.
func (m *ModernScreen) handleRestored(msg restoredMsg) tea.Cmd {
	if msg.err != nil {
		m.transcript = m.transcript.CommitError(msg.err)
		m.rerender()
		return nil
	}
	if len(msg.transcript.committed) == 0 {
		return nil
	}
	m.startupPending = false
	m.startupCommitted = false
	m.transcript = msg.transcript
	// The restore fold built a FRESH (default 1-line, background-free) interaction; re-apply
	// the modern composer treatment so a cold-restored session keeps the 2-line gray panel.
	m.interaction = modernizeComposer(msg.interaction)
	m.rerender()
	return nil
}

// handleSubscribed installs the session-lifetime subscription via the core and starts the
// reader. On error the core commits a fatal error entry the viewport re-renders; on success
// it returns subNext, the single reader driving every subsequent event.
func (m *ModernScreen) handleSubscribed(msg subscribedMsg) tea.Cmd {
	cmd, present := m.sessionCore.applySubscribed(msg)
	if present {
		m.rerender()
	}
	return cmd
}

// handleSubClosed reacts to the reader observing a closed channel. A nil err is an
// intentional Close (nothing to surface); a non-nil err (hub-forced loss) commits an error
// entry the viewport re-renders.
func (m *ModernScreen) handleSubClosed(msg subClosedMsg) tea.Cmd {
	if m.sessionCore.applySubClosed(msg) {
		m.rerender()
	}
	return nil
}

// handleSubmitResult surfaces a fire-and-forget Submit outcome via the core. Success commits
// nothing (the authoritative user row arrives from the loop's TurnStarted); a non-nil err
// commits a faint error entry the viewport re-renders.
func (m *ModernScreen) handleSubmitResult(msg submitResultMsg) tea.Cmd {
	if m.sessionCore.applySubmitResult(msg) {
		m.rerender()
	}
	return nil
}

// handleInterruptResult applies an Interrupt outcome via the core. On error it returns to
// Running with a faint error entry the viewport re-renders; on success it stays Interrupting
// until the loop's TurnInterrupted terminal lands Idle.
func (m *ModernScreen) handleInterruptResult(msg interruptResultMsg) tea.Cmd {
	if m.sessionCore.applyInterruptResult(msg) {
		m.rerender()
	}
	return nil
}

// handlePromptResult surfaces a bounded prompt-dispatch outcome via the core. A nil err is a
// silent success; a non-nil err commits a faint error entry the viewport re-renders.
func (m *ModernScreen) handlePromptResult(msg promptResultMsg) tea.Cmd {
	if m.sessionCore.applyPromptResult(msg) {
		m.rerender()
	}
	return nil
}

// handleReopenResult applies a /clear reopen outcome. The core owns the transport ordering —
// on error the old agent is kept and it returns Idle with an error entry the viewport
// re-renders; on success it swaps in the fresh agent, resets the shared transport, and
// re-subscribes with the INJECTED (all-loops) filter. ModernScreen then resets its own
// viewport/collapse/focus so no remnant of the old session survives the swap.
func (m *ModernScreen) handleReopenResult(msg reopenResultMsg) tea.Cmd {
	cmd, present := m.sessionCore.applyReopenResult(msg)
	if present {
		m.rerender()
		return cmd
	}
	// Successful reopen: reset the modern presentation to match the fresh session.
	m.focusedLoopID = m.agent.PrimaryLoopID()
	m.collapse = newCollapseState()
	m.viewport = viewportModel{atTail: true}
	m.startupPending = false
	m.startupCommitted = false
	m.rerender()
	return cmd
}

// handleKey routes a key press in ACTUAL execution order: (1) the GLOBAL chords ctrl+c
// (quit), ctrl+t (toggle the global collapse fold), and ctrl+n/ctrl+p (cycle focus over the
// bar's loops) fire first, even with a prompt open — focus/fold are pure VIEW state and the
// composer must never swallow those chords; (2) an active prompt consumes its approve/deny/
// choice/answer keys; (3) with no
// prompt, Esc interrupts a running turn; (4) the viewport consumes ONLY its non-conflicting
// nav keys (PageUp/PageDown/Home/End); (5) everything else — the arrow keys and printable
// input — falls through to the composer.
func (m ModernScreen) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Close the subscription best-effort so it does not leak past quit (a synchronous,
		// idempotent teardown), then close the agent (bounded async) and quit.
		if m.sub != nil {
			_ = m.sub.Close()
			m.sub = nil
		}
		return m, tea.Sequence(closeAgent(m.agent), tea.Quit)
	case "ctrl+t":
		// Retroactive global fold: because the viewport re-renders from committed each frame,
		// flipping the default re-folds the WHOLE buffer (design §Collapse). Fires even with a
		// prompt open — it is pure display state.
		m.collapse.ToggleAll()
		m.rerender()
		return m, nil
	case "ctrl+n":
		// Focus the NEXT loop in the bar's VISIBLE (creation) order, wrapping. bar().cycle
		// reuses the exact ordering Render/HitTest draw, so the keyboard cycle and a bar click
		// agree on which loop sits where. A single-loop session cycles back to the focused
		// loop, so focusLoop no-ops. It is VIEW-ONLY — focusLoop never submits/interrupts, so
		// the chord returns a nil command.
		m.focusLoop(m.bar().cycle(1))
		return m, nil
	case "ctrl+p":
		// Focus the PREVIOUS loop (bar().cycle(-1), wrapping); see ctrl+n. View-only.
		m.focusLoop(m.bar().cycle(-1))
		return m, nil
	}
	// Precedence (2): an active prompt consumes its own approve/deny/choice/answer keys.
	if m.activePrompt() != nil {
		return m.routeToInteraction(msg)
	}
	// Precedence (3): Esc with no prompt keeps its legacy meaning — interrupt a running turn
	// (a no-op when idle).
	if msg.String() == "esc" {
		return m, m.sessionCore.interruptRunning()
	}
	// Precedence (4): the viewport consumes ONLY PageUp/PageDown/Home/End (handleKey returns
	// false for every other key), so those scroll the content.
	if m.viewport.handleKey(msg) {
		return m, nil
	}
	// Precedence (5): everything else — the arrow keys and printable input — falls through to
	// the composer.
	return m.routeToInteraction(msg)
}

// routeToInteraction delegates a key to the interaction model and maps the resulting typed
// uiAction into the agent-driving command. When the action committed an out-of-band entry
// (e.g. /help, a submit build error) the rendered buffer changed, so the viewport re-renders
// to surface it. A plain composer edit leaves the transcript UNCHANGED — only the composer's
// auto-grow may have shrunk the content region — so it just syncs the viewport SIZE (resize),
// never re-rendering the transcript per keystroke. The editor's blink cmd is batched so the
// cursor keeps blinking in compose/answer modes.
//
// Submit goes to the FOCUSED loop (Stage 2): a uiSubmit is intercepted here and routed to the
// currently focused loop via the core's loop-targeted submitToLoop — a submit while focused on
// a subagent runs a NEW turn on THAT loop and STAYS focused there (no auto-refocus), while a
// submit on the primary behaves exactly like before (submitToLoop(primary) == the primary
// submit). Only uiSubmit is intercepted; every other action kind (approve/deny/answer/slash/
// edit/interrupt) still routes through the shared core's mapAction unchanged, so Screen's
// primary-loop submit path (mapAction → submit → Submit) is untouched.
func (m ModernScreen) routeToInteraction(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var action uiAction
	var blink tea.Cmd
	m.interaction, action, blink = m.interaction.Update(msg)
	var cmd tea.Cmd
	var present bool
	if action.Kind == uiSubmit {
		// Modern override: submit to the FOCUSED loop (not the primary) and do NOT refocus —
		// the user stays on the loop they are watching. Blocks are built the same way the
		// core's primary submit does (buildBlocks from action.Text), and a build error commits
		// the same faint error entry.
		cmd, present = m.sessionCore.submitToLoop(m.focusedLoopID, action.Text)
	} else {
		cmd, present = m.sessionCore.mapAction(action)
	}
	if present {
		m.rerender()
	} else {
		m.resize()
	}
	return m, tea.Batch(cmd, blink)
}

// handleMouse routes a mouse event by the region it falls in. The wheel scrolls the content
// wherever the pointer sits (the viewport reads only the wheel's direction), so it is routed
// unconditionally. A content-region click/drag/release drives the viewport's select/copy and
// (on a plain click) toggles the clicked entry's fold. A bar-region LEFT click focuses the
// loop whose segment covers the column (barMouse → focusLoop). The status/box regions have no
// mouse behavior (keys drive the composer/prompt).
func (m *ModernScreen) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if w, ok := msg.(tea.MouseWheelMsg); ok {
		return m.viewport.handleMouse(w)
	}
	mouse := msg.Mouse()
	switch m.regionAt(mouse.Y) {
	case regionContent:
		return m.contentMouse(msg, mouse)
	case regionBar:
		return m.barMouse(msg, mouse)
	default: // regionStatus, regionBox — keys, not the mouse, drive these.
		return nil
	}
}

// barMouse focuses the loop whose active-loops-bar segment covers a LEFT click's column
// (design §Active-loops bar: focus is the bar's job). It builds the SAME loopBar View draws,
// so the hit-tested column and the drawn segment agree, and maps the click to a loop id via
// HitTest. The bar is drawn from column 0 of its row (composeBody stacks it as a full row),
// so the global mouse column IS the bar-local column HitTest expects. A click on a gap, the
// "… +N" overflow marker, or past the last segment (HitTest false) is a no-op, as is a
// non-left click or motion/release (focus is a single click). It refuses to hit-test a bar
// that would render EMPTY (width <= 0) — HitTest's precondition: resolving a click against a
// row the user never saw would be wrong. It is VIEW-ONLY — focusLoop repoints focus and
// re-renders, never submitting/interrupting — so it always returns a nil command.
func (m *ModernScreen) barMouse(msg tea.MouseMsg, mouse tea.Mouse) tea.Cmd {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft || m.width <= 0 {
		return nil
	}
	if id, hit := m.bar().HitTest(mouse.X); hit {
		m.focusLoop(id)
	}
	return nil
}

// focusLoop repoints the viewport to loopID's projection (design §Active-loops bar + focus).
// It is a strict no-op when loopID already has focus (no needless re-render or tail reset).
// Otherwise it is VIEW-ONLY — it changes ONLY the view (focusedLoopID + the viewport), never
// the transcript and never a submit/interrupt/approve command: focus lets the user READ any
// loop's live stream, it must never message or interrupt one. On the swap it (1) clears any
// active selection — its (entry, sub) anchors belong to the OLD projection's buffer and are
// meaningless in the new one; (2) resets the viewport to the tail so the new loop's LATEST
// content shows; and (3) re-renders the new projection (rerender pins to the just-set tail).
// The whole viewport is a pure view over already-received, already-projected state, so this
// is a re-render, never a re-subscribe.
func (m *ModernScreen) focusLoop(loopID uuid.UUID) {
	if loopID == m.focusedLoopID {
		return
	}
	m.focusedLoopID = loopID
	m.viewport.clearSelection()
	m.viewport.resetToTail()
	m.rerender()
}

// contentMouse forwards a content-region mouse event to the viewport (which draws its own
// selection and returns a copy command on release) and implements the click-vs-drag collapse
// rule: a plain click (press+release with no intervening motion) on an entry's HEADER row
// toggles just that entry's fold, while a drag is a text selection and never toggles. The
// content region sits at row 0, so the global Y is already the viewport-local row.
func (m *ModernScreen) contentMouse(msg tea.MouseMsg, mouse tea.Mouse) tea.Cmd {
	switch msg.(type) {
	case tea.MouseClickMsg:
		if mouse.Button == tea.MouseLeft {
			m.mouseDragging = false // a fresh press; a following motion promotes it to a drag
		}
		return m.viewport.handleMouse(msg)
	case tea.MouseMotionMsg:
		if mouse.Button == tea.MouseLeft {
			m.mouseDragging = true
		}
		return m.viewport.handleMouse(msg)
	case tea.MouseReleaseMsg:
		cmd := m.viewport.handleMouse(msg) // finishes any drag; copyCmd for a real selection
		// A plain click (no motion) on an entry's HEADER line (sub == 0 — e.g. the
		// "│ thinking" / "thinking · N lines" header) toggles that entry's fold; a click on a
		// body row does NOT toggle (design: header-click). A drag is a selection, never a toggle.
		if !m.mouseDragging {
			if id, sub, ok := m.viewport.entryAt(mouse.Y); ok && sub == 0 {
				m.collapse.Toggle(id)
				m.rerender()
			}
		}
		return cmd
	}
	return nil
}

// modernLayout is the frame's top-to-bottom region geometry, the SINGLE source both View
// (which draws the regions) and regionAt (which hit-tests a mouse row) compute from, so a
// drawn row and a hit-tested row can never disagree. The content region occupies rows
// [0, contentH); the status line, loop bar, and bottom box follow.
type modernLayout struct {
	contentH int // viewport content rows: region [0, contentH)
	statusY  int // the status line row
	barY     int // the loop bar row
	boxTop   int // the first row of the bottom box
	boxH     int // the bottom box's rendered height
}

// layout derives the region geometry from the current frame: the bottom box is measured
// first (its height varies with the composer/prompt), then the status + bar reserve one row
// each, and the viewport content gets whatever remains (floored at 0). Because it is
// deterministic in the model state, View and regionAt compute the identical layout.
//
// NOT side-effect-free: measuring the box (bottomBoxView → the bubbles textarea's View)
// drives the textarea's internal render/measure cache, so layout must run only on Bubble
// Tea's single model goroutine (never concurrently — see the serial subtest note in the
// tests).
func (m ModernScreen) layout() modernLayout {
	const statusH, barH = 1, 1
	boxH := lipgloss.Height(m.bottomBoxView())
	if boxH < 1 {
		boxH = 1
	}
	contentH := m.height - statusH - barH - boxH
	if contentH < 0 {
		contentH = 0
	}
	return modernLayout{
		contentH: contentH,
		statusY:  contentH,
		barY:     contentH + statusH,
		boxTop:   contentH + statusH + barH,
		boxH:     boxH,
	}
}

// modernRegion is a frame region the mouse can fall in — the content viewport, the status
// line, the loop bar, or the bottom box — the discriminant handleMouse routes on.
type modernRegion uint8

const (
	regionContent modernRegion = iota
	regionStatus
	regionBar
	regionBox
)

// regionAt maps a terminal row y to its frame region using the same layout View draws, so
// mouse routing (content select/copy vs a bar focus click) matches what the user sees.
func (m ModernScreen) regionAt(y int) modernRegion {
	lay := m.layout()
	switch {
	case y < lay.contentH:
		return regionContent
	case y == lay.statusY:
		return regionStatus
	case y == lay.barY:
		return regionBar
	default:
		return regionBox
	}
}

// contentWidth is the column budget the focused projection renders to — the full frame
// width (the viewport draws no side gutter). A negative width (never in a real frame) floors
// to 0 so the renderers stay well-defined.
func (m ModernScreen) contentWidth() int {
	if m.width < 0 {
		return 0
	}
	return m.width
}

// resize and rerender are the two viewport-sync primitives, deliberately SPLIT so a
// composer keystroke does not re-render the whole transcript.
//
// resize syncs ONLY the viewport's size to the current layout's content region — it does
// NOT re-render the buffer. The composer auto-grows as the user types, which shrinks the
// content region, but the transcript LINES are unchanged, so a keystroke needs only this
// (SetSize re-clamps the offset and preserves the auto-follow tail). This keeps typing O(1)
// in the transcript length: renderFocused re-renders every committed entry through glamour
// (which builds a fresh renderer per block), which must NOT run per keystroke over a long
// buffer.
func (m *ModernScreen) resize() {
	lay := m.layout()
	m.viewport.SetSize(m.contentWidth(), lay.contentH)
}

// rerender re-sizes the viewport AND re-renders the focused projection into it. It is the
// "the rendered buffer changed" sync — called ONLY when the lines actually differ: a
// transcript-changing event, a collapse toggle (ctrl+t / header-click), a focus change, or a
// width change (a new width reflows every line). Keeping the viewport height tied to the
// (variable) bottom chrome lets the mouse hit-test's row math (entryAt) agree with what View
// drew, and the auto-follow tail stays pinned across the re-render.
func (m *ModernScreen) rerender() {
	m.resize()
	m.viewport.SetLines(m.renderFocused())
}

// renderFocused renders the focused loop's whole projection to the viewport's provenance-
// carrying lines: each committed entry through renderEntryLines under its EFFECTIVE collapse
// state (per-entry override else the global default), then the in-progress live segment
// appended after. It is the flat []renderedLine the viewport windows and selects over.
func (m ModernScreen) renderFocused() []renderedLine {
	committed, live := m.transcript.projectionFor(m.focusedLoopID)
	width := m.contentWidth()
	var out []renderedLine
	for i := range committed {
		lines := renderEntryLines(committed[i], width, m.collapse.Effective(committed[i].ID))
		// MODERN-ONLY: paint the gray panel behind user rows (scrollback keeps them bare).
		if committed[i].Kind == kindUser {
			lines = paintUserBackground(lines, width)
		}
		out = append(out, lines...)
	}
	out = append(out, m.liveTailLines(live)...)
	return out
}

// liveTailLines renders the focused loop's in-progress live segment to viewport lines,
// REUSING the existing live renderer (renderLiveAssistant) — never a new one. For the
// PRIMARY alias it matches the scrollback surface's live tail: it suppresses the
// orchestrator's raw running Subagent card (its activity shows via the nested pending card)
// and appends the in-flight pending subagent cards. A focused NON-primary projection carries
// no live.Calls and no pending cards of its own, so its live tail is just its streamed
// thinking/text (live tool-spinner parity for projections is deferred — see routeProjection).
// The thinking fold follows the GLOBAL collapse default (the live tail has no committed
// displayID to key a per-entry override on), and a zero animState renders a static tail
// (Task 9 threads the blink/spinner phase).
func (m ModernScreen) liveTailLines(live liveSeg) []renderedLine {
	width := m.contentWidth()
	calls := live.Calls
	var pending []ToolCallView
	if m.focusedLoopID == m.transcript.primaryLoopID || m.focusedLoopID.IsZero() {
		calls = nonSubagentCalls(live.Calls)
		pending = m.transcript.pendingSubagentCards()
	}
	if live.empty() && len(pending) == 0 {
		return nil
	}
	styled := renderLiveAssistant(live.Thinking, live.Text, calls, pending, !m.collapse.globalCollapsed, width, animState{})
	if styled == "" {
		return nil
	}
	lines := strings.Split(styled, "\n")
	out := make([]renderedLine, len(lines))
	for i, ln := range lines {
		out[i] = renderedLine{styled: ln, plain: plainFromStyled(ln), entry: liveTailEntryID, sub: i}
	}
	return out
}

// bar builds the active-loops bar from the transcript's loop table (loops()) — the SINGLE
// source of the session's loops + liveness, never a second registry. focused is the currently
// focused loop. The gate marker ("!") is set for any loop with a PENDING prompt (permission or
// AskUser) awaiting the user, read from the interaction model's pending FIFO
// (pendingGateLoops): a gate on a non-focused loop signals it needs attention WITHOUT stealing
// focus (design §Prompts), leaving the user to focus it. A gate on the focused loop still marks
// (focus and gate are independent flags).
func (m ModernScreen) bar() loopBar {
	infos := m.transcript.loops()
	gated := m.interaction.pendingGateLoops()
	entries := make([]loopBarEntry, 0, len(infos))
	for _, li := range infos {
		entries = append(entries, loopBarEntry{id: li.ID, name: li.Name, live: li.Live, gate: gated[li.ID]})
	}
	return loopBar{entries: entries, focused: m.focusedLoopID, max: modernLoopBarCap}
}

// bottomBoxView renders the bottom box for the current interaction mode (the reused composer,
// or a prompt control when a prompt is active) via the shared surface primitive.
func (m ModernScreen) bottomBoxView() string {
	return bottomBox(m.surfaceInputs())
}

// surfaceInputs builds the agent-free snapshot the shared bottom-box + status primitives
// read: the interaction model, the FOCUSED loop's status + live signals, and the frame
// dimensions.
func (m ModernScreen) surfaceInputs() surfaceInputs {
	return surfaceInputs{
		Interaction: m.interaction,
		Status:      m.focusedStatus(),
		StatusState: m.statusInputs(),
		Width:       m.width,
		Height:      m.height,
	}
}

// focusedStatus is the turn-lifecycle Status the status line reflects for the FOCUSED loop
// (design §Status line). For the PRIMARY loop — and the zero id (the single-loop default) —
// it is the shared session/turn status the core derives (m.status), preserving idle/running
// PLUS the interrupt/clear transitions the core owns. For a NON-PRIMARY focused loop the core
// status (which tracks ONLY the primary) does not apply, so it is derived from that loop's own
// projection: its live segment being active — set on the loop's TurnStarted, cleared on the
// loop's terminal — reads Running, else Idle. statusInputs() then refines a Running label into
// thinking…/streaming… from the same projection's live signals. This is a deliberately MINIMAL
// "you are viewing loop X, and whether it is live" indication, NOT a full per-loop status
// machine: Interrupting/Resetting are primary-only concerns and are never shown for a subagent.
func (m ModernScreen) focusedStatus() Status {
	if m.focusedLoopID == m.transcript.primaryLoopID || m.focusedLoopID.IsZero() {
		return m.status
	}
	_, live := m.transcript.projectionFor(m.focusedLoopID)
	if live.active {
		return StatusRunning
	}
	return StatusIdle
}

// statusInputs snapshots the status signals for the FOCUSED loop: whether its live segment
// is streaming narration or only thinking, and which prompt (if any) is active. The status
// line reflects the focused loop's activity (design §Status line).
func (m ModernScreen) statusInputs() statusInputs {
	_, live := m.transcript.projectionFor(m.focusedLoopID)
	in := statusInputs{
		streaming: live.Text != "",
		thinking:  live.Text == "" && live.Thinking != "",
	}
	if p := m.activePrompt(); p != nil {
		in.permissionActive = p.Kind == promptPermission
		in.userInputActive = p.Kind == promptUserInput
	}
	return in
}

// modernStatusLine is the focused loop's status line for the frame: the shared gradient
// status label PLUS, while the focused loop's turn is running, a faint live-elapsed suffix
// " (Ns)" / " (Nm Ss)". Idle / no active turn → no suffix. The suffix is a single faint
// Render call so its "(2m 34s)" text stays contiguous (substring-findable) rather than
// split by per-glyph styling.
func (m ModernScreen) modernStatusLine() string {
	line := renderStatusLine(m.focusedStatus(), m.statusInputs(), 0)
	if d, ok := m.turnElapsed(); ok {
		line += styles.StatusStyle.Render(" (" + formatElapsed(d) + ")")
	}
	return line
}

// turnElapsed returns the focused loop's live turn-elapsed duration and whether to show it.
// It shows ONLY when the focused loop is Running and has a recorded, non-zero turn start:
// "awaiting approval" is a Running sub-state, so its timer keeps counting, while idle,
// interrupting, and clearing carry none. Elapsed is now - turnStartedAt[focused] (the tick
// clock less the event start time), floored at zero so a not-yet-arrived tick never shows a
// negative span. A zero start (a restore/backlog with no streaming timestamps) reads as "no
// timer" — mirroring formatThought's bare-"Thought" fallback.
func (m ModernScreen) turnElapsed() (time.Duration, bool) {
	if m.focusedStatus() != StatusRunning {
		return 0, false
	}
	lid := m.focusedLoopID
	if lid.IsZero() {
		lid = m.transcript.primaryLoopID
	}
	start, ok := m.turnStartedAt[lid]
	if !ok || start.IsZero() {
		return 0, false
	}
	d := m.now.Sub(start)
	if d < 0 {
		d = 0
	}
	return d, true
}

// formatElapsed renders a live turn-elapsed span in whole seconds: under a minute as "Ns"
// (e.g. "8s"), a minute or more as "Nm Ss" (e.g. "2m 34s"), matching the status-line
// examples. A negative span floors to "0s" (a defensive guard; turnElapsed already floors).
func formatElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d / time.Second)
	if secs < 60 {
		return strconv.Itoa(secs) + "s"
	}
	return strconv.Itoa(secs/60) + "m " + strconv.Itoa(secs%60) + "s"
}

// View composes the frame top to bottom — the viewport content (the focused projection with
// collapse), one status line, the active-loops bar, and the bottom box — and returns a
// per-frame View with the modern configuration: AltScreen on and cell-motion mouse (the v2
// per-frame fields the copy-while-scrolling design turns on), plus the composer's Kitty
// keyboard request (see Screen.View for why). It returns an empty view until the first sized
// frame (avoids a 0×0 first frame).
func (m ModernScreen) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	v := tea.NewView(m.composeBody(m.layout()))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
	return v
}

// composeBody stacks the frame's rows for lay: the viewport content padded/clamped to
// EXACTLY contentH rows (so the chrome sits at the fixed rows regionAt assumes), then the
// status line, the loop bar, and the bottom box. The viewport is sized to lay.contentH on a
// LOCAL copy before rendering, so the drawn content region always matches the current layout
// even if the bottom chrome changed since the last Update-side resize/rerender. Every line is
// width-clamped (truncate, never wrap) so no row exceeds the frame width.
func (m ModernScreen) composeBody(lay modernLayout) string {
	vp := m.viewport
	vp.SetSize(m.contentWidth(), lay.contentH)

	rows := make([]string, 0, m.height)
	if content := vp.View(); content != "" {
		rows = append(rows, strings.Split(content, "\n")...)
	}
	if len(rows) > lay.contentH {
		rows = rows[:lay.contentH]
	}
	for len(rows) < lay.contentH {
		rows = append(rows, "")
	}
	rows = append(rows, m.modernStatusLine())
	rows = append(rows, m.bar().Render(m.width))
	rows = append(rows, strings.Split(m.bottomBoxView(), "\n")...)
	return clampSurfaceWidth(strings.Join(rows, "\n"), m.width)
}

// compile-time assertion that ModernScreen is a tea.Model (Init/Update/View, value receiver).
var _ tea.Model = ModernScreen{}
