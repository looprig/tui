package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/cli/tui/styles"
	"github.com/looprig/harness/pkg/event"
)

// Screen is the SCROLLBACK-FIRST presentation shell over the shared sessionCore
// transport (embedded). The core owns event routing — the ONE session-lifetime
// subscription and its lifecycle, the dispatch of each event into the transcript +
// interaction reducers, the primary-loop turn status, the /clear reopen ordering, and
// the submit/interrupt/gate command wiring. Screen adds ONLY the scrollback
// presentation: it prints each committed entry to native terminal scrollback exactly
// once (scrollbackModel), freezes the just-finished response in place while the next
// turn streams (heldLines), spills a long live tail to scrollback as it grows
// (liveSpill), and animates the live surface (anim) + rotates a tip. There is no
// transcript viewport — the terminal owns history.
//
// Update delegates transport to the core, then does its scrollback flush; View composes
// the active surface (unspilled live tail + bottom box + status line) and leaves
// AltScreen off and the mouse uncaptured — the scrollback-first configuration. User
// rows are EVENT-DRIVEN (committed by the transcript reducer from the loop's
// TurnStarted/TurnFoldedInto Message, genuine input only), never optimistically at
// submit; a successful submit only records the queued affordance (RecordSubmit), shown
// once InputQueued arrives.
type Screen struct {
	sessionCore

	scrollback scrollbackModel
	liveSpill  liveSpillState

	// heldLines is the rolling HELD TAIL: committed scrollback lines (already spill-trimmed
	// and marked printed by flushActions) rendered in the active surface, above the live
	// tail, instead of being emitted to native scrollback immediately. It is the
	// freeze-in-place mechanism. Emitting a finalized step to scrollback the instant the
	// live tail collapses shrinks the managed region by the step's full height in one frame;
	// the inline renderer repaints the shorter surface from its top, springing the input box
	// upward and stranding blank rows below it (the "input jumped to the top" symptom) — at
	// commit AND, if released all at once, at the next turn's start. Instead the held lines
	// spill to scrollback from the TOP one line at a time as the live tail grows (spillHeld),
	// so the surface height stays constant and the input box never jumps: the previous
	// response and the new user row scroll up into history exactly as fast as the new
	// response fills in below them. While idle it holds the last response frozen above the
	// input, capped at the live-tail budget. nil when nothing is held.
	heldLines []string

	// expand drives the ctrl+t fold for the THINKING block only. It defaults to TRUE
	// (full "│ " thinking body) because native scrollback is append-only: an entry prints
	// once and can never be retroactively re-rendered, so a toggle cannot expand thinking
	// already committed to history; showing it full by default avoids permanently
	// truncating that history. ctrl+t flips it to the compact "thinking · N lines" summary
	// for the live tail + future commits. Tool RESULT output is NOT governed by this flag —
	// it is hard-capped to previewLineCap lines always (see render.go), so a huge result
	// can't fill the live tail or strand a commit-time scrollback gap.
	expand        bool
	width, height int
	ready         bool

	// startupPending/startupCommitted coordinate the opening banner with Bubble Tea's
	// first sized frame. systemReadyMsg may arrive before WindowSizeMsg. The startup
	// entries are committed once, kept unprinted while idle, and rendered in the active
	// surface until the first real scrollback flush carries them into native history.
	startupPending   bool
	startupCommitted bool
	startupEntryIDs  []displayID

	// anim holds the LIVE-surface animation state (blink phase + spinner frame +
	// ticking guard). It is advanced once per blinkTick while Running, threaded into
	// renderLiveTail (its blink phase pulses the live assistant dot) AND the status line
	// (its frame counter flows the status-line gradient — label and dot), and reset to its
	// zero value when the turn ends. The committed scrollback path never consults it. See
	// animState and the blinkMsg handler.
	anim animState

	// tip is the rotating educational hint shown faint below the status line. It is
	// seeded at construction and refreshed (nextTip) on every turn terminal, so a fresh
	// hint shows after each turn.
	tip string
}

// liveSpillState tracks the prefix of the current live assistant prose/thinking that
// has already been emitted to native scrollback while the step is still streaming.
// StepDone later suppresses the same prefix from the authoritative committed assistant
// entry, avoiding a duplicate answer while still letting long streams free-flow.
type liveSpillState struct {
	printedLines []string
	finalizing   bool
}

