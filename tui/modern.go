package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
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
// Stage-1 scope (this task): focus stays on the PRIMARY loop — focusedLoopID is structured
// so Task 8's bar-click / ctrl+n / ctrl+p only need to change it. Full prompt handling,
// submit auto-refocus, the bar's gate marker, and the parity items (clear/interrupt/queued/
// restore/images) are deferred to Task 9; a bar click resolves its region but does not yet
// switch focus (Task 8).
type ModernScreen struct {
	sessionCore

	viewport viewportModel // the scrollable/selectable content window
	collapse collapseState // the retroactive thinking-fold state (ctrl+t + header-click)

	// focusedLoopID selects which loop's projection the viewport renders
	// (projectionFor(focusedLoopID)). It defaults to the agent's primary loop and — in
	// Stage 1 — never changes; Task 8 repoints it on a bar click / focus chord. The whole
	// viewport is a pure VIEW over already-received, already-projected state, so a focus
	// change is a re-render, never a re-subscribe.
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
	return ModernScreen{
		sessionCore:   newSessionCore(ctx, agent, open, banner, allLoopsFilter),
		viewport:      viewportModel{atTail: true},
		collapse:      newCollapseState(),
		focusedLoopID: agent.PrimaryLoopID(),
	}
}

// Init focuses the composer (starting the cursor blink), schedules the opening banner
// (systemReadyMsg), and attaches the session-lifetime ALL-LOOPS subscription (m.subscribe
// resolves the injected allLoopsFilter). Cold-restore backlog repaint (ReplayBacklog) is a
// parity item deferred to Task 9 — a NEW session comes up idle, so the viewport is driven
// entirely by live events after the first Submit.
func (m ModernScreen) Init() tea.Cmd {
	return tea.Batch(
		m.interaction.input.Focus(),
		func() tea.Msg { return systemReadyMsg{} },
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
	}
	return m, nil
}

// handleResize stores the terminal dimensions, resizes the composer, commits a deferred
// opening banner (if systemReadyMsg arrived first), and re-sizes + re-renders the viewport.
func (m ModernScreen) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	m.ready = true
	m.interaction.input.Resize(msg.Width)
	if m.startupPending && !m.startupCommitted {
		m.commitStartup()
	}
	m.refresh()
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
	m.refresh()
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
	m.refresh()
	return rearm
}

// handleSubscribed installs the session-lifetime subscription via the core and starts the
// reader. On error the core commits a fatal error entry the viewport re-renders; on success
// it returns subNext, the single reader driving every subsequent event.
func (m *ModernScreen) handleSubscribed(msg subscribedMsg) tea.Cmd {
	cmd, present := m.sessionCore.applySubscribed(msg)
	if present {
		m.refresh()
	}
	return cmd
}

// handleSubClosed reacts to the reader observing a closed channel. A nil err is an
// intentional Close (nothing to surface); a non-nil err (hub-forced loss) commits an error
// entry the viewport re-renders.
func (m *ModernScreen) handleSubClosed(msg subClosedMsg) tea.Cmd {
	if m.sessionCore.applySubClosed(msg) {
		m.refresh()
	}
	return nil
}

// handleSubmitResult surfaces a fire-and-forget Submit outcome via the core. Success commits
// nothing (the authoritative user row arrives from the loop's TurnStarted); a non-nil err
// commits a faint error entry the viewport re-renders.
func (m *ModernScreen) handleSubmitResult(msg submitResultMsg) tea.Cmd {
	if m.sessionCore.applySubmitResult(msg) {
		m.refresh()
	}
	return nil
}

// handleInterruptResult applies an Interrupt outcome via the core. On error it returns to
// Running with a faint error entry the viewport re-renders; on success it stays Interrupting
// until the loop's TurnInterrupted terminal lands Idle.
func (m *ModernScreen) handleInterruptResult(msg interruptResultMsg) tea.Cmd {
	if m.sessionCore.applyInterruptResult(msg) {
		m.refresh()
	}
	return nil
}

// handlePromptResult surfaces a bounded prompt-dispatch outcome via the core. A nil err is a
// silent success; a non-nil err commits a faint error entry the viewport re-renders.
func (m *ModernScreen) handlePromptResult(msg promptResultMsg) tea.Cmd {
	if m.sessionCore.applyPromptResult(msg) {
		m.refresh()
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
		m.refresh()
		return cmd
	}
	// Successful reopen: reset the modern presentation to match the fresh session.
	m.focusedLoopID = m.agent.PrimaryLoopID()
	m.collapse = newCollapseState()
	m.viewport = viewportModel{atTail: true}
	m.startupPending = false
	m.startupCommitted = false
	m.refresh()
	return cmd
}

// handleKey routes a key press by the design's precedence. ctrl+c (quit) and ctrl+t (toggle
// the global collapse fold) are GLOBAL and handled first; ctrl+n/ctrl+p are RESERVED as
// no-ops for Task 8's focus cycle so the composer never swallows them. Then: (1) an active
// prompt consumes its keys; else Esc interrupts a running turn; (2/3) the viewport consumes
// ONLY its non-conflicting nav keys (PageUp/PageDown/Home/End), and everything else — the
// arrow keys and printable input — falls through to the composer.
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
		m.refresh()
		return m, nil
	case "ctrl+n":
		// Task 8: focus the NEXT loop (bar.cycle(+1)). Reserved as a no-op so the chord is
		// claimed by the viewport layer, never routed to the composer.
		return m, nil
	case "ctrl+p":
		// Task 8: focus the PREVIOUS loop (bar.cycle(-1)). Reserved no-op (see ctrl+n).
		return m, nil
	}
	// Precedence (1): an active prompt consumes its own approve/deny/choice/answer keys.
	if m.activePrompt() != nil {
		return m.routeToInteraction(msg)
	}
	// Esc with no prompt keeps its legacy meaning — interrupt a running turn (a no-op idle).
	if msg.String() == "esc" {
		return m, m.sessionCore.interruptRunning()
	}
	// Precedence (3): the viewport consumes ONLY PageUp/PageDown/Home/End (handleKey returns
	// false for every other key), so those scroll the content while the arrow keys and
	// printable input fall through to the composer (precedence 2).
	if m.viewport.handleKey(msg) {
		return m, nil
	}
	// Precedence (2): the composer consumes printable input and its editing/↑↓ keys.
	return m.routeToInteraction(msg)
}

