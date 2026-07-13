package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"

	"github.com/looprig/cli/tui/styles"
)

// Screen is the MODERN VIEWPORT presentation shell over the shared sessionCore
// transport (embedded). Where the scrollback-first Screen lets the terminal own history,
// Screen owns an alt-screen VIEWPORT the user can scroll and select/copy from while
// content streams: it renders the FOCUSED loop's projection into a hand-rolled
// viewportModel (scroll + drag-select + copy), applies a RETROACTIVE collapse fold
// (ctrl+t + header-click), draws a bottom active-loops bar and the reused composer, and
// subscribes to EVERY loop's live stream (AllLoopsEventFilter) so any focused subagent's tokens
// render live rather than freezing at Enduring StepDone granularity.
//
// The core owns event routing exactly as it does for Screen; Screen adds ONLY the
// viewport presentation. Update delegates transport to the core then re-renders the focused
// projection into the viewport (keeping the auto-follow tail pinned); View composes, top to
// bottom, the viewport content, one status line, a blank gap, the bottom box, a blank gap, and
// the active-loops bar, and returns a per-frame View with AltScreen + cell-motion mouse (the
// v2 fields the copy-while-scrolling design turns on). Agent() is promoted from the embedded
// sessionCore, so Screen satisfies the composition root's agentHolder through that single
// definition.
//
// Focus switching (Task 8): ctrl+n / ctrl+p cycle focus over the bar's loops and a bar-region
// click focuses the clicked loop, both repointing focusedLoopID and re-rendering that loop's
// projection (focusLoop). Focus is VIEW-ONLY — it never submits/interrupts a loop.
//
// Prompts + feature parity (Task 9): prompt keys route to the reused interaction model
// (handleKey precedence (2)) and the bottom box renders the head gate via the shared surface;
// a pending gate marks its loop with "!" in the bar WITHOUT stealing focus (bar() reads
// pendingGateLoops). A composer submit targets the FOCUSED loop (routeToInteraction — Stage 2:
// submitting while focused on any loop runs a new turn on that loop and stays focused there).
// A cold-restore session repaints its history: Init batches restoreBacklogCmd and handleRestored
// folds the backlog into the transcript + projections + loop table and re-renders. The remaining
// parity items (/clear reopen, esc/ctrl+c interrupt, queued input, image @path rejection) all
// flow through the shared sessionCore, so they are shared with Screen rather than re-implemented.
type Screen struct {
	sessionCore

	viewport viewportModel // the scrollable/selectable content window
	collapse collapseState // the retroactive thinking-fold state (ctrl+t + header-click)

	// focusedLoopID selects which loop's projection the viewport renders
	// (projectionFor(focusedLoopID)). It defaults to the agent's ACTIVE loop (Agent.ActiveLoopID,
	// which may differ from the transcript root) and is
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

	// restoring is the initial replay barrier. Subscription events continue to be read and
	// re-armed, but reducer inputs queue in arrival order until restoredMsg installs history.
	restoring     bool
	restoreBuffer []restoreInput

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

	// anim holds the status line's animation state: its frame counter flows the status-label
	// + dot lime↔blue gradient (renderStatusLine's phase). A SINGLE anim tick chain, started at
	// Init and re-armed every blinkInterval, advances it for the life of the program and stops
	// only on quit (quitting) — kept continuous for lifecycle simplicity rather than gated on
	// status. Only ACTIVE states render the gradient, so the shimmer flows only while something
	// is happening; IDLE renders a static faint line (renderStatusLine), so an idle tick costs
	// just a no-op recompose. It is meaningful ONLY to View's status line; the committed
	// transcript never consults it, and an anim tick recomposes ONLY the status line.
	anim animState

	// quitting latches on ctrl+c so the continuous anim tick chain self-terminates instead of
	// leaking a reschedule past tea.Quit: handleAnim stops re-arming once it is set.
	quitting        bool
	quitAfterReopen bool // ctrl+c during /clear waits until the handoff result is consumed
}

// tickInterval is the status-line timer's cadence: one tick per second while a turn
// is active, matching the whole-second granularity of formatElapsed.
const tickInterval = time.Second

// tickMsg is one 1s timer tick carrying the tick's own time — the clock the status-line
// elapsed timer advances on (never a render-time wall clock).
type tickMsg struct{ at time.Time }

// tickCmd schedules the next 1s timer tick. It is re-issued from handleTick only while a
// turn is active, so the chain self-terminates when the session goes idle.
func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg { return tickMsg{at: t} })
}

// animMsg is one status-line animation tick carrying its own time (unused at the UI; it
// satisfies tea.Tick's func(time.Time) tea.Msg shape). Handling it advances the status
// gradient phase (m.anim) by one frame and re-arms the next tick, so the shimmer runs
// continuously — idle included — for the life of the program. It never touches the transcript.
type animMsg time.Time

// animCmd schedules ONE status-line animation tick after blinkInterval — Screen's animation
// cadence (commands.go) — delivering an animMsg. handleAnim re-arms it every tick until the
// shell is quitting, so the modern status gradient animates continuously without a per-turn
// gate. It never re-renders the transcript.
func animCmd() tea.Cmd {
	return tea.Tick(blinkInterval, func(t time.Time) tea.Msg { return animMsg(t) })
}

// loopBarCap is the visible-cap the modern active-loops bar renders under: at most
// this many loop segments show, the rest folding into a "… +N" overflow marker so the bar
// never grows unbounded across a long session's accumulated loops.
const loopBarCap = 8

// liveTailEntryID is the reserved provenance id every live-tail line carries. The live tail
// is NOT a committed entry, and the first committed entry allocates displayID 1, so the zero
// id can never collide with a committed entry: selection/copy still work by (entry, sub)
// identity, and a stray collapse-click on the live tail toggles an id no committed entry owns
// (a harmless no-op).
const liveTailEntryID displayID = 0