// AgentBanner is the agent metadata shown as the startup info notice — its Name and
// Description, threaded in at construction from the composition root (cmd/swe) so the
// Agent interface need not expose them. The zero value renders a name-less banner;
// bannerText degrades gracefully when either field is empty.
type AgentBanner struct {
	Name        string
	Description string

	// Greeting is the OPTIONAL, UI-only startup greeting (§5a): a deterministic, already-
	// built capability description (composed by the composition root from the agent
	// registry — never the model). When non-empty it is committed as a SECOND opening
	// transcript notice, after the banner, by the systemReady handler. It is purely a
	// rendered opening entry — NOT a turn, NOT a command, never in the model's context —
	// so the primary loop's history stays empty until the first real user message. Empty
	// (the default-off case) → no greeting entry, behavior identical to today.
	Greeting string
}

// bannerText renders the startup banner line from the agent metadata: "<Name> —
// <Description>" when both are present, just the Name when the description is empty,
// just the Description when the name is empty, and a neutral fallback when both are
// empty (the notice still marks the session start). It degrades rather than emitting
// a dangling separator.
func (b AgentBanner) bannerText() string {
	name, desc := strings.TrimSpace(b.Name), strings.TrimSpace(b.Description)
	switch {
	case name != "" && desc != "":
		return name + " — " + desc
	case name != "":
		return name
	case desc != "":
		return desc
	default:
		return "session ready"
	}
}

// New constructs an idle Screen driving agent, with open as the /clear thunk and
// banner the agent name/description shown as the startup info notice. The expand flag
// starts TRUE so the thinking block renders in full from the start — see the field
// comment for why append-only scrollback forces expanded-by-default (tool output is
// hard-capped independently of this flag).
func New(ctx context.Context, agent Agent, open OpenAgent, banner AgentBanner) Screen {
	return Screen{
		sessionCore: newSessionCore(ctx, agent, open, banner, defaultLoopFilter),
		scrollback:  newScrollbackModel(0),
		expand:      true,
		tip:         nextTip(""),
	}
}

// Init focuses the composer (starting the cursor blink), emits the initial
// startup-banner entry, schedules the cold-restore repaint, AND attaches the
// session-lifetime event subscription. The subscription is the single LIVE event
// source for the whole session; subscribeCmd's subscribedMsg installs it and starts
// the continuous reader. restoreBacklogCmd folds a RESTORED session's historical
// Enduring backlog OFF the update loop and repaints it once (restoredMsg) before any
// live event drives the transcript — a new session's empty backlog makes it a no-op.
// Both run as independent background commands: for a cold restore the loop comes up
// idle, so no live event arrives until a user Submit, and there is no backlog/live
// overlap to dedup (the live-attach overlap case is deferred).
func (m Screen) Init() tea.Cmd {
	return tea.Batch(
		m.interaction.input.Focus(),
		func() tea.Msg { return systemReadyMsg{} },
		restoreBacklogCmd(m.appCtx, m.agent, m.agent.PrimaryLoopID()),
		m.subscribe(),
	)
}

// Update advances the model. It is a value receiver so Screen satisfies tea.Model;
// the mutating handlers take a pointer to the addressable receiver and the updated
// value is returned.
func (m Screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// A resize must NOT print to scrollback (see TestWindowSizeMsgNoScrollbackPrint): the
		// scrollback-strand fix depends on resize only re-rendering the managed region. The
		// held tail therefore rides the resize in place — its stored lines are at the old
		// width, so a narrower terminal truncates them until the next event drains them, a
		// minor transient bounded by the tail budget (never a strand).
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.scrollback.width = msg.Width
		m.interaction.input.Resize(msg.Width)
		if m.startupPending && !m.startupCommitted {
			return m, m.commitStartup()
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case eventMsg:
		return m, m.handleEvent(msg.ev)
	case restoredMsg:
		return m, m.handleRestored(msg)
	case subscribedMsg:
		return m, m.handleSubscribed(msg)
	case subClosedMsg:
		return m, m.handleSubClosed(msg)
	case submitResultMsg:
		return m, m.handleSubmitResult(msg)
	case interruptResultMsg:
		return m, m.handleInterruptResult(msg)
	case reopenResultMsg:
		return m, m.handleReopenResult(msg)
	case promptResultMsg:
		return m, m.handlePromptResult(msg)
	case systemReadyMsg:
		return m, m.handleSystemReady()
	case blinkMsg:
		return m, m.handleBlink()
	}
	return m, nil
}