// routeToInteraction delegates a key to the interaction model and maps the resulting typed
// uiAction into the agent-driving command via the core. When the action committed an
// out-of-band entry (e.g. /help, a submit build error), the viewport re-renders to surface
// it. It ALSO refreshes the viewport size unconditionally, so a composer that grew to
// multiple rows (shrinking the content region) keeps the viewport height in sync. The
// editor's blink cmd is batched so the cursor keeps blinking in compose/answer modes.
func (m ModernScreen) routeToInteraction(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var action uiAction
	var blink tea.Cmd
	m.interaction, action, blink = m.interaction.Update(msg)
	cmd, _ := m.sessionCore.mapAction(action)
	m.refresh()
	return m, tea.Batch(cmd, blink)
}

// handleMouse routes a mouse event by the region it falls in. The wheel scrolls the content
// wherever the pointer sits (the viewport reads only the wheel's direction), so it is routed
// unconditionally. A content-region click/drag/release drives the viewport's select/copy and
// (on a plain click) toggles the clicked entry's fold. A bar-region click resolves its region
// but does NOT switch focus in Stage 1 (Task 8). The status/box regions have no mouse
// behavior yet (keys drive the composer/prompt).
func (m *ModernScreen) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if w, ok := msg.(tea.MouseWheelMsg); ok {
		return m.viewport.handleMouse(w)
	}
	mouse := msg.Mouse()
	switch m.regionAt(mouse.Y) {
	case regionContent:
		return m.contentMouse(msg, mouse)
	case regionBar:
		// TODO(Task 8): resolve m.bar().HitTest(mouse.X) and repoint m.focusedLoopID +
		// re-render. Stage 1 only proves content-vs-bar routing; focus-switching is deferred.
		return nil
	default: // regionStatus, regionBox — keys, not the mouse, drive these in Stage 1.
		return nil
	}
}

// contentMouse forwards a content-region mouse event to the viewport (which draws its own
// selection and returns a copy command on release) and implements the click-vs-drag collapse
// rule: a plain click (press+release with no intervening motion) toggles the clicked entry's
// fold, while a drag is a text selection and never toggles. The content region sits at row 0,
// so the global Y is already the viewport-local row.
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
		if !m.mouseDragging {
			if id, ok := m.viewport.entryAt(mouse.Y); ok {
				m.collapse.Toggle(id)
				m.refresh()
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

// refresh re-sizes the viewport to the current layout's content region and re-renders the
// focused projection into it — the single "the view changed" sync called after every
// transcript / collapse / size change. Keeping the viewport height tied to the (variable)
// bottom chrome is what lets the mouse hit-test's row math (entryAt) agree with what View
// drew, and the auto-follow tail stays pinned across the re-render.
func (m *ModernScreen) refresh() {
	lay := m.layout()
	m.viewport.SetSize(m.contentWidth(), lay.contentH)
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
		out = append(out, renderEntryLines(committed[i], width, m.collapse.Effective(committed[i].ID))...)
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
// focused loop (Stage 1: the primary). The gate marker is left off in Stage 1; Task 9 marks a
// loop with a pending prompt without stealing focus.
func (m ModernScreen) bar() loopBar {
	infos := m.transcript.loops()
	entries := make([]loopBarEntry, 0, len(infos))
	for _, li := range infos {
		entries = append(entries, loopBarEntry{id: li.ID, name: li.Name, live: li.Live, gate: false})
	}
	return loopBar{entries: entries, focused: m.focusedLoopID, max: modernLoopBarCap}
}

// bottomBoxView renders the bottom box for the current interaction mode (the reused composer,
// or a prompt control when a prompt is active) via the shared surface primitive.
func (m ModernScreen) bottomBoxView() string {
	return bottomBox(m.surfaceInputs())
}

// surfaceInputs builds the agent-free snapshot the shared bottom-box + status primitives
// read: the interaction model, the focused loop's status signals, and the frame dimensions.
func (m ModernScreen) surfaceInputs() surfaceInputs {
	return surfaceInputs{
		Interaction: m.interaction,
		Status:      m.status,
		StatusState: m.statusInputs(),
		Width:       m.width,
		Height:      m.height,
	}
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
// even if the bottom chrome changed since the last Update-side refresh. Every line is
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
	rows = append(rows, renderStatusLine(m.status, m.statusInputs(), 0))
	rows = append(rows, m.bar().Render(m.width))
	rows = append(rows, strings.Split(m.bottomBoxView(), "\n")...)
	return clampSurfaceWidth(strings.Join(rows, "\n"), m.width)
}

// compile-time assertion that ModernScreen is a tea.Model (Init/Update/View, value receiver).
var _ tea.Model = ModernScreen{}