// New constructs an idle Screen driving agent, with open as the /clear thunk and
// banner the agent name/description shown as the opening info notice. The session
// subscription delivers EVERY loop's live Ephemeral stream (see AllLoopsEventFilter) — the
// modern mode renders any focused loop's whole live output.
// The viewport starts pinned to the tail (atTail) so streaming content auto-follows, the
// collapse state starts folded (dense; ctrl+t expands), and focus starts on the agent's ACTIVE
// loop (Agent.ActiveLoopID — the session's current default target); a later selection event
// never moves it.
func New(ctx context.Context, agent Agent, open OpenAgent, banner AgentBanner) Screen {
	m := Screen{
		sessionCore:   newSessionCore(ctx, agent, open, banner),
		viewport:      viewportModel{atTail: true},
		collapse:      newCollapseState(),
		focusedLoopID: agent.ActiveLoopID(),
		turnStartedAt: make(map[uuid.UUID]time.Time),
		restoring:     true,
	}
	m.interaction = styleComposer(m.interaction)
	return m
}

// composerMinLines is the modern composer's default visible height — two rows
// instead of the scrollback composer's one, so the input reads as a roomier panel while
// still auto-growing to maxInputLines.
const composerMinLines = 2

// composerPadV is the modern composer's inner vertical padding: 1 gray pad row above and below
// the text, so the composer reads as a padded card matching the user-message card (padUserCard).
// The text region still auto-grows to maxInputLines as the user types — the padding only frames
// it. Scrollback's composer keeps 0 (it never calls SetVerticalPadding), staying bare.
const composerPadV = 1

// styleComposer applies the MODERN composer treatment to in's input box — the 2-line
// default height, the gray panel fill (styles.PanelBg), and one inner vertical pad row above
// and below the text (composerPadV, matching the user card) — and returns the updated model.
// It runs at every site that
// installs a fresh (default 1-line, background-free, unpadded) interaction for a Screen.
// The /clear reopen keeps the same input
// (ClearPrompts preserves it), so it needs no re-apply. Scrollback's Screen never calls this,
// so its composer stays byte-identical.
func styleComposer(in interactionModel) interactionModel {
	in.input.SetMinLines(composerMinLines)
	in.input.SetBackground(styles.PanelBg)
	in.input.SetVerticalPadding(composerPadV)
	return in
}

// Init focuses the composer (starting the cursor blink), schedules the opening banner
// (systemReadyMsg), schedules the cold-restore repaint, and attaches the session-lifetime
// ALL-LOOPS subscription (m.subscribe uses AllLoopsEventFilter). restoreBacklogCmd
// folds a restored session's historical Enduring backlog off the update loop. Live events may
// arrive first; the restore barrier buffers and continuously re-arms them, then applies them in
// arrival order after restoredMsg installs history. An empty backlog simply releases the barrier.
func (m Screen) Init() tea.Cmd {
	return tea.Batch(
		m.interaction.input.Focus(),
		func() tea.Msg { return systemReadyMsg{} },
		restoreBacklogCmd(m.appCtx, m.agent),
		m.subscribe(),
		// The anim tick chain runs continuously for the life of the program (started here,
		// re-armed by handleAnim, stopped only on quit). It shimmers the gradient only for
		// ACTIVE states; while idle it just recomposes the static faint status line.
		animCmd(),
	)
}

// Update advances the model. It is a value receiver so Screen satisfies tea.Model;
// the mutating handlers take a pointer to the addressable receiver and Update returns the
// updated value. Note the two-statement pattern for the pointer-receiver handlers (cmd :=
// …; return m, cmd): a `return m, m.handle(...)` would evaluate the first result (the OLD
// m) before the handler mutates it, stranding the mutation.
func (m Screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		cmd := m.handleMouse(msg)
		return m, cmd
	case eventMsg:
		if m.restoring {
			cmd := m.bufferRestoreEvent(msg)
			return m, cmd
		}
		cmd := m.handleEvent(msg.ev)
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
		if m.restoring {
			m.restoreBuffer = append(m.restoreBuffer, restoreInput{kind: restoreInputSubmit, submit: msg})
			return m, nil
		}
		cmd := m.handleSubmitResult(msg)
		return m, cmd
	case interruptResultMsg:
		cmd := m.handleInterruptResult(msg)
		return m, cmd
	case reopenResultMsg:
		if m.restoring {
			return m, nil // /clear is not admitted before initial replay completes
		}
		cmd := m.handleReopenResult(msg)
		return m, cmd
	case closeForQuitResultMsg:
		cmd := m.handleCloseForQuitResult(msg)
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
	case animMsg:
		cmd := m.handleAnim()
		return m, cmd
	}
	return m, nil
}