// handleSystemReady commits the opening transcript once the active surface has its
// first terminal size. If Bubble Tea delivers systemReadyMsg before WindowSizeMsg, the
// commit is deferred so the banner can render in the first managed frame instead of
// landing one row outside the active screen.
func (m *Screen) handleSystemReady() tea.Cmd {
	if m.startupCommitted {
		return nil
	}
	if !m.ready {
		m.startupPending = true
		return nil
	}
	return m.commitStartup()
}

// commitStartup commits the opening transcript entries once per session: the startup
// banner (always) followed by the OPTIONAL greeting (only when banner.Greeting is
// non-empty/non-blank). Both are committed via the plain info-notice path — they are
// rendered opening entries, NOT turns or commands: this never calls Submit, never
// drives a loop, and never enters the model's context. It does NOT flush immediately:
// while idle, renderStartupSurface shows these unprinted entries in the active surface;
// the first real transcript commit flushes them into native scrollback exactly once.
func (m *Screen) commitStartup() tea.Cmd {
	if m.startupCommitted {
		return nil
	}
	m.startupPending = false
	m.startupCommitted = true

	start := len(m.transcript.committed)
	m.transcript = m.transcript.CommitNotice(noticeInfo, m.banner.bannerText())
	if greeting := strings.TrimSpace(m.banner.Greeting); greeting != "" {
		m.transcript = m.transcript.CommitNotice(noticeInfo, greeting)
	}
	m.startupEntryIDs = m.startupEntryIDs[:0]
	for _, e := range m.transcript.committed[start:] {
		m.startupEntryIDs = append(m.startupEntryIDs, e.ID)
	}
	return nil
}

// handleBlink advances the live-surface animation by one frame and, ONLY while the
// turn is still Running, reschedules the next tick — so View re-renders the live tail
// with the new blink/spinner phase. It is a PURE active-surface re-render: it never
// calls flush/printToScrollback/subNext, so a tick can never write to scrollback.
// At any non-Running status it stops the loop (returns nil — no reschedule) and resets
// the animation state so the next live render is clean and a fresh turn starts a new
// tick loop. Reset clears the ticking guard, letting the next Running turn start one.
func (m *Screen) handleBlink() tea.Cmd {
	if m.status != StatusRunning {
		m.anim = m.anim.reset()
		return nil
	}
	m.anim = m.anim.advance()
	return blinkTick()
}

// handleEvent delegates one subscription event to the shared core (which routes it
// through BOTH reducers — the transcript and the interaction model — derives the turn
// STATUS from the primary loop's turn-lifecycle events, and re-arms the continuous
// reader) and then does the SCROLLBACK presentation: it reacts to the turn phase
// (blink/tip), flushes any newly committed entries to the held tail / scrollback, and
// spills a long live tail. Reading continues unconditionally: the loop is blocked on the
// permission GATE, not the stream, so the user's keypress releases it. While a prompt
// gate is active (scrollbackGated) every commit is HELD in the surface rather than
// emitted to scrollback — writing to scrollback while the box is up strands it into
// native history — and the held tail drains via the normal spill path once the gate
// resolves.
func (m *Screen) handleEvent(ev event.Event) tea.Cmd {
	finalizingLive := m.finalizesPrimaryLive(ev)
	rearm, phase := m.sessionCore.handleEvent(ev)
	statusCmd := m.reactTurnPhase(phase)
	if finalizingLive {
		m.liveSpill.finalizing = true
	}
	// Route newly committed entries into the held tail (rendered in the surface) rather
	// than emitting them to scrollback, then drain the held tail from the top to fit the
	// budget as the live tail grows. spillHeld runs BEFORE spillLive so the older held lines
	// reach scrollback ahead of the newer live-prose overflow, keeping history in order.

	// While a prompt gate owns the bottom surface, emit NOTHING to scrollback: the
	// permission/AskUser box (and the "awaiting approval" status line above it) live in
	// the managed region, and any scrollback write — printToScrollback → insertAbove —
	// repaints and strands that box into history. Because reading continues during a
	// gate (the loop is blocked on the gate, not the stream), sibling/subagent events
	// keep committing entries; without this guard each commit strands another copy of the
	// box and status line (the "awaiting approval shown multiple times, box never went
	// away" symptom). So hold every newly committed entry in the surface and suppress
	// both spill paths; the held tail drains normally on the next event once the gate has
	// resolved (the prompt is popped optimistically on the decision keypress, so the
	// resuming turn's events run the unguarded hold/spill path).
	if m.scrollbackGated() {
		m.heldLines = append(m.heldLines, flattenActions(m.flushActions())...)
		return tea.Batch(rearm, statusCmd)
	}

	holdCmd := m.hold(finalizingLive)
	heldSpillCmd := m.spillHeld()
	spillCmd := m.spillLive()
	return tea.Batch(rearm, statusCmd, holdCmd, heldSpillCmd, spillCmd)
}

// reactTurnPhase is the scrollback-only presentation reaction to the turn-status
// transition the core just derived: the primary loop's TurnStarted (turnStarted) arms
// the live-surface blink, and its terminal (turnEnded) rotates the educational tip so a
// fresh hint shows after each turn. The status STATE itself was already set on the core;
// this only drives Screen's own animation + tip. A subagent loop's turn events and
// non-turn events (turnUnchanged) leave both untouched.
func (m *Screen) reactTurnPhase(phase turnPhase) tea.Cmd {
	switch phase {
	case turnStarted:
		return m.startBlink()
	case turnEnded:
		m.tip = nextTip(m.tip) // rotate the hint after each completed turn
	}
	return nil
}

// scrollbackGated reports whether a prompt gate (permission or AskUser) currently owns
// the bottom surface. While a prompt box is up it lives in the managed region, so any
// scrollback write repaints and strands it into native history — the input box's
// managed region cannot be committed while the terminal is still using part of it to
// show the box. handleEvent and flush consult this to hold every commit until the gate
// resolves, at which point the queued handoff drains gradually via the normal spill
// path. Fail-secure: when in doubt (a prompt is present) we withhold the write.
func (m Screen) scrollbackGated() bool {
	return m.activePrompt() != nil
}

// hold moves newly committed entries into the held tail (rendered in the surface) instead
// of emitting them to scrollback, so they hand off to native history gradually (spillHeld)
// as new content grows — the freeze-in-place mechanism. A finalized step is ALWAYS held so
// the live tail's collapse cannot jump the input; other commits are held only when
// something is already held, to preserve scrollback order and continue a gradual handoff.
// With nothing held and no finalized step — the session's opening entries (startup banner,
// first user row) — there is no live tail to collapse and no ordering to preserve, so they
// go straight to scrollback rather than being delayed.
func (m *Screen) hold(finalizing bool) tea.Cmd {
	actions := m.flushActions()
	if !finalizing && len(m.heldLines) == 0 {
		return printToScrollback(actions)
	}
	m.heldLines = append(m.heldLines, flattenActions(actions)...)
	return nil
}

// spillHeld drains the held tail to native scrollback from the TOP (oldest first) so the
// held lines plus the live tail fit the live-tail budget. As a new turn streams, the
// previous frozen response and the new user row spill up into history one line at a time
// while the new response grows below them — the surface height stays constant, so the input
// box does not jump. While idle (no live tail) it caps the frozen tail at the budget,
// flushing only the overflow of a step that arrived taller than the surface (e.g. a
// non-streamed StepDone whose spill never ran). Returns the overflow print command, or nil
// when the held tail already fits.
func (m *Screen) spillHeld() tea.Cmd {
	if len(m.heldLines) == 0 {
		return nil
	}
	avail := liveTailDisplayCap(m.surfaceInputs("")) - m.liveTailHeight()
	if avail < 0 {
		avail = 0
	}
	if len(m.heldLines) <= avail {
		return nil
	}
	n := len(m.heldLines) - avail
	overflow := append([]string(nil), m.heldLines[:n]...)
	m.heldLines = append([]string(nil), m.heldLines[n:]...)
	return printToScrollback([]printAction{{Lines: overflow}})
}

// liveTailHeight is the physical row count of the currently rendered live tail (0 when
// there is none) — the rows spillHeld reserves for the live segment when budgeting the
// held tail.
func (m Screen) liveTailHeight() int {
	live := m.renderLiveTail()
	if live == "" {
		return 0
	}
	return strings.Count(live, "\n") + 1
}