// handleResize stores the terminal dimensions, resizes the composer, and commits a deferred
// opening banner (if systemReadyMsg arrived first). A WIDTH change reflows every rendered
// line (they were laid out at the old width) and a deferred banner commit changed the buffer
// — either needs a full rerender; a PURE height change only needs a size sync (the lines are
// unchanged, so re-rendering the transcript is wasted work).
func (m Screen) handleResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
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
func (m *Screen) handleSystemReady() tea.Cmd {
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
func (m *Screen) commitStartup() {
	if m.startupCommitted {
		return
	}
	m.startupPending = false
	m.startupCommitted = true
	m.transcript = m.transcript.CommitGlobalNotice(noticeInfo, m.banner.bannerText())
	if greeting := strings.TrimSpace(m.banner.Greeting); greeting != "" {
		m.transcript = m.transcript.CommitGlobalNotice(noticeInfo, greeting)
	}
}

// handleEvent delegates one subscription event to the shared core (which routes it
// through BOTH reducers, derives the active-loop turn status, and re-arms the reader) and then
// re-renders the FOCUSED projection into the viewport, keeping the auto-follow tail pinned.
// The turn-phase cue is unused in Stage 1 (a static status line; Task 9 threads the blink).
//
// Before the core folds the event it stamps the model clock (m.now) onto an unstamped
// Ephemeral TokenDelta (stampEphemeralClock): the harness publishes TokenDeltas UNSTAMPED
// (only Enduring events get a Factory CreatedAt — see pkg/loop publish; per-token crypto/rand
// is deliberately avoided), so a thinking delta reaches the transcript reducer with a ZERO
// CreatedAt and its "thought for Nsec" span would always be 0. Stamping it with the SAME
// clock the turn timer uses (m.now, seeded from TurnStarted.CreatedAt and advanced by the 1s
// tick) gives the reducer a real, second-granularity timestamp — so every loop projection
// measures a genuine thinking span, deterministically
// driven by injected event/tick times (no render-time wall clock).
func (m *Screen) handleEvent(ev event.Event) tea.Cmd {
	ev = stampEphemeralClock(ev, m.now)
	rearm, _ := m.sessionCore.handleEvent(ev)
	// Commit the "turn ran for Ns" line BEFORE trackTurnClock clears the start time it reads.
	m.commitTurnRanNotice(ev)
	m.trackTurnClock(ev)
	m.rerender()
	// A turn becoming active (re)starts the 1s status-line tick if none is in flight; the
	// tick self-terminates once every turn has ended (handleTick), so it never leaks past
	// an idle session.
	return tea.Batch(rearm, m.maybeStartTick())
}

// bufferRestoreEvent retains one raw delivery behind the initial replay barrier while
// immediately re-arming the subscription reader. Token timestamps are deliberately deferred:
// ordered drain must let an earlier TurnStarted advance m.now before stamping a later delta.
func (m *Screen) bufferRestoreEvent(msg eventMsg) tea.Cmd {
	m.restoreBuffer = append(m.restoreBuffer, restoreInput{kind: restoreInputEvent, delivery: msg})
	if m.sub == nil {
		return nil
	}
	return subNext(m.sub)
}

// stampEphemeralClock returns ev with the model clock stamped onto its Header.CreatedAt when
// ev is an UNSTAMPED Ephemeral TokenDelta — the timing source for a committed thinking
// entry's "thought for Nsec" span. The harness never stamps Ephemeral events (they are never
// journaled and per-token crypto/rand is avoided), so a TokenDelta arrives with a zero
// CreatedAt; the reducer's recordThinking/recordNonThinking would then never seed a start and
// the span would collapse to 0. Stamping the delta with clock (m.now) — the same clock the
// turn timer advances on the 1s tick — feeds the reducer a real timestamp through its
// existing per-loop timing machinery, so subagent
// thinking is timed for free. It touches ONLY a zero-CreatedAt TokenDelta: any other event
// (including an already-stamped delta, e.g. a test that stamps its own) is returned unchanged,
// so trackTurnClock still reads the authoritative TurnStarted.CreatedAt. A zero clock (no turn
// has seeded m.now yet) is a no-op — the delta stays unstamped and reads a 0 span, the safe
// bare-"thought" fallback.
func stampEphemeralClock(ev event.Event, clock time.Time) event.Event {
	td, ok := ev.(event.TokenDelta)
	if !ok || clock.IsZero() || !td.CreatedAt.IsZero() {
		return ev
	}
	td.Header.CreatedAt = clock
	return td
}

// trackTurnClock maintains turnStartedAt from a loop's turn-lifecycle events: a TurnStarted
// records its start time (and seeds the clock so the first pre-tick frame reads a sane
// elapsed); any terminal (TurnDone/TurnFailed/TurnInterrupted) clears it. Every loop's turn
// events reach here through the all-loops stream. Non-turn events are ignored.
func (m *Screen) trackTurnClock(ev event.Event) {
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

// commitTurnRanNotice appends a faint "○ turn ran for Ns" harness line when a loop's
// turn completes (event.TurnDone) — the frozen form of the live status-bar timer, so a finished
// turn leaves a record of how long it took. It MUST run before trackTurnClock clears
// turnStartedAt: the span is the TurnDone timestamp less turnStartedAt[loopID] (both Enduring
// CreatedAt values, so it is the TRUE turn duration, not the possibly-stale tick clock),
// formatted via formatElapsed ("25s" / "2m 5s"); a zero terminal timestamp falls back to the
// tick clock (m.now). It is a no-op for a failed/interrupted turn (those commit their own error/tombstone — a
// "ran for" summary would double up), and when no turn start was recorded (a cold restore
// carries no streaming timestamps).
func (m *Screen) commitTurnRanNotice(ev event.Event) {
	td, ok := ev.(event.TurnDone)
	if !ok {
		return
	}
	loopID := td.EventHeader().LoopID
	start, ok := m.turnStartedAt[loopID]
	if !ok || start.IsZero() {
		return
	}
	end := td.EventHeader().CreatedAt
	if end.IsZero() {
		end = m.now // the terminal carried no timestamp — fall back to the tick clock
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	m.transcript = m.transcript.CommitHarnessFor(loopID, "turn ran for "+formatElapsed(d))
}

// maybeStartTick launches the 1s status-line tick chain, but only when a turn is active
// AND no tick is already in flight — so a second concurrent TurnStarted never spawns a
// second chain, and an idle session never ticks. Returns nil when a tick is unnecessary.
func (m *Screen) maybeStartTick() tea.Cmd {
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
func (m *Screen) handleTick(msg tickMsg) tea.Cmd {
	m.ticking = false
	if msg.at.After(m.now) {
		m.now = msg.at
	}
	return m.maybeStartTick()
}

// handleAnim advances the status-line gradient one frame and re-arms the next anim tick,
// UNLESS the shell is quitting (then the chain stops so no tick leaks past tea.Quit). It is a
// PURE recompose: it advances ONLY m.anim (the gradient phase) and never re-renders the
// transcript buffer (renderFocused/SetLines) — the transcript is unchanged on an anim tick,
// and View recomposes the status line from the new frame every render, so advancing the frame
// and returning the reschedule is the whole update. It runs continuously at every status, but
// only ACTIVE states render the gradient — while idle the recompose yields the same static
// faint line (no shimmer). The turn timer keeps its own 1s tick.
func (m *Screen) handleAnim() tea.Cmd {
	if m.quitting {
		return nil
	}
	m.anim = m.anim.advance()
	return animCmd()
}

// handleRestored releases the initial replay barrier. A non-empty historical fold installs
// first, preserving shell-global startup/error rows and composer draft; buffered subscription
// events then pass through the normal reducers in arrival order. Empty/error replay also drains
// the buffer. Startup flags are deliberately untouched so message ordering cannot duplicate or
// suppress the banner.
func (m *Screen) handleRestored(msg restoredMsg) tea.Cmd {
	buffered := m.restoreBuffer
	m.restoreBuffer = nil
	m.restoring = false
	if msg.err != nil {
		m.transcript = m.transcript.CommitGlobalError(msg.err)
	} else if msg.eventCount != 0 {
		m.transcript = installRestoredTranscript(m.transcript, msg.transcript)
		m.interaction = installRestoredInteraction(m.interaction, msg.interaction)
	}
	for _, input := range buffered {
		switch input.kind {
		case restoreInputEvent:
			ev := stampEphemeralClock(input.delivery.ev, m.now)
			if id := ev.EventHeader().EventID; !id.IsZero() {
				if _, replayed := msg.eventIDs[id]; replayed {
					continue
				}
			}
			_, _ = m.sessionCore.handleEvent(ev) // reader was re-armed when the delivery was buffered
			m.commitTurnRanNotice(ev)
			m.trackTurnClock(ev)
		case restoreInputSubmit:
			m.sessionCore.applySubmitResult(input.submit)
		}
	}
	m.rerender()
	return m.maybeStartTick()
}

type restoreInputKind uint8

const (
	restoreInputEvent restoreInputKind = iota
	restoreInputSubmit
)

// restoreInput is one reducer input observed while initial replay is pending. Event deliveries
// and submit outcomes share one slice so their cross-kind arrival order is preserved.
type restoreInput struct {
	kind     restoreInputKind
	delivery eventMsg
	submit   submitResultMsg
}

// installRestoredTranscript installs historical reducer state while preserving shell-global
// startup/error rows already committed before replay completed. Historical IDs are rebased
// above the current allocator so preserved globals never collide with restored rows.
func installRestoredTranscript(current, restored transcriptModel) transcriptModel {
	offset := current.nextID
	if offset != 0 {
		restored.global = rebaseEntries(restored.global, offset)
		if restored.projections != nil {
			next := make(map[uuid.UUID]*loopProjection, len(restored.projections))
			for id, p := range restored.projections {
				if p == nil {
					next[id] = nil
					continue
				}
				cp := *p
				cp.committed = rebaseEntries(cp.committed, offset)
				next[id] = &cp
			}
			restored.projections = next
		}
		restored.nextID += offset
	}
	restored.global = append(append([]entry(nil), current.global...), restored.global...)
	return restored
}

// installRestoredInteraction preserves the live composer/draft while installing historical
// prompts. When history has no prompts the already-styled live interaction is authoritative;
// buffered live prompt events are applied normally after this install.
func installRestoredInteraction(current, restored interactionModel) interactionModel {
	if len(restored.pending) == 0 {
		return current
	}
	restored.input = current.input
	restored.slash = current.slash
	restored.files = current.files
	restored.composeDraft = current.input.Value()
	return restored
}

func rebaseEntries(entries []entry, offset displayID) []entry {
	if entries == nil {
		return nil
	}
	next := append([]entry(nil), entries...)
	for i := range next {
		next[i].ID += offset
	}
	return next
}

// handleSubscribed installs the session-lifetime subscription via the core and starts the
// reader. On error the core commits a fatal error entry the viewport re-renders; on success
// it returns subNext, the single reader driving every subsequent event.
func (m *Screen) handleSubscribed(msg subscribedMsg) tea.Cmd {
	cmd, present := m.sessionCore.applySubscribed(msg)
	if present {
		m.rerender()
	}
	return cmd
}

// handleSubClosed reacts to the reader observing a closed channel. A nil err is an
// intentional Close (nothing to surface); a non-nil err (hub-forced loss) commits an error
// entry the viewport re-renders.
func (m *Screen) handleSubClosed(msg subClosedMsg) tea.Cmd {
	if m.sessionCore.applySubClosed(msg) {
		m.rerender()
	}
	return nil
}

// handleSubmitResult surfaces a fire-and-forget Submit outcome via the core. Success commits
// nothing (the authoritative user row arrives from the loop's TurnStarted); a non-nil err
// commits a faint error entry the viewport re-renders.
func (m *Screen) handleSubmitResult(msg submitResultMsg) tea.Cmd {
	if m.sessionCore.applySubmitResult(msg) {
		m.rerender()
	}
	return nil
}

// handleInterruptResult applies an Interrupt outcome via the core. On error it returns to
// Running with a faint error entry the viewport re-renders; on success it stays Interrupting
// until the loop's TurnInterrupted terminal lands Idle.
func (m *Screen) handleInterruptResult(msg interruptResultMsg) tea.Cmd {
	if m.sessionCore.applyInterruptResult(msg) {
		m.rerender()
	}
	return nil
}

// handlePromptResult surfaces a bounded prompt-dispatch outcome via the core. A nil err is a
// silent success; a non-nil err commits a faint error entry the viewport re-renders.
func (m *Screen) handlePromptResult(msg promptResultMsg) tea.Cmd {
	if m.sessionCore.applyPromptResult(msg) {
		m.rerender()
	}
	return nil
}

// handleReopenResult applies a /clear reopen outcome. The core owns the transport ordering —
// on error no live agent remains, so it renders the error and exits; on success it swaps
// in the fresh agent, resets the shared transport, and
// re-subscribes with the INJECTED (all-loops) filter. Screen then resets its own
// viewport/collapse/focus so no remnant of the old session survives the swap.
func (m *Screen) handleReopenResult(msg reopenResultMsg) tea.Cmd {
	cmd, present := m.sessionCore.applyReopenResult(msg)
	if present {
		m.rerender()
		return cmd
	}
	// Successful reopen: reset the modern presentation to match the fresh session. Focus
	// initializes from the replacement agent's ActiveLoopID (the session's current default
	// target), mirroring New, and is set here once, never from a later
	// selection event.
	m.focusedLoopID = m.agent.ActiveLoopID()
	m.collapse = newCollapseState()
	m.viewport = viewportModel{atTail: true}
	m.startupPending = false
	m.startupCommitted = false
	m.rerender()
	if m.quitAfterReopen {
		m.closing = newAgentCloseHandoff(m.agent)
		return closeAgentForQuit(m.closing)
	}
	return cmd
}

// handleCloseForQuitResult consumes the deferred replacement close before clearing ownership.
// A close failure is terminal and retained for cli.Run; either way the close is complete.
func (m *Screen) handleCloseForQuitResult(msg closeForQuitResultMsg) tea.Cmd {
	if msg.handoff != m.closing {
		return nil
	}
	m.closing = nil
	m.agent = nil
	if msg.err != nil {
		m.terminalErr = fmt.Errorf("close replacement on deferred quit: %w", msg.err)
		m.transcript = m.transcript.CommitGlobalError(m.terminalErr)
	}
	return tea.Quit
}

// handleKey routes a key press in ACTUAL execution order: (1) the GLOBAL chords ctrl+c
// (quit), ctrl+t (toggle the global collapse fold), and ctrl+n/ctrl+p (cycle focus over the
// bar's loops) fire first, even with a prompt open — focus/fold are pure VIEW state and the
// composer must never swallow those chords; (2) an active prompt consumes its approve/deny/
// choice/answer keys; (3) with no
// prompt, Esc interrupts a running turn; (4) the viewport consumes ONLY its non-conflicting
// nav keys (PageUp/PageDown/Home/End); (5) everything else — the arrow keys and printable
// input — falls through to the composer.
func (m Screen) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.status == StatusResetting {
			// The async handoff owns the old/replacement lifecycle. Defer quitting until its
			// result is consumed so no replacement can escape the model unclaimed.
			m.quitting = true
			m.quitAfterReopen = true
			return m, nil
		}
		// Latch quitting so the continuous anim tick chain self-terminates (handleAnim stops
		// re-arming) rather than leaking a reschedule past tea.Quit. Then close the subscription
		// best-effort so it does not leak past quit (a synchronous, idempotent teardown), close
		// the agent (bounded async), and quit.
		m.quitting = true
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
// any loop runs a new turn on that loop and stays focused there (no auto-refocus). Only
// uiSubmit is intercepted; every other action kind (approve/deny/answer/slash/
// edit/interrupt) still routes through the shared core's mapAction unchanged, so Screen's
// default submit path (mapAction → submit → Submit) is untouched.
func (m Screen) routeToInteraction(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var action uiAction
	var blink tea.Cmd
	m.interaction, action, blink = m.interaction.Update(msg)
	if m.restoring && action.Kind == uiRunSlash && action.Slash == "/clear" {
		m.transcript = m.transcript.CommitGlobalNotice(noticeInfo, "restore in progress; /clear is unavailable")
		m.rerender()
		return m, nil
	}
	var cmd tea.Cmd
	var present bool
	if action.Kind == uiSubmit {
		// Submit to the focused loop and do not refocus —
		// the user stays on the loop they are watching. Blocks are built the same way the
		// core's default submit does (buildBlocks from action.Text), and a build error commits
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
func (m *Screen) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if w, ok := msg.(tea.MouseWheelMsg); ok {
		return m.viewport.handleMouse(w)
	}
	mouse := msg.Mouse()
	switch m.regionAt(mouse.Y) {
	case regionContent:
		return m.contentMouse(msg, mouse)
	case regionBar:
		return m.barMouse(msg, mouse)
	default: // regionStatus, regionBox, regionGap — keys (not the mouse) drive these, gaps are inert.
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
func (m *Screen) barMouse(msg tea.MouseMsg, mouse tea.Mouse) tea.Cmd {
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
func (m *Screen) focusLoop(loopID uuid.UUID) {
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
func (m *Screen) contentMouse(msg tea.MouseMsg, mouse tea.Mouse) tea.Cmd {
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

// screenLayout is the frame's top-to-bottom region geometry, the SINGLE source both View
// (which draws the regions) and regionAt (which hit-tests a mouse row) compute from, so a
// drawn row and a hit-tested row can never disagree. Top to bottom: the content region
// occupies rows [0, contentH); then an inert blank pad row, the status line, a blank gap row,
// the bottom box, a second blank gap row, and finally the active-loops bar at the very bottom.
type screenLayout struct {
	contentH int // viewport content rows: region [0, contentH)
	padTopY  int // the inert blank pad row between the content and the status line
	statusY  int // the status line row
	gapTopY  int // the inert blank gap row between the status line and the box
	boxTop   int // the first row of the bottom box
	boxH     int // the bottom box's rendered height
	gapBotY  int // the inert blank gap row between the box and the loop bar
	barY     int // the loop bar row (the very bottom)
}

// layout derives the region geometry from the current frame: the bottom box is measured
// first (its height varies with the composer/prompt), then the status, the blank pad row above
// it, the two blank gap rows, and the bar reserve one row each, and the viewport content gets
// whatever remains (floored at 0). Because it is deterministic in the model state, View and
// regionAt compute the identical layout.
//
// The row order is pad → status → gap → box → gap → bar (an inert blank pad row sets the
// status line off the content above it; the input box sits ABOVE the loop bar, each set off by
// an inert blank row), matching composeBody's stacking exactly.
//
// NOT side-effect-free: measuring the box (bottomBoxView → the bubbles textarea's View)
// drives the textarea's internal render/measure cache, so layout must run only on Bubble
// Tea's single model goroutine (never concurrently — see the serial subtest note in the
// tests).
func (m Screen) layout() screenLayout {
	const padH, statusH, barH, gapH = 1, 1, 1, 1
	boxH := lipgloss.Height(m.bottomBoxView())
	if boxH < 1 {
		boxH = 1
	}
	contentH := m.height - padH - statusH - gapH - boxH - gapH - barH
	if contentH < 0 {
		contentH = 0
	}
	padTopY := contentH
	statusY := padTopY + padH
	gapTopY := statusY + statusH
	boxTop := gapTopY + gapH
	gapBotY := boxTop + boxH
	barY := gapBotY + gapH
	return screenLayout{
		contentH: contentH,
		padTopY:  padTopY,
		statusY:  statusY,
		gapTopY:  gapTopY,
		boxTop:   boxTop,
		boxH:     boxH,
		gapBotY:  gapBotY,
		barY:     barY,
	}
}

// screenRegion is a frame region the mouse can fall in — the content viewport, the status
// line, the bottom box, the loop bar, or an inert gap row — the discriminant handleMouse
// routes on.
type screenRegion uint8

const (
	regionContent screenRegion = iota
	regionStatus
	regionBar
	regionBox
	regionGap
)

// regionAt maps a terminal row y to its frame region using the same layout View draws, so
// mouse routing (content select/copy vs a bar focus click) matches what the user sees. The
// inert blank pad row above the status line, the two blank gap rows (and any row past the bar)
// are regionGap — inert, no mouse behavior.
func (m Screen) regionAt(y int) screenRegion {
	lay := m.layout()
	switch {
	case y < lay.contentH:
		return regionContent
	case y == lay.statusY:
		return regionStatus
	case y >= lay.boxTop && y < lay.boxTop+lay.boxH:
		return regionBox
	case y == lay.barY:
		return regionBar
	default: // the two blank gap rows (and any row past the bar) — inert
		return regionGap
	}
}

// contentWidth is the column budget the focused projection renders to — the full frame
// width (the viewport draws no side gutter). A negative width (never in a real frame) floors
// to 0 so the renderers stay well-defined.
func (m Screen) contentWidth() int {
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
func (m *Screen) resize() {
	lay := m.layout()
	m.viewport.SetSize(m.contentWidth(), lay.contentH)
}

// rerender re-sizes the viewport AND re-renders the focused projection into it. It is the
// "the rendered buffer changed" sync — called ONLY when the lines actually differ: a
// transcript-changing event, a collapse toggle (ctrl+t / header-click), a focus change, or a
// width change (a new width reflows every line). Keeping the viewport height tied to the
// (variable) bottom chrome lets the mouse hit-test's row math (entryAt) agree with what View
// drew, and the auto-follow tail stays pinned across the re-render.
func (m *Screen) rerender() {
	m.resize()
	m.viewport.SetLines(m.renderFocused())
}

// renderFocused renders the focused loop's whole projection to the viewport's provenance-
// carrying lines: each committed entry through renderEntryLines under its EFFECTIVE collapse
// state (per-entry override else the global default), then the in-progress live segment
// appended after. It is the flat []renderedLine the viewport windows and selects over.
//
// MODERN-ONLY SPACING: one blank breathing-space row follows every committed entry (see
// blankSeparator) — the opening banner/greeting included, so the first real message is not
// glued to the header — EXCEPT within a tool-call group (see suppressSeparator): an assistant
// message and the (possibly parallel) tool calls it led render glued together with no blank,
// so the group reads as one cohesive action rather than a ladder of separated rows. Because
// every group's LAST entry still carries its trailing blank, the streaming live tail sits
// exactly one blank below the last committed entry (one gap, never two), so no special-casing
// is needed at the tail seam. Scrollback's renderEntry owns its own spacing and is untouched.
//
// MODERN-ONLY USER CARD: a kindUser entry is first bracketed with rail pad rows (padUserCard)
// and then gray-filled (paintUserBackground) so its message reads as a padded card — the pad
// rows are part of the entry (gray, rail-continued), distinct from the transparent blank
// separator that still follows the whole card.
func (m Screen) renderFocused() []renderedLine {
	committed, live := m.transcript.projectionFor(m.focusedLoopID)
	width := m.contentWidth()
	var out []renderedLine
	for i := range committed {
		lines := renderEntryLines(committed[i], width, m.collapse.Effective(committed[i].ID))
		// MODERN-ONLY: bracket the user row with rail pad rows (a padded card), then paint the
		// gray panel behind the whole block — pads included (scrollback keeps user rows bare).
		if committed[i].Kind == kindUser {
			lines = padUserCard(lines)
			lines = paintUserBackground(lines, width)
		}
		out = append(out, lines...)
		// A zero-line entry (an unknown/empty kind) has no last sub and nothing to set off, so it
		// gets no blank — which also keeps the blank's sub non-header (>= 1) for every real entry.
		// A tool call glued to its leading message/sibling (suppressSeparator) also gets no blank.
		if n := len(lines); n > 0 && !suppressSeparator(committed, i) {
			out = append(out, blankSeparator(committed[i].ID, n))
		}
	}
	out = append(out, m.liveTailLines(live)...)
	// The focused loop's pending queued inputs render LAST — below the live tail, where the
	// next turn's input will land — so a user firing several messages mid-turn sees them
	// stacked. They drop the instant each one's turn starts (startTurnUser commits the real
	// user row and dropQueued removes the affordance), so a queued row never duplicates its
	// committed row.
	out = append(out, m.queuedTailLines()...)
	return out
}

// blankSeparator is the MODERN-ONLY breathing-space row appended after a committed entry: an
// empty renderedLine (no styled, no plain, no background) that provides one blank line of
// visual separation between transcript entries. It is tagged with the PRECEDING entry's
// displayID and sub (the entry's last sub + 1, i.e. lineCount), so the gap "belongs" to the
// entry above it: a selection spanning entries naturally includes the newline (plain is empty),
// and a collapse-click that lands on it resolves to a non-header sub (>= 1) and never toggles.
// It carries NO styled bytes, so it never picks up the user gray fill (paintUserBackground has
// already run on the entry's own lines before this blank is appended).
func blankSeparator(id displayID, lineCount int) renderedLine {
	return renderedLine{styled: "", plain: "", entry: id, sub: lineCount}
}

// suppressSeparator reports whether the breathing-space blank after committed entry i should
// be OMITTED so the entry glues to the next one. It suppresses the blank when the NEXT entry is
// a tool call led by this assistant message or a preceding tool call (kindAssistant→kindTool or
// kindTool→kindTool) — so a step's narration and its (possibly parallel) tool calls read as one
// cohesive group, the assistant message visibly leading its calls, rather than a ladder of
// blank-separated rows. Every other adjacency keeps its blank, and the last entry (no next, so
// i+1 is out of range) always keeps its blank — preserving the one-gap seam to the live tail.
func suppressSeparator(committed []entry, i int) bool {
	if i+1 >= len(committed) || committed[i+1].Kind != kindTool {
		return false
	}
	return committed[i].Kind == kindAssistant || committed[i].Kind == kindTool
}

// liveTailLines renders the focused loop's in-progress live segment to viewport lines,
// REUSING the existing live renderer (renderLiveAssistant) — never a new one. For the
// focused projection it matches the scrollback surface's live tail: it suppresses the
// orchestrator's raw running Subagent card (its activity shows via the nested pending card)
// and appends the in-flight pending subagent cards.
// The live segment is EXCLUDED from the collapse fold: streaming thinking always renders
// fully EXPANDED ("│ thinking" + all reasoning) regardless of the global collapse default, so
// the user watches the model reason in real time; only COMMITTED thinking follows the collapse
// state (default collapsed → the one-line "│ thought for Nsec" summary), keyed on its committed
// displayID (the live tail has none). A zero animState renders a static tail (Task 9 threads
// the blink/spinner phase).
func (m Screen) liveTailLines(live liveSeg) []renderedLine {
	width := m.contentWidth()
	calls := live.Calls
	var pending []ToolCallView
	calls = nonSubagentCalls(live.Calls)
	pending = m.transcript.pendingSubagentCardsFor(m.focusedLoopID)
	if live.empty() && len(pending) == 0 {
		return nil
	}
	// expand=true unconditionally: the LIVE segment is not part of the retroactive collapse
	// fold — streaming reasoning is always shown in full, and collapses to the one-liner only
	// once the step COMMITS as an entry (which then follows collapse.Effective).
	styled := renderLiveAssistant(live.Thinking, live.Text, calls, pending, true, width, animState{})
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

// queuedTailEntryID is the reserved provenance id every queued-affordance line carries. Like
// the live tail (liveTailEntryID) a queued row is NOT a committed entry; using a distinct
// sentinel — the max displayID the incrementing allocator (starting at 1) never reaches —
// keeps queued rows from colliding with the live tail's (entry, sub) selection identity OR a
// committed entry's collapse-click, so a stray click on a queued row toggles an id no entry
// owns (a harmless no-op) and a selection spanning queued rows resolves unambiguously.
const queuedTailEntryID displayID = ^displayID(0)

// queuedTailLines renders the FOCUSED loop's pending queued-input affordances — the user
// messages fired while a turn is still running — to viewport lines appended below the live
// tail. It scopes the queue to the focused loop (QueuedInputsFor) so a subagent's queued
// message never leaks under another loop's view, and reuses the dim, "queued"-tagged
// renderQueued (the faint styles.QueuedStyle rows).
// Once a queued message's turn starts it commits as a real user row and drops from the queue
// (startTurnUser → dropQueued), so it never renders both queued and committed. An empty queue
// yields no lines.
func (m Screen) queuedTailLines() []renderedLine {
	styled := renderQueued(m.transcript.QueuedInputsFor(m.focusedLoopID), m.contentWidth())
	if styled == "" {
		return nil
	}
	lines := strings.Split(styled, "\n")
	out := make([]renderedLine, len(lines))
	for i, ln := range lines {
		out[i] = renderedLine{styled: ln, plain: plainFromStyled(ln), entry: queuedTailEntryID, sub: i}
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
//
// The bar shows the loops currently doing work plus two always-kept exceptions:
// activeBarEntries keeps the LIVE loops, the FOCUSED loop (so the current view is always
// labeled), and the ACTIVE loop (the session's current default target), dropping only idle
// loops that are neither. A selection event advances active without moving focus.
func (m Screen) bar() loopBar {
	infos := m.transcript.loops()
	gated := m.interaction.pendingGateLoops()
	active := m.effectiveActiveLoopID()
	entries := activeBarEntries(infos, gated, m.focusedLoopID, active)
	return loopBar{entries: entries, focused: m.focusedLoopID, active: active, max: loopBarCap}
}

// activeBarEntries maps the loop table into bar entries, keeping LIVE loops plus the FOCUSED,
// ACTIVE loops (both kept even when idle); an unrelated idle loop drops off. Entry order
// follows loops() (stable creation order), so the bar draws loops in the order they appeared.
func activeBarEntries(infos []loopInfo, gated map[uuid.UUID]bool, focused, active uuid.UUID) []loopBarEntry {
	entries := make([]loopBarEntry, 0, len(infos))
	for _, li := range infos {
		if !li.Live && li.ID != focused && li.ID != active {
			continue
		}
		entries = append(entries, loopBarEntry{id: li.ID, name: li.Name, live: li.Live, gate: gated[li.ID]})
	}
	return entries
}

// bottomBoxView renders the bottom box for the current interaction mode (the reused composer,
// or a prompt control when a prompt is active) via the shared surface primitive.
func (m Screen) bottomBoxView() string {
	return bottomBox(m.surfaceInputs())
}

// surfaceInputs builds the agent-free snapshot the shared bottom-box + status primitives
// read: the interaction model, the FOCUSED loop's status + live signals, and the frame
// dimensions.
func (m Screen) surfaceInputs() surfaceInputs {
	return surfaceInputs{
		Interaction: m.interaction,
		Status:      m.focusedStatus(),
		StatusState: m.statusInputs(),
		Width:       m.width,
		Height:      m.height,
	}
}

// focusedStatus is the turn-lifecycle Status the status line reflects for the FOCUSED loop
// (design §Status line). It follows the FOCUSED loop's OWN turn liveness, independent of which
// primer is currently active. This matters because the core status (m.status) now tracks the
// ACTIVE loop: reusing it for a different focused loop would wrongly read "running" when that
// focused loop is idle but the active primer is mid-turn. The only exception is the two
// session-global transitions the core owns — StatusInterrupting (an interrupt in flight) and
// StatusResetting (a /clear reopen) — which surface regardless of focus. The ordinary
// Running/Idle is read from the focused loop's per-loop turn bit
// (sessionCore.loopRunning, folded from EVERY loop's TurnStarted/terminal, keyed by loop id).
// That map is the accurate per-loop signal. statusInputs() then refines a Running label into
// thinking…/streaming… from the focused loop's live signals. This is a deliberately MINIMAL
// "you are viewing loop X, and whether it is live" indication, NOT a full per-loop status
// machine: Interrupting/Resetting are session-global transitions.
func (m Screen) focusedStatus() Status {
	if m.status == StatusInterrupting || m.status == StatusResetting {
		return m.status
	}
	focus := m.focusedLoopID
	if m.loopRunning[focus] {
		return StatusRunning
	}
	return StatusIdle
}

// statusInputs snapshots the status signals for the FOCUSED loop: whether its live segment
// is streaming narration or only thinking, and which prompt (if any) is active. The status
// line reflects the focused loop's activity (design §Status line).
func (m Screen) statusInputs() statusInputs {
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

// statusLine is the focused loop's status line for the frame: the shared ANIMATED
// gradient status label (label + dot flow with m.anim.frame — the same phase Screen threads,
// advanced continuously by the anim tick so idle/waiting/thinking/streaming all shimmer) PLUS,
// while the focused loop's turn is running, a faint live-elapsed suffix " (Ns)" / " (Nm Ss)".
// Idle / no active turn → no suffix. The suffix is a single faint Render call so its "(2m 34s)"
// text stays contiguous (substring-findable) rather than split by per-glyph styling, and it is
// deliberately NOT part of the gradient so it reads as a quiet, static timer beside the shimmer.
func (m Screen) statusLine() string {
	line := renderStatusLine(m.focusedStatus(), m.statusInputs(), m.anim.frame)
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
func (m Screen) turnElapsed() (time.Duration, bool) {
	if m.focusedStatus() != StatusRunning {
		return 0, false
	}
	lid := m.focusedLoopID
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
// collapse), one status line, a blank gap, the bottom box, a blank gap, and the active-loops
// bar — and returns a per-frame View with the modern configuration: AltScreen on and
// cell-motion mouse (the v2
// per-frame fields the copy-while-scrolling design turns on), plus the composer's Kitty
// keyboard request (see Screen.View for why). It returns an empty view until the first sized
// frame (avoids a 0×0 first frame).
func (m Screen) View() tea.View {
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
// EXACTLY contentH rows (so the chrome sits at the fixed rows regionAt assumes), then an inert
// blank pad row, the status line, an inert blank gap row, the bottom box, a second inert blank
// gap row, and the active-loops bar at the very bottom. The blank pad row sets the status line
// off the content above it, and the input box sits ABOVE the loop bar, each set off by a blank
// row so the chrome does not read as cramped. The viewport is sized to
// lay.contentH on a LOCAL copy before rendering, so the drawn content region always matches
// the current layout even if the bottom chrome changed since the last Update-side
// resize/rerender. Every line is width-clamped (truncate, never wrap) so no row exceeds the
// frame width. The stacking order MUST match layout()/regionAt exactly.
func (m Screen) composeBody(lay screenLayout) string {
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
	rows = append(rows, "")                                        // inert pad: content → status
	rows = append(rows, m.statusLine())                            // status line
	rows = append(rows, "")                                        // inert gap: status → box
	rows = append(rows, strings.Split(m.bottomBoxView(), "\n")...) // bottom box (input)
	rows = append(rows, "")                                        // inert gap: box → bar
	rows = append(rows, m.bar().Render(m.width))                   // active-loops bar (very bottom)
	return clampSurfaceWidth(strings.Join(rows, "\n"), m.width)
}

// compile-time assertion that Screen is a tea.Model (Init/Update/View, value receiver).
var _ tea.Model = Screen{}