// flattenActions concatenates every action's Lines, in order, into one slice — the row
// view the held tail is stored and budgeted as.
func flattenActions(actions []printAction) []string {
	var lines []string
	for _, a := range actions {
		lines = append(lines, a.Lines...)
	}
	return lines
}

func (m Screen) finalizesPrimaryLive(ev event.Event) bool {
	if ev.EventHeader().LoopID != m.agent.PrimaryLoopID() {
		return false
	}
	switch ev.(type) {
	case event.StepDone, event.TurnDone, event.TurnInterrupted, event.TurnFailed:
		return true
	default:
		return false
	}
}

// handleSubscribed installs the session-lifetime subscription via the core and starts
// the continuous reader. On a non-nil err the core commits a fatal error entry (the user
// sees the failure rather than a silently dead surface) and Screen flushes it; on success
// the core stores the stream and returns subNext, the single reader that drives every
// subsequent event.
func (m *Screen) handleSubscribed(msg subscribedMsg) tea.Cmd {
	cmd, present := m.sessionCore.applySubscribed(msg)
	if present {
		return m.flush()
	}
	return cmd
}

// handleSubClosed reacts to the continuous reader observing a closed channel. A nil err
// is an intentional Close (a /clear swap or quit teardown) — nothing to surface. A
// non-nil err is a hub-forced loss (egress overflow); the core commits an error entry so
// the user learns the live stream was dropped rather than silently stalling, and Screen
// flushes it.
func (m *Screen) handleSubClosed(msg subClosedMsg) tea.Cmd {
	if m.sessionCore.applySubClosed(msg) {
		return m.flush()
	}
	return nil
}

// handleSubmitResult surfaces a fire-and-forget Submit outcome. On success the core
// records the submit (RecordSubmit) under the loop-assigned InputID so the queued
// affordance can show the remembered blocks once the loop's InputQueued event arrives;
// the authoritative user row is committed later from the loop's TurnStarted/
// TurnFoldedInto Message, NOT here — so success commits nothing and Screen does not
// flush. A non-nil err commits a faint, NON-FATAL error entry the shell flushes.
func (m *Screen) handleSubmitResult(msg submitResultMsg) tea.Cmd {
	if m.sessionCore.applySubmitResult(msg) {
		return m.flush()
	}
	return nil
}

// flush renders every newly committed transcript entry to scrollback exactly once and
// returns the print command (nil when nothing is new). renderEntry is given the current
// expand flag and width so scrollback and the live tail render identically. Any HELD tail
// is released to scrollback first (then the new entries, in committed order): an explicit
// flush means fresh out-of-band content is being committed (an error, a notice, /help, the
// build-error user row), so the held tail hands off to history rather than staying stranded
// in the surface. The freeze-aware turn path uses hold + spillHeld (which keep content in
// the surface and drain it gradually); flush is the direct path for those out-of-band
// commits, where an immediate handoff is acceptable.
func (m *Screen) flush() tea.Cmd {
	actions := flattenActions(m.flushActions())
	// A prompt gate owns the surface — releasing to scrollback now would strand the
	// box (see scrollbackGated). Hold the out-of-band entry in the surface instead
	// (where it still renders, above the box) and let it drain once the gate resolves.
	if m.scrollbackGated() {
		m.heldLines = append(m.heldLines, actions...)
		return nil
	}
	lines := append(append([]string(nil), m.heldLines...), actions...)
	m.heldLines = nil
	return printToScrollback([]printAction{{Lines: lines}})
}

func (m *Screen) flushActions() []printAction {
	pendingSpill := append([]string(nil), m.liveSpill.printedLines...)
	var actions []printAction
	m.scrollback, actions = m.scrollback.Flush(m.transcript.committed, func(e entry) []string {
		lines := renderEntry(e, m.expand, m.width)
		if m.liveSpill.finalizing && e.Kind == kindAssistant && len(pendingSpill) > 0 {
			lines, pendingSpill = trimMatchingPrefix(lines, pendingSpill)
		}
		return lines
	})
	if m.liveSpill.finalizing {
		m.liveSpill = liveSpillState{}
	}
	return actions
}

func trimMatchingPrefix(lines, prefix []string) ([]string, []string) {
	n := matchingPrefixCount(lines, prefix)
	return lines[n:], prefix[n:]
}

func matchingPrefixCount(lines, prefix []string) int {
	n := 0
	for n < len(lines) && n < len(prefix) && lines[n] == prefix[n] {
		n++
	}
	return n
}

// handleInterruptResult applies the outcome of an Interrupt call via the core. On error
// the turn may still be live, so the core returns to Running and commits a faint error
// entry the shell flushes; on success it stays Interrupting — the loop's TurnInterrupted
// terminal on the subscription (applyTurnStatus, primary loop) returns it to Idle.
func (m *Screen) handleInterruptResult(msg interruptResultMsg) tea.Cmd {
	if m.sessionCore.applyInterruptResult(msg) {
		return m.flush()
	}
	return nil
}

// handleReopenResult applies a /clear reopen outcome (the model is Resetting). The core
// owns the transport ordering — on error the old agent is kept and it returns to Idle
// with an error entry the shell flushes; on success it closes the OLD subscription,
// swaps in the fresh agent, resets the transcript/interaction/status, and re-subscribes
// against the fresh agent (the swap happens BEFORE subscribeCmd is built). Screen then
// resets its own scrollback presentation to match the fresh session. Already-printed
// scrollback stays in the terminal (native history is append-only); the print-once
// engine is reset so a fresh session starts a clean surface.
func (m *Screen) handleReopenResult(msg reopenResultMsg) tea.Cmd {
	cmd, present := m.sessionCore.applyReopenResult(msg)
	if present {
		return m.flush()
	}
	// Successful reopen: the core reset the shared transport; reset the scrollback
	// presentation too so no held/spilled remnant of the old session survives the swap.
	m.scrollback = newScrollbackModel(m.width)
	m.liveSpill = liveSpillState{}
	m.heldLines = nil
	m.startupPending = false
	m.startupCommitted = false
	m.startupEntryIDs = nil
	return cmd
}

// handlePromptResult surfaces a bounded prompt-dispatch outcome via the core. A nil err
// is a silent success (the gate released; the next events arrive on the stream). A
// non-nil err commits a faint, NON-FATAL error entry the shell flushes: the prompt was
// already optimistically popped, and a terminal event clears any siblings — this only
// adds a record.
func (m *Screen) handlePromptResult(msg promptResultMsg) tea.Cmd {
	if m.sessionCore.applyPromptResult(msg) {
		return m.flush()
	}
	return nil
}

// startBlink starts the live-surface animation tick loop iff one is not already
// running: it sets the ticking guard and returns the first blinkTick. If a tick is
// already in flight (anim.ticking) — e.g. a fresh TurnStarted arrives before the
// prior loop has observed Idle and reset — it returns nil so no second, parallel
// loop is spawned. The single in-flight tick keeps ticking (it observes Running), so
// the animation continues seamlessly across back-to-back turns.
func (m *Screen) startBlink() tea.Cmd {
	if m.anim.ticking {
		return nil
	}
	m.anim.ticking = true
	return blinkTick()
}

// View renders an empty string until the first WindowSizeMsg (avoids a 0×0 first
// frame), then composes the active surface via surfaceView: the unspilled live tail,
// the bottom box (composer / prompt control / answer field by interaction mode), the
// slash panel when visible, and one status line. Committed entries are NOT re-rendered
// here — they live in native scrollback. The View leaves AltScreen false and MouseMode
// none (the v2 zero values), the scrollback-first configuration.
//
// KeyboardEnhancements.ReportAllKeysAsEscapeCodes is requested so the composer's
// Shift+Enter binding works (see tui/components/input.go). v2's DEFAULT Kitty request
// is just flag 1 ("disambiguate escape codes"), under which the Kitty spec keeps
// Enter/Tab/Backspace as their legacy bytes — so a modified Enter (Shift+Enter) is NOT
// reported distinctly and arrives as a plain Enter (submit). Flag 8 ("report all keys
// as escape codes") is the one that makes the terminal report a modified Enter as
// CSI 13;2u, which v2 decodes to KeyEnter+ModShift. This ONLY helps on terminals that
// implement the Kitty keyboard protocol (kitty, Ghostty, WezTerm, foot, Alacritty,
// recent iTerm2 with the option enabled). On terminals WITHOUT it (Apple Terminal,
// many VS Code setups) Shift+Enter is indistinguishable from Enter no matter what we
// request — those rely on the Ctrl+J fallback (input.go). The request is inert on
// non-supporting terminals (they ignore the CSI), and it does NOT enable alt-screen,
// mouse capture, or focus reporting, so the scrollback-first invariant is preserved.
func (m Screen) View() tea.View {
	if !m.ready {
		return tea.NewView("")
	}
	v := tea.NewView(surfaceView(m.surfaceInputs(m.tailView())))
	v.KeyboardEnhancements.ReportAllKeysAsEscapeCodes = true
	return v
}

// tailView is the active surface's tail content: the HELD tail (committed lines not yet in
// scrollback — the just-finished response and, mid-handoff, the new user row) stacked above
// the streaming live tail. Held and live share the tail region; spillHeld keeps their
// combined height within the budget by draining the held lines from the top as the live
// tail grows, so the input box stays put. While idle the held tail alone is the frozen
// response above the input; while streaming with nothing held it is just the live tail. The
// trailing blank each committed entry carries separates held from live (and is trimmed at
// the very end — surfaceView supplies the tail's own trailing blank).
func (m Screen) tailView() string {
	held := strings.Join(m.heldLines, "\n")
	live := m.renderLiveTail()
	var combined string
	switch {
	case held == "":
		combined = live
	case live == "":
		combined = held
	default:
		combined = held + "\n" + live
	}
	return strings.TrimRight(combined, "\n")
}

func (m Screen) surfaceInputs(liveTail string) surfaceInputs {
	return surfaceInputs{
		Interaction: m.interaction,
		Startup:     m.renderStartupSurface(),
		LiveTail:    liveTail,
		Queued:      m.renderQueued(),
		Status:      m.status,
		StatusState: m.statusInputs(),
		Phase:       m.anim.frame,
		Tip:         m.tip,
		Width:       m.width,
		Height:      m.height,
	}
}

// renderStartupSurface renders the committed-but-not-yet-printed startup entries in
// the active surface. Once any flush prints those entries to native scrollback, this
// returns empty so there is never a second visible banner.
func (m Screen) renderStartupSurface() string {
	if len(m.startupEntryIDs) == 0 {
		return ""
	}
	lines := make([]string, 0, len(m.startupEntryIDs)*2)
	for _, id := range m.startupEntryIDs {
		if m.scrollback.printed[id] {
			continue
		}
		e, ok := m.committedByID(id)
		if !ok {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, renderEntry(e, m.expand, m.width)...)
	}
	return strings.Join(lines, "\n")
}

func (m Screen) committedByID(id displayID) (entry, bool) {
	for _, e := range m.transcript.committed {
		if e.ID == id {
			return e, true
		}
	}
	return entry{}, false
}

// renderLiveTail renders the in-progress assistant segment (streamed thinking,
// narration, any still-running tool cards, and any in-flight nested Subagent cards) to
// its display lines. It is empty when there is no live content AND no pending subagent
// card, so the surface omits the tail region entirely.
func (m Screen) renderLiveTail() string {
	spilled := matchingPrefixCount(m.spillableAssistantLines(), m.liveSpill.printedLines)
	return dropLiveSpillPrefix(m.renderLiveTailFull(), spilled)
}

func (m Screen) renderLiveTailFull() string {
	live := m.transcript.live
	pending := m.transcript.pendingSubagentCards()
	if live.empty() && len(pending) == 0 {
		return ""
	}
	// Suppress the orchestrator's raw running Subagent tool card — its activity is shown
	// by the nested pending card (renderSubagentCard) instead, so it must not be doubled.
	calls := nonSubagentCalls(live.Calls)
	return renderLiveAssistant(live.Thinking, live.Text, calls, pending, m.expand, m.width, m.anim)
}

func (m *Screen) spillLive() tea.Cmd {
	if !m.ready || m.liveSpill.finalizing {
		return nil
	}
	fullTail := m.renderLiveTailFull()
	if fullTail == "" {
		return nil
	}
	assistantLines := m.spillableAssistantLines()
	if len(assistantLines) == 0 {
		return nil
	}
	tailBudget := liveTailDisplayCap(m.surfaceInputs(""))
	fullTailLines := strings.Split(fullTail, "\n")
	spillUntil := len(fullTailLines) - tailBudget
	if spillUntil > len(assistantLines) {
		spillUntil = len(assistantLines)
	}
	already := len(m.liveSpill.printedLines)
	if spillUntil <= already {
		return nil
	}
	newLines := append([]string(nil), assistantLines[already:spillUntil]...)
	m.liveSpill.printedLines = append(m.liveSpill.printedLines, newLines...)
	return printToScrollback([]printAction{{Lines: newLines}})
}

func (m Screen) spillableAssistantLines() []string {
	live := m.transcript.live
	if live.Thinking == "" && live.Text == "" {
		return nil
	}
	// The spill renders the STILL-STREAMING live segment, so its reasoning header is the
	// present-tense "thinking" (styles.ThinkingHeader) — the same header renderLiveAssistant
	// uses — so the spilled prefix matches the live tail. It flips to "thought for Nsec" only
	// when StepDone commits the step and renderEntry repaints it.
	return splitNonEmpty(renderAssistant(live.Thinking, live.Text, "", m.expand, m.width, styles.ThinkingHeader))
}

func dropLiveSpillPrefix(tail string, n int) string {
	if tail == "" || n <= 0 {
		return tail
	}
	lines := strings.Split(tail, "\n")
	if n >= len(lines) {
		return ""
	}
	lines = lines[n:]
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

// renderQueued renders the transcript's pending queued-input affordances (the
// submitted-but-not-yet-running user messages) to their dim display lines, shown
// below the live tail. It is empty when nothing is queued, so the surface omits the
// region entirely.
func (m Screen) renderQueued() string {
	return renderQueued(m.transcript.QueuedInputs(), m.width)
}

// statusInputs snapshots the live signals the status label is derived from: which
// prompt (if any) is active, and whether the live segment is streaming narration or
// only thinking so far.
func (m Screen) statusInputs() statusInputs {
	in := statusInputs{
		streaming: m.transcript.live.Text != "",
		thinking:  m.transcript.live.Text == "" && m.transcript.live.Thinking != "",
	}
	if p := m.activePrompt(); p != nil {
		in.permissionActive = p.Kind == promptPermission
		in.userInputActive = p.Kind == promptUserInput
	}
	return in
}

// handleKey routes a key press. ctrl+c (quit) and ctrl+t (toggle expand) are GLOBAL
// — they fire even with a prompt open — so they are handled first, and both are
// scrollback presentation (quit flushes the held tail; expand is Screen's fold state).
// Esc with no active prompt interrupts a running turn via the core. Every other key is
// delegated to the interaction model, which returns the next model, a typed uiAction,
// and the editor's cursor-blink Cmd; the core's mapAction turns the action into the
// agent-driving command and reports whether it committed an out-of-band entry the shell
// must flush; the blink Cmd is batched in so the cursor keeps blinking in compose and
// free-text answer modes.
func (m *Screen) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Close the session subscription best-effort so it does not leak past quit.
		// Close is a synchronous, idempotent in-process teardown (it just closes the
		// egress channel under a lock), so it is safe to call inline here; the agent
		// close is the bounded async cmd that may block.
		if m.sub != nil {
			_ = m.sub.Close()
			m.sub = nil
		}
		// Flush the held tail to native scrollback BEFORE quitting: it lives in the managed
		// region, which the renderer erases on close, so without this the last response
		// would vanish from the terminal on exit. Sequenced ahead of the close so the
		// tea.Println lands while the renderer is still live.
		cmds := make([]tea.Cmd, 0, 3)
		if len(m.heldLines) > 0 {
			cmds = append(cmds, printToScrollback([]printAction{{Lines: m.heldLines}}))
			m.heldLines = nil
		}
		cmds = append(cmds, closeAgent(m.agent), tea.Quit)
		return *m, tea.Sequence(cmds...)
	case "ctrl+t":
		m.expand = !m.expand // pure display state; default expanded, so this collapses first
		return *m, nil
	case "esc":
		// With no prompt active, Esc keeps its legacy meaning — interrupt a running
		// turn (a no-op otherwise). When a prompt IS active the interaction model owns
		// Esc (deny in permission mode, interrupt in choice/answer mode), so fall through.
		if m.activePrompt() == nil {
			return *m, m.sessionCore.interruptRunning()
		}
	}

	var action uiAction
	var blink tea.Cmd
	m.interaction, action, blink = m.interaction.Update(msg)
	cmd, present := m.sessionCore.mapAction(action)
	var flushCmd tea.Cmd
	if present {
		flushCmd = m.flush()
	}
	return *m, tea.Batch(cmd, flushCmd, blink)
}
