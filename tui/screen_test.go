package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/tool"
)

// compile-time assertion that ModernScreen satisfies tea.Model (a value receiver, so the
// concrete value — not a pointer — is the model the runtime drives).
var _ tea.Model = ModernScreen{}

// updateModern drives m.Update with msg and returns the concrete ModernScreen plus the
// cmd, failing the test if the model is not a ModernScreen. It mirrors updateScreen.
func updateModern(t *testing.T, m ModernScreen, msg tea.Msg) (ModernScreen, tea.Cmd) {
	t.Helper()
	model, cmd := m.Update(msg)
	got, ok := model.(ModernScreen)
	if !ok {
		t.Fatalf("Update returned %T, want ModernScreen", model)
	}
	return got, cmd
}

// newModernSized builds a ModernScreen over agent and gives it a first terminal size, the
// common starting point for the viewport tests (ready + a sized viewport).
func newModernSized(t *testing.T, agent Agent, w, h int) ModernScreen {
	t.Helper()
	m := NewModern(context.Background(), agent, fakeOpen(agent), AgentBanner{})
	m, _ = updateModern(t, m, tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

// feedModern drives one synthetic stream event through Update and returns the new model.
func feedModern(t *testing.T, m ModernScreen, ev event.Event) ModernScreen {
	t.Helper()
	m, _ = updateModern(t, m, eventMsg{ev: ev})
	return m
}

// plainAll joins every rendered line's ANSI-free plain text, so a test can assert the
// viewport rendered a committed/live string without matching styled bytes.
func plainAll(lines []renderedLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.plain)
		b.WriteByte('\n')
	}
	return b.String()
}

// containsPlain reports whether any rendered line's plain text contains sub.
func containsPlain(lines []renderedLine, sub string) bool {
	return strings.Contains(plainAll(lines), sub)
}

// queuedLines returns the viewport lines carrying the queued-affordance provenance
// (queuedTailEntryID) — the modern per-loop queued rows rendered below the live tail.
func queuedLines(lines []renderedLine) []renderedLine {
	var out []renderedLine
	for _, l := range lines {
		if l.entry == queuedTailEntryID {
			out = append(out, l)
		}
	}
	return out
}

// TestModernUpdateRoutesEventToTranscriptAndViewport pins the shell's core wiring: an event
// routed through Update reaches the embedded sessionCore's transcript AND re-renders the
// focused projection into the viewport's lines.
func TestModernUpdateRoutesEventToTranscriptAndViewport(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	before := len(m.transcript.committed)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", "hello from the agent")))

	if len(m.transcript.committed) <= before {
		t.Fatalf("committed did not grow: before=%d after=%d", before, len(m.transcript.committed))
	}
	if len(m.viewport.lines) == 0 {
		t.Fatal("viewport lines empty after committing an assistant entry")
	}
	if !containsPlain(m.viewport.lines, "hello") {
		t.Errorf("viewport lines missing committed text; got %q", plainAll(m.viewport.lines))
	}
}

// TestModernRendersLiveSegment locks that the in-progress live segment (streamed narration)
// is rendered into the viewport after the committed entries — the live tail reuses the
// existing renderer.
func TestModernRendersLiveSegment(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	m = feedModern(t, m, event.TokenDelta{Header: hdr(primary), Chunk: &content.TextChunk{Text: "streaming words"}})

	if len(m.transcript.committed) != 0 {
		t.Fatalf("committed = %d, want 0 (live text is not committed yet)", len(m.transcript.committed))
	}
	if !containsPlain(m.viewport.lines, "streaming") {
		t.Errorf("viewport lines missing live narration; got %q", plainAll(m.viewport.lines))
	}
}

// TestModernViewAltScreenAndMouse pins the modern View configuration: an unsized model
// yields the empty, non-alt-screen frame; a sized model yields the alt-screen + cell-motion
// mouse frame the copy-while-scrolling design requires.
func TestModernViewAltScreenAndMouse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sized     bool
		wantAlt   bool
		wantMouse tea.MouseMode
		wantEmpty bool
	}{
		{name: "sized frame is alt-screen + cell motion", sized: true, wantAlt: true, wantMouse: tea.MouseModeCellMotion},
		{name: "unsized frame is inert", sized: false, wantAlt: false, wantMouse: tea.MouseModeNone, wantEmpty: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: callID(1)}
			m := NewModern(context.Background(), agent, fakeOpen(agent), AgentBanner{})
			if tt.sized {
				m, _ = updateModern(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
			}
			v := m.View()
			if v.AltScreen != tt.wantAlt {
				t.Errorf("AltScreen = %v, want %v", v.AltScreen, tt.wantAlt)
			}
			if v.MouseMode != tt.wantMouse {
				t.Errorf("MouseMode = %v, want %v", v.MouseMode, tt.wantMouse)
			}
			if tt.wantEmpty && v.Content != "" {
				t.Errorf("Content = %q, want empty before first size", v.Content)
			}
		})
	}
}

// TestModernWindowSizeSizesViewport pins that a WindowSizeMsg stores the dimensions and sizes
// the viewport to the layout's content region (frame less the status line, the loop bar, and
// the bottom box).
func TestModernWindowSizeSizesViewport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		w, h int
	}{
		{name: "standard 80x24", w: 80, h: 24},
		{name: "large 120x50", w: 120, h: 50},
		{name: "tiny 10x3 floors content", w: 10, h: 3},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: callID(1)}
			m := newModernSized(t, agent, tt.w, tt.h)
			if m.width != tt.w || m.height != tt.h {
				t.Fatalf("dims = %dx%d, want %dx%d", m.width, m.height, tt.w, tt.h)
			}
			// The viewport height is the layout's content region — the SINGLE invariant the
			// mouse hit-test and View both rely on.
			if got, want := m.viewport.height, m.layout().contentH; got != want {
				t.Errorf("viewport height = %d, want layout contentH %d", got, want)
			}
			if m.viewport.height < 0 {
				t.Errorf("viewport height = %d, want >= 0", m.viewport.height)
			}
		})
	}
}

// TestModernWheelScrolls pins that a wheel-up message scrolls the viewport off the tail.
func TestModernWheelScrolls(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 8)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	for i := 0; i < 20; i++ {
		m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", fmt.Sprintf("committed line %d", i))))
	}
	if m.viewport.maxOffset() == 0 {
		t.Fatalf("not enough content to scroll (maxOffset==0, lines=%d height=%d)", len(m.viewport.lines), m.viewport.height)
	}

	before := m.viewport.offset
	m, _ = updateModern(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.viewport.offset >= before {
		t.Errorf("wheel-up did not scroll: before=%d after=%d", before, m.viewport.offset)
	}
	if m.viewport.atTail {
		t.Error("atTail still true after scrolling up off the bottom")
	}
}

// TestModernCtrlTExpandsThinking pins the retroactive collapse toggle: ctrl+t flips the
// global fold and re-renders, so an already-committed thinking entry expands from its
// one-line summary to the full rail — the rendered line count grows.
func TestModernCtrlTExpandsThinking(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	m = feedModern(t, m, stepDoneFrom(primary, aiMessage("first reason\nsecond reason\nthird reason", "the answer")))

	if !m.collapse.globalCollapsed {
		t.Fatal("modern collapse should start collapsed (dense)")
	}
	collapsedCount := len(m.viewport.lines)

	m, _ = updateModern(t, m, tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl})

	if m.collapse.globalCollapsed {
		t.Error("globalCollapsed still true after ctrl+t")
	}
	if expanded := len(m.viewport.lines); expanded <= collapsedCount {
		t.Errorf("ctrl+t did not expand thinking: collapsed=%d expanded=%d", collapsedCount, expanded)
	}
}

// TestModernPrintableGoesToComposer pins the key-routing precedence: a printable key reaches
// the composer (appended to its value) and does NOT scroll the viewport.
func TestModernPrintableGoesToComposer(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 10)

	// Give the viewport scrollable content so a mis-routed key would be observable as a scroll.
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	for i := 0; i < 20; i++ {
		m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", fmt.Sprintf("line %d", i))))
	}
	offsetBefore := m.viewport.offset

	m, _ = updateModern(t, m, keyPress("a"))

	if got := m.interaction.input.Value(); got != "a" {
		t.Errorf("composer value = %q, want %q (printable routed to composer)", got, "a")
	}
	if m.viewport.offset != offsetBefore {
		t.Errorf("viewport offset changed on a printable key: before=%d after=%d", offsetBefore, m.viewport.offset)
	}
}

// TestModernViewportNavGoesToViewport pins the other side of precedence: PageUp is a
// non-conflicting nav key the viewport consumes (it scrolls), never reaching the composer.
func TestModernViewportNavGoesToViewport(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 8)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	for i := 0; i < 20; i++ {
		m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", fmt.Sprintf("line %d", i))))
	}
	before := m.viewport.offset

	m, _ = updateModern(t, m, keyPress("pgup"))

	if m.viewport.offset >= before {
		t.Errorf("PageUp did not scroll the viewport: before=%d after=%d", before, m.viewport.offset)
	}
	if v := m.interaction.input.Value(); v != "" {
		t.Errorf("composer value = %q, want empty (PageUp must not reach the composer)", v)
	}
}

// TestModernRegionAt pins the frame's region hit-testing (content → status → gap → box → gap →
// bar), the routing the mouse handler depends on. The two blank gap rows are regionGap (inert);
// the status/box regions are inert too, but the bar row focuses and the content region selects.
func TestModernRegionAt(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{primaryLoopID: callID(1)}
	m := newModernSized(t, agent, 80, 24)
	lay := m.layout()
	if lay.contentH <= 0 {
		t.Fatalf("expected a positive content height, got %d", lay.contentH)
	}

	tests := []struct {
		name string
		y    int
		want modernRegion
	}{
		{name: "top content row", y: 0, want: regionContent},
		{name: "last content row", y: lay.contentH - 1, want: regionContent},
		{name: "status row", y: lay.statusY, want: regionStatus},
		{name: "gap row above the box", y: lay.gapTopY, want: regionGap},
		{name: "box row", y: lay.boxTop, want: regionBox},
		{name: "gap row below the box", y: lay.gapBotY, want: regionGap},
		{name: "bar row (very bottom)", y: lay.barY, want: regionBar},
	}
	// The subtests deliberately run SEQUENTIALLY over the shared m: regionAt → layout →
	// the composer's textarea View() mutates an internal render cache, which is single-
	// threaded in production (only Bubble Tea's model goroutine calls View), so parallel
	// subtests sharing one m would data-race the textarea cache, not a real defect.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.regionAt(tt.y); got != tt.want {
				t.Errorf("regionAt(%d) = %d, want %d", tt.y, got, tt.want)
			}
		})
	}
}

// TestModernMouseRoutesByRegion pins that a content-region click drives the viewport
// selection while a bar-region click does not — the content-vs-bar routing.
func TestModernMouseRoutesByRegion(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", "some content here")))
	lay := m.layout()

	content, _ := updateModern(t, m, tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	if !content.viewport.hasSel {
		t.Error("content-region click did not begin a viewport selection")
	}

	bar, _ := updateModern(t, m, tea.MouseClickMsg{X: 0, Y: lay.barY, Button: tea.MouseLeft})
	if bar.viewport.hasSel {
		t.Error("bar-region click was mis-routed into the viewport selection")
	}
}

// TestModernContentClickCollapse pins the click/drag collapse discriminator and the
// header-only rule: a plain click on an entry's HEADER row (sub 0) toggles its fold; a click
// on a BODY row (sub > 0) does not; and a drag (press → motion held → release) is a text
// selection that never toggles. Each case asserts whether the rendered line count changed.
func TestModernContentClickCollapse(t *testing.T) {
	t.Parallel()

	primary := callID(1)

	tests := []struct {
		name       string
		gesture    func(t *testing.T, m ModernScreen) ModernScreen
		wantChange bool // did the rendered line count change (i.e. a fold toggled)?
	}{
		{
			name: "header click (sub 0) toggles the fold",
			gesture: func(t *testing.T, m ModernScreen) ModernScreen {
				m, _ = updateModern(t, m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
				m, _ = updateModern(t, m, tea.MouseReleaseMsg{X: 0, Y: 0, Button: tea.MouseLeft})
				return m
			},
			wantChange: true,
		},
		{
			name: "body click (sub > 0) does not toggle",
			gesture: func(t *testing.T, m ModernScreen) ModernScreen {
				y := len(m.viewport.lines) - 1 // the last line is narration — an entry BODY row
				m, _ = updateModern(t, m, tea.MouseClickMsg{X: 0, Y: y, Button: tea.MouseLeft})
				m, _ = updateModern(t, m, tea.MouseReleaseMsg{X: 0, Y: y, Button: tea.MouseLeft})
				return m
			},
			wantChange: false,
		},
		{
			name: "drag (press, motion, release) is a selection, never a toggle",
			gesture: func(t *testing.T, m ModernScreen) ModernScreen {
				m, _ = updateModern(t, m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
				m, _ = updateModern(t, m, tea.MouseMotionMsg{X: 5, Y: 1, Button: tea.MouseLeft})
				m, _ = updateModern(t, m, tea.MouseReleaseMsg{X: 5, Y: 1, Button: tea.MouseLeft})
				return m
			},
			wantChange: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: primary}
			m := newModernSized(t, agent, 80, 24)
			m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
			m = feedModern(t, m, stepDoneFrom(primary, aiMessage("first reason\nsecond reason\nthird reason", "the answer")))

			before := len(m.viewport.lines)
			m = tt.gesture(t, m)
			changed := len(m.viewport.lines) != before
			if changed != tt.wantChange {
				t.Errorf("line-count change = %v (before=%d after=%d), want change=%v", changed, before, len(m.viewport.lines), tt.wantChange)
			}
		})
	}
}

// TestModernAgentReachable pins that ModernScreen exposes Agent() (promoted from the embedded
// sessionCore), so it satisfies the composition root's agentHolder shape.
func TestModernAgentReachable(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{primaryLoopID: callID(7)}
	m := NewModern(context.Background(), agent, fakeOpen(agent), AgentBanner{})

	if m.Agent() != agent {
		t.Error("Agent() did not return the wrapped agent")
	}
	var h interface{ Agent() Agent } = m
	if h.Agent() != agent {
		t.Error("ModernScreen does not satisfy the agentHolder shape (Agent() Agent)")
	}
}

// TestModernSubscribeUsesAllLoopsFilter pins the filter injection: ModernScreen subscribes
// with the ALL-LOOPS scope (every loop's live Ephemeral stream), not the primary-only
// default — a focused subagent projection must not starve.
func TestModernSubscribeUsesAllLoopsFilter(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{primaryLoopID: callID(3)}
	m := NewModern(context.Background(), agent, fakeOpen(agent), AgentBanner{})

	_ = m.subscribe()() // drive agent.Subscribe with the injected filter and capture it

	if !agent.subFilter.Ephemeral.All {
		t.Errorf("modern Ephemeral scope = %+v, want All=true", agent.subFilter.Ephemeral)
	}
	if !agent.subFilter.Enduring.All {
		t.Errorf("modern Enduring scope = %+v, want All=true", agent.subFilter.Enduring)
	}
}

// NOTE: TestScreenSubscribeUsesPrimaryOnlyFilter (the scrollback Screen's primary-only
// Ephemeral-filter contrast case) was removed with that shell. The modern all-loops filter
// is covered by TestModernSubscribeUsesAllLoopsFilter above.

// ctrlKey builds a ctrl+<r> key press (e.g. ctrl+n / ctrl+p), the focus-cycle chords.
func ctrlKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
}

// barSpanOf returns the bar segment for loop id (and whether one exists), so a focus test
// can click at a segment's exact drawn cell span.
func barSpanOf(segs []barSeg, id uuid.UUID) (barSeg, bool) {
	for _, s := range segs {
		if s.id == id {
			return s, true
		}
	}
	return barSeg{}, false
}

// TestModernFocusRendersFocusedProjection is the core focus-swap assertion: with focus on the
// primary the viewport shows the PRIMARY's stream, and focusing a subagent re-renders THAT
// loop's projection — the viewport lines equal a fresh renderFocused() of the focused loop and
// carry the focused loop's content, not the other loop's.
func TestModernFocusRendersFocusedProjection(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("primary question")})
	m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", "primary answer")))
	m = feedModern(t, m, loopStarted(sub, "reviewer"))
	m = feedModern(t, m, event.TurnStarted{Header: hdr(sub), Message: userMsg("subagent task")})
	m = feedModern(t, m, stepDoneFrom(sub, aiMessage("", "subagent answer")))

	// Focus starts on the primary: the viewport shows the primary's stream only.
	if !containsPlain(m.viewport.lines, "primary answer") {
		t.Fatalf("primary focus missing primary content; got %q", plainAll(m.viewport.lines))
	}
	if containsPlain(m.viewport.lines, "subagent answer") {
		t.Errorf("primary focus leaked subagent content; got %q", plainAll(m.viewport.lines))
	}

	// Focusing the subagent re-renders ITS projection.
	m.focusLoop(sub)
	if m.focusedLoopID != sub {
		t.Fatalf("focusedLoopID = %v, want sub %v", m.focusedLoopID, sub)
	}
	if !containsPlain(m.viewport.lines, "subagent answer") {
		t.Errorf("subagent focus missing subagent content; got %q", plainAll(m.viewport.lines))
	}
	if containsPlain(m.viewport.lines, "primary answer") {
		t.Errorf("subagent focus leaked primary content; got %q", plainAll(m.viewport.lines))
	}
	// The rendered lines must equal a fresh render of the (now-focused) projection.
	if got, want := plainAll(m.viewport.lines), plainAll(m.renderFocused()); got != want {
		t.Errorf("viewport lines != renderFocused(sub):\n got %q\nwant %q", got, want)
	}
}

// TestModernCtrlNPCyclesFocusInLoopOrder pins that ctrl+n / ctrl+p move focus in loops()
// (creation) order and WRAP at both ends — the same order the bar draws, so the keyboard cycle
// and a bar click agree — and that each cycle is view-only (a nil command).
func TestModernCtrlNPCyclesFocusInLoopOrder(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	subA := callID(2)
	subB := callID(3)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	// Establish the creation order [primary, subA, subB] in loops().
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	m = feedModern(t, m, loopStarted(subA, "a"))
	m = feedModern(t, m, loopStarted(subB, "b"))
	if got := len(m.transcript.loops()); got != 3 {
		t.Fatalf("loops() = %d, want 3 (primary, subA, subB)", got)
	}

	steps := []struct {
		key  tea.KeyPressMsg
		want uuid.UUID
	}{
		{ctrlKey('n'), subA},
		{ctrlKey('n'), subB},
		{ctrlKey('n'), primary}, // forward wrap
		{ctrlKey('p'), subB},    // backward wrap
		{ctrlKey('p'), subA},
		{ctrlKey('p'), primary},
	}
	for i, s := range steps {
		var cmd tea.Cmd
		m, cmd = updateModern(t, m, s.key)
		if m.focusedLoopID != s.want {
			t.Errorf("step %d (%s): focusedLoopID = %v, want %v", i, s.key.String(), m.focusedLoopID, s.want)
		}
		if cmd != nil {
			t.Errorf("step %d (%s): focus cycle returned a non-nil cmd (view-only)", i, s.key.String())
		}
	}
}

// TestModernSingleLoopCycleIsNoop pins that with only the primary loop present a focus cycle is
// a no-op: there is nowhere else to focus, so ctrl+n / ctrl+p leave focus on the primary.
func TestModernSingleLoopCycleIsNoop(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	if got := len(m.transcript.loops()); got != 1 {
		t.Fatalf("loops() = %d, want 1 (primary only)", got)
	}

	for _, key := range []tea.KeyPressMsg{ctrlKey('n'), ctrlKey('p')} {
		var cmd tea.Cmd
		m, cmd = updateModern(t, m, key)
		if m.focusedLoopID != primary {
			t.Errorf("%s over a single loop moved focus to %v, want primary %v", key.String(), m.focusedLoopID, primary)
		}
		if cmd != nil {
			t.Errorf("%s single-loop cycle returned a non-nil cmd", key.String())
		}
	}
}

// TestModernBarClickFocuses pins the bar's click focus: a left click inside a loop's drawn
// segment focuses it, while a click on the inter-segment gap or past the last segment (both
// HitTest false) leaves focus unchanged. The click column is the segment's own drawn cell span
// (the bar draws from column 0 of its row, so the global column IS the bar-local column).
func TestModernBarClickFocuses(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	subA := callID(2)

	tests := []struct {
		name string
		colX func(segs []barSeg) int
		want uuid.UUID
	}{
		{
			name: "click on subA's span focuses subA",
			colX: func(segs []barSeg) int { s, _ := barSpanOf(segs, subA); return s.start },
			want: subA,
		},
		{
			name: "click on the gap before subA is a no-op",
			colX: func(segs []barSeg) int { s, _ := barSpanOf(segs, subA); return s.start - 1 },
			want: primary,
		},
		{
			name: "click past the last segment is a no-op",
			colX: func(segs []barSeg) int { return segs[len(segs)-1].end + 5 },
			want: primary,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: primary}
			m := newModernSized(t, agent, 80, 24)
			m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
			m = feedModern(t, m, loopStarted(subA, "reviewer"))

			lay := m.layout()
			segs, _ := m.bar().layout()
			if _, ok := barSpanOf(segs, subA); !ok {
				t.Fatalf("bar has no segment for subA; segs=%+v", segs)
			}
			x := tt.colX(segs)
			// A no-op column must genuinely miss every segment (guard the fixture).
			if tt.want == primary {
				if _, hit := m.bar().HitTest(x); hit {
					t.Fatalf("column %d expected to be a gap/overflow but HitTest hit a segment", x)
				}
			}

			m, cmd := updateModern(t, m, tea.MouseClickMsg{X: x, Y: lay.barY, Button: tea.MouseLeft})
			if m.focusedLoopID != tt.want {
				t.Errorf("bar click at x=%d focused %v, want %v", x, m.focusedLoopID, tt.want)
			}
			if cmd != nil {
				t.Error("bar-click focus returned a non-nil cmd (view-only)")
			}
		})
	}
}

// TestModernFocusResetsTailAndClearsSelection pins the two view resets a focus swap performs:
// the viewport re-pins to the tail (so the new loop's latest content shows) and any active
// selection is cleared (its (entry, sub) anchors belong to the OLD projection's buffer).
func TestModernFocusResetsTailAndClearsSelection(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	// A short-but-nonzero content region (height leaves a few content rows above the taller
	// modern bottom chrome — status + two gap rows + the padded box + the bar) so content still
	// scrolls AND a Y:0 click lands in the content region.
	m := newModernSized(t, agent, 80, 14)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	for i := 0; i < 20; i++ {
		m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", fmt.Sprintf("primary %d", i))))
	}
	m = feedModern(t, m, loopStarted(sub, "reviewer"))
	m = feedModern(t, m, event.TurnStarted{Header: hdr(sub), Message: userMsg("subtask")})
	for i := 0; i < 20; i++ {
		m = feedModern(t, m, stepDoneFrom(sub, aiMessage("", fmt.Sprintf("sub %d", i))))
	}

	// Scroll off the tail, then begin a selection in the primary's buffer.
	m, _ = updateModern(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.viewport.atTail {
		t.Fatal("precondition: viewport should be off the tail after a wheel-up")
	}
	m, _ = updateModern(t, m, tea.MouseClickMsg{X: 1, Y: 0, Button: tea.MouseLeft})
	if !m.viewport.hasSel {
		t.Fatal("precondition: a content click should begin a selection")
	}

	m.focusLoop(sub)

	if !m.viewport.atTail {
		t.Error("focus swap did not re-pin the viewport to the tail")
	}
	if m.viewport.offset != m.viewport.maxOffset() {
		t.Errorf("focus swap offset = %d, want maxOffset %d (tail)", m.viewport.offset, m.viewport.maxOffset())
	}
	if m.viewport.hasSel {
		t.Error("focus swap did not clear the stale selection")
	}
	if m.viewport.frozen != nil {
		t.Error("focus swap did not clear the frozen drag snapshot")
	}
}

// TestModernFocusIsViewOnly pins the view-only invariant: focusing a loop changes ONLY the view
// (focusedLoopID + viewport) — it returns no command, mutates no transcript entry, and issues
// no agent command (submit/approve/deny/answer). Focus must never message or interrupt a loop.
func TestModernFocusIsViewOnly(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	m = feedModern(t, m, loopStarted(sub, "reviewer"))
	m = feedModern(t, m, event.TurnStarted{Header: hdr(sub), Message: userMsg("subtask")})
	m = feedModern(t, m, stepDoneFrom(sub, aiMessage("", "sub answer")))

	committedBefore := len(m.transcript.committed)

	m, cmd := updateModern(t, m, ctrlKey('n'))
	if cmd != nil {
		t.Error("ctrl+n focus returned a non-nil cmd (must not submit/interrupt)")
	}
	if m.focusedLoopID != sub {
		t.Fatalf("ctrl+n did not focus the subagent (focused=%v)", m.focusedLoopID)
	}
	if got := len(m.transcript.committed); got != committedBefore {
		t.Errorf("focus mutated the transcript: committed %d -> %d", committedBefore, got)
	}
	if agent.submitCalled || agent.approveCalled || agent.denyCalled || agent.answerCalled {
		t.Error("focus issued an agent command (submit/approve/deny/answer) — not view-only")
	}
}

// TestModernStatusReflectsFocusedLoop pins that the status line follows the FOCUSED loop: with
// the primary idle and a subagent mid-turn streaming, the status reads "idle" on the primary
// and "streaming…" once the live subagent is focused (focusedStatus derives Running from the
// subagent projection's active live segment; statusInputs refines it to streaming).
func TestModernStatusReflectsFocusedLoop(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	m = feedModern(t, m, event.TurnDone{Header: hdr(primary)}) // primary parks idle
	m = feedModern(t, m, loopStarted(sub, "reviewer"))
	m = feedModern(t, m, event.TurnStarted{Header: hdr(sub), Message: userMsg("subtask")})
	m = feedModern(t, m, event.TokenDelta{Header: hdr(sub), Chunk: &content.TextChunk{Text: "sub streaming"}})

	// Focused on the idle primary → idle.
	if got := m.focusedStatus(); got != StatusIdle {
		t.Errorf("primary focus status = %d, want StatusIdle", got)
	}
	if got := statusLabel(m.focusedStatus(), m.statusInputs()); got != labelIdle {
		t.Errorf("primary focus label = %q, want %q", got, labelIdle)
	}

	// Focus the live subagent → running/streaming.
	m.focusLoop(sub)
	if got := m.focusedStatus(); got != StatusRunning {
		t.Errorf("subagent focus status = %d, want StatusRunning", got)
	}
	if got := statusLabel(m.focusedStatus(), m.statusInputs()); got != labelStreaming {
		t.Errorf("subagent focus label = %q, want %q (live subagent narration)", got, labelStreaming)
	}
}

// runningModern returns a ModernScreen wired for a live turn: sized (ready), a non-nil
// session subscription (subNext targets must be non-nil), and StatusRunning. It mirrors
// runningScreen so the prompt/interrupt/queued-input parity tests exercise a mid-turn model.
func runningModern(t *testing.T, agent Agent) ModernScreen {
	t.Helper()
	m := newModernSized(t, agent, 80, 24)
	m.sub = newFakeSubscription()
	m.status = StatusRunning
	return m
}

// barEntryFor returns loop id's assembled bar entry (and whether one exists), so a gate-marker
// test can assert the "!" flag on exactly the gated loop's segment.
func barEntryFor(b loopBar, id uuid.UUID) (loopBarEntry, bool) {
	for _, e := range b.entries {
		if e.id == id {
			return e, true
		}
	}
	return loopBarEntry{}, false
}

// TestModernInitTriggersRestore pins that Init schedules the cold-restore repaint alongside the
// live subscription — the viewport must repaint a RESTORED session's history before live events
// take over, exactly as Screen does (the parity gate for a restored swe session showing blank).
func TestModernInitTriggersRestore(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)
	agent := &fakeAgent{primaryLoopID: primary}
	m := NewModern(context.Background(), agent, fakeOpen(agent), AgentBanner{})

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() = nil, want non-nil (restore + subscribe batched)")
	}
	drainCmd(t, cmd)
	if !agent.replayCalled {
		t.Error("Init did not schedule the restore-repaint (ReplayBacklog not called)")
	}
}

// TestModernHandleRestored pins the cold-restore repaint into the viewport: a RESTORED backlog
// folds into the transcript (committed + per-loop projections + loop table) and re-renders so the
// history shows; a NEW session (empty backlog) is a strict no-op that leaves the banner untouched;
// a read failure surfaces a faint, non-fatal error notice the viewport re-renders.
func TestModernHandleRestored(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)
	sub := callID(0xBB)
	restored := []event.Event{
		event.TurnStarted{Header: hdr(primary), Message: userMsg("restored question")},
		stepDoneFrom(primary, aiMessage("", "restored primary answer")),
		loopStarted(sub, "reviewer"),
		event.TurnStarted{Header: hdr(sub), Message: userMsg("sub task")},
		stepDoneFrom(sub, aiMessage("", "restored sub answer")),
	}

	tests := []struct {
		name    string
		backlog []event.Event
		replay  error
		check   func(t *testing.T, m ModernScreen)
	}{
		{
			name:    "restored backlog repaints history + projections + loop table",
			backlog: restored,
			check: func(t *testing.T, m ModernScreen) {
				if len(m.transcript.committed) == 0 {
					t.Fatal("restore did not populate committed transcript")
				}
				if !containsPlain(m.viewport.lines, "restored primary answer") {
					t.Errorf("viewport missing repainted primary history; got %q", plainAll(m.viewport.lines))
				}
				if got := len(m.transcript.loops()); got != 2 {
					t.Errorf("loops() = %d, want 2 (primary + sub) — loop table must repaint", got)
				}
				if pc, _ := m.transcript.projectionFor(sub); len(pc) == 0 {
					t.Error("subagent projection empty after restore — projections must repaint from history")
				}
			},
		},
		{
			name:   "read failure surfaces a faint error notice",
			replay: errors.New("replay read"),
			check: func(t *testing.T, m ModernScreen) {
				if len(m.transcript.committed) == 0 {
					t.Fatal("restore error did not commit a notice")
				}
				rec := m.transcript.committed[len(m.transcript.committed)-1]
				if rec.Kind != kindNotice || rec.Level != noticeError {
					t.Errorf("restore-error entry = (kind %d, level %d), want (kindNotice, noticeError)", rec.Kind, rec.Level)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: primary, backlog: tt.backlog, replayErr: tt.replay}
			m := newModernSized(t, agent, 80, 24)
			msg := runRestoreCmd(t, restoreBacklogCmd(context.Background(), agent, primary))
			m = feedRestored(t, m, msg)
			tt.check(t, m)
		})
	}
}

// feedRestored drives one restoredMsg through Update and returns the new model.
func feedRestored(t *testing.T, m ModernScreen, msg restoredMsg) ModernScreen {
	t.Helper()
	m, _ = updateModern(t, m, msg)
	return m
}

// TestModernRestoreEmptyBacklogPreservesBanner proves the real contract of the empty-backlog
// guard (the load-bearing `if len(msg.transcript.committed) == 0 { return nil }` early-return):
// a NEW session that has ALREADY committed its opening banner must NOT have that banner
// discarded — nor the displayID counter reset — when the empty restoredMsg arrives. Installing
// the empty fold wholesale (without the guard) would clobber the banner; asserting the banner
// entry survives UNCHANGED (same id + text + count) makes the guard load-bearing under test.
func TestModernRestoreEmptyBacklogPreservesBanner(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)
	agent := &fakeAgent{primaryLoopID: primary, backlog: nil}
	m := NewModern(context.Background(), agent, fakeOpen(agent), AgentBanner{Name: "swe", Description: "test agent"})
	m, _ = updateModern(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = updateModern(t, m, systemReadyMsg{}) // commit the opening banner into the transcript

	if len(m.transcript.committed) == 0 {
		t.Fatal("precondition: opening banner not committed before the empty-backlog restore")
	}
	bannerLen := len(m.transcript.committed)
	bannerID := m.transcript.committed[0].ID
	bannerText := committedText(m.transcript.committed[0])
	if !strings.Contains(bannerText, "swe") {
		t.Fatalf("precondition: banner entry = %q, want the agent banner text", bannerText)
	}

	// The empty (new-session) fold must commit nothing itself...
	msg := runRestoreCmd(t, restoreBacklogCmd(context.Background(), agent, primary))
	if msg.err != nil {
		t.Fatalf("empty-backlog restoredMsg err = %v, want nil", msg.err)
	}
	if len(msg.transcript.committed) != 0 {
		t.Fatalf("empty-backlog fold committed = %d, want 0", len(msg.transcript.committed))
	}

	// ...and applying it must leave the already-committed banner untouched (the guard).
	m = feedRestored(t, m, msg)
	if len(m.transcript.committed) != bannerLen {
		t.Errorf("committed = %d after empty restore, want %d (banner must survive)", len(m.transcript.committed), bannerLen)
	}
	if m.transcript.committed[0].ID != bannerID {
		t.Errorf("banner entry id = %v after empty restore, want %v unchanged (displayID counter must not reset)", m.transcript.committed[0].ID, bannerID)
	}
	if got := committedText(m.transcript.committed[0]); got != bannerText {
		t.Errorf("banner entry text = %q after empty restore, want %q unchanged", got, bannerText)
	}
}

// TestModernBarGateMarker pins the "!" gate marker without focus steal: a pending prompt on a
// NON-focused loop marks THAT loop's bar segment (gate=true) while other loops stay unmarked, and
// prompt-open does NOT change focusedLoopID — the marker is how a non-focused loop signals it
// needs attention, and focus stays the user's to change (design §Prompts).
func TestModernBarGateMarker(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	subA := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := runningModern(t, agent)

	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	m = feedModern(t, m, loopStarted(subA, "reviewer"))
	if e, ok := barEntryFor(m.bar(), subA); ok && e.gate {
		t.Fatal("precondition: bar already marked a gate before any prompt")
	}
	focusBefore := m.focusedLoopID

	m = feedModern(t, m, event.PermissionRequested{Header: hdr(subA), ToolExecutionID: callID(7), Request: tool.BashRequest{Command: "ls"}})

	if m.focusedLoopID != focusBefore {
		t.Errorf("prompt-open stole focus: %v -> %v (must not steal)", focusBefore, m.focusedLoopID)
	}
	bar := m.bar()
	subEntry, ok := barEntryFor(bar, subA)
	if !ok {
		t.Fatalf("bar has no segment for the gated subagent; entries=%+v", bar.entries)
	}
	if !subEntry.gate {
		t.Error("gated subagent bar segment not marked (gate=false), want gate=true")
	}
	primEntry, ok := barEntryFor(bar, primary)
	if !ok {
		t.Fatal("bar has no segment for the primary")
	}
	if primEntry.gate {
		t.Error("non-gated primary bar segment marked gate=true, want false")
	}
	// The rendered bar carries the "!" glyph so the user sees the attention marker.
	if !strings.Contains(bar.Render(m.width), barGateMark) {
		t.Errorf("rendered bar missing %q gate glyph; got %q", barGateMark, bar.Render(m.width))
	}
}

// TestModernSubmitRoutesToFocusedLoop pins Stage-2 composer routing: a composer submit goes to
// the FOCUSED loop via SubmitToLoop and STAYS focused there (no auto-refocus to primary), and
// ONLY uiSubmit is intercepted — every other action still routes through the shared core.
// (a) a submit while focused on a subagent calls SubmitToLoop with the focused loop id + the
// composer text and does NOT move focus; (b) a submit while focused on the primary submits to
// the primary loop id; (c) a plain composer EDIT submits nothing and never moves focus;
// (d) a non-submit action (approve) still routes through the core unchanged, never a submit.
func TestModernSubmitRoutesToFocusedLoop(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	subA := callID(2)
	gateCall := callID(7)

	tests := []struct {
		name      string
		focusSub  bool // start focused on subA (else primary)
		act       func(t *testing.T, m ModernScreen) (ModernScreen, tea.Cmd)
		wantFocus uuid.UUID
		assert    func(t *testing.T, agent *fakeAgent)
	}{
		{
			name:     "submit from a subagent view targets that loop and stays focused",
			focusSub: true,
			act: func(t *testing.T, m ModernScreen) (ModernScreen, tea.Cmd) {
				m.interaction.input.SetValue("hello sub")
				return updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
			},
			wantFocus: subA,
			assert: func(t *testing.T, agent *fakeAgent) {
				if !agent.submitToLoopCalled {
					t.Error("SubmitToLoop not called for a subagent-view submit")
				}
				if agent.lastSubmitToLoopID != subA {
					t.Errorf("SubmitToLoop loopID = %v, want focused subagent %v", agent.lastSubmitToLoopID, subA)
				}
				if got := firstBlockText(agent.lastSubmitToLoopBlocks); got != "hello sub" {
					t.Errorf("SubmitToLoop blocks text = %q, want %q", got, "hello sub")
				}
				if agent.submitCalled {
					t.Error("primary Submit called on the modern focused-loop path, want SubmitToLoop only")
				}
			},
		},
		{
			name:     "submit from the primary view targets the primary loop",
			focusSub: false,
			act: func(t *testing.T, m ModernScreen) (ModernScreen, tea.Cmd) {
				m.interaction.input.SetValue("hello primary")
				return updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
			},
			wantFocus: primary,
			assert: func(t *testing.T, agent *fakeAgent) {
				if !agent.submitToLoopCalled {
					t.Error("SubmitToLoop not called for a primary-view submit")
				}
				if agent.lastSubmitToLoopID != primary {
					t.Errorf("SubmitToLoop loopID = %v, want primary %v", agent.lastSubmitToLoopID, primary)
				}
				if got := firstBlockText(agent.lastSubmitToLoopBlocks); got != "hello primary" {
					t.Errorf("SubmitToLoop blocks text = %q, want %q", got, "hello primary")
				}
			},
		},
		{
			name:     "a plain edit does NOT move focus or submit",
			focusSub: true,
			act: func(t *testing.T, m ModernScreen) (ModernScreen, tea.Cmd) {
				return updateModern(t, m, keyPress("x"))
			},
			wantFocus: subA,
			assert: func(t *testing.T, agent *fakeAgent) {
				if agent.submitToLoopCalled || agent.submitCalled {
					t.Error("a plain composer edit reached a submit path, want none")
				}
			},
		},
		{
			name:     "a non-submit action (approve) still routes through the core",
			focusSub: false,
			act: func(t *testing.T, m ModernScreen) (ModernScreen, tea.Cmd) {
				// A pending permission gate on the primary loop; 'y' approves it (uiApprove),
				// which must reach the core's approve path — NOT the submit interception.
				m = feedModern(t, m, event.PermissionRequested{
					Header:          hdr(primary),
					ToolExecutionID: gateCall,
					Request:         tool.BashRequest{Command: "ls"},
				})
				return updateModern(t, m, runeKey('y'))
			},
			wantFocus: primary,
			assert: func(t *testing.T, agent *fakeAgent) {
				if !agent.approveCalled {
					t.Error("approve did not route through the core (Approve not called)")
				}
				if agent.submitToLoopCalled || agent.submitCalled {
					t.Error("a non-submit action reached a submit path, want the core's approve only")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: primary}
			m := newModernSized(t, agent, 80, 24)
			m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
			m = feedModern(t, m, loopStarted(subA, "reviewer"))
			if tt.focusSub {
				m.focusLoop(subA)
				if m.focusedLoopID != subA {
					t.Fatalf("precondition: focus = %v, want subA %v", m.focusedLoopID, subA)
				}
			}

			m, cmd := tt.act(t, m)
			if m.focusedLoopID != tt.wantFocus {
				t.Errorf("focusedLoopID = %v, want %v (Stage 2: no auto-refocus)", m.focusedLoopID, tt.wantFocus)
			}
			drainCmd(t, cmd)
			tt.assert(t, agent)
		})
	}
}

// TestModernPermissionPromptDispatches pins permission-gate parity: a PermissionRequested renders
// in the bottom box and marks the bar; a key (y/s/n/esc) dispatches Approve/Deny to the gate's
// producing loop and pops the prompt (which clears the bar marker).
func TestModernPermissionPromptDispatches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		key         tea.KeyPressMsg
		wantApprove bool
		wantDeny    bool
		wantScope   tool.ApprovalScope
	}{
		{name: "y approves once", key: runeKey('y'), wantApprove: true, wantScope: tool.ScopeOnce},
		{name: "s approves session", key: runeKey('s'), wantApprove: true, wantScope: tool.ScopeSession},
		{name: "n denies", key: runeKey('n'), wantDeny: true},
		{name: "esc denies", key: tea.KeyPressMsg{Code: tea.KeyEsc}, wantDeny: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			primary := callID(1)
			gateLoop := callID(9)
			agent := &fakeAgent{primaryLoopID: primary}
			m := runningModern(t, agent)
			m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
			m = feedModern(t, m, loopStarted(gateLoop, "reviewer"))
			m = feedModern(t, m, event.PermissionRequested{
				Header:          hdr(gateLoop),
				ToolExecutionID: callID(7),
				Request:         tool.BashRequest{Command: "ls"},
			})

			// The bottom box renders the permission control, and the gated loop is marked.
			if !strings.Contains(m.bottomBoxView(), "Approve") {
				t.Errorf("bottom box missing the permission control; got %q", m.bottomBoxView())
			}
			if e, ok := barEntryFor(m.bar(), gateLoop); !ok || !e.gate {
				t.Error("gated loop not marked in the bar before resolving")
			}

			m, cmd := updateModern(t, m, tt.key)
			if cmd == nil {
				t.Fatal("permission key cmd = nil, want a bounded dispatch cmd")
			}
			if m.interaction.PendingCount() != 0 {
				t.Errorf("PendingCount = %d, want 0 (prompt popped)", m.interaction.PendingCount())
			}
			if e, ok := barEntryFor(m.bar(), gateLoop); ok && e.gate {
				t.Error("gate marker still set after resolving the prompt")
			}

			drainCmd(t, cmd)
			if tt.wantApprove {
				if !agent.approveCalled {
					t.Error("Approve not called")
				}
				if agent.lastScope != tt.wantScope {
					t.Errorf("Approve scope = %v, want %v", agent.lastScope, tt.wantScope)
				}
			}
			if tt.wantDeny && !agent.denyCalled {
				t.Error("Deny not called")
			}
			if agent.lastLoopID != gateLoop {
				t.Errorf("dispatched LoopID = %v, want the gate's producing loop %v", agent.lastLoopID, gateLoop)
			}
			if agent.lastCallID != callID(7) {
				t.Errorf("dispatched ToolExecutionID = %v, want %v", agent.lastCallID, callID(7))
			}
		})
	}
}

// TestModernAskUserDispatches pins AskUser parity: a free-text UserInputRequested renders its
// answer field and a typed answer dispatches ProvideAnswer to the producing loop; a choice
// request renders its choices and Enter answers the selected choice.
func TestModernAskUserDispatches(t *testing.T) {
	t.Parallel()

	t.Run("free-text answer dispatches ProvideAnswer", func(t *testing.T) {
		t.Parallel()
		primary := callID(1)
		gateLoop := callID(9)
		agent := &fakeAgent{primaryLoopID: primary}
		m := runningModern(t, agent)
		m = feedModern(t, m, event.UserInputRequested{
			Header:          hdr(gateLoop),
			ToolExecutionID: callID(3),
			Question:        "name?",
		})
		if m.interaction.mode != modeAnswerPrompt {
			t.Fatalf("mode = %d, want modeAnswerPrompt", m.interaction.mode)
		}
		if !strings.Contains(m.bottomBoxView(), "name?") {
			t.Errorf("bottom box missing the AskUser question; got %q", m.bottomBoxView())
		}

		for _, r := range "neo" {
			m, _ = updateModern(t, m, runeKey(r))
		}
		m, cmd := updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("answer submit cmd = nil, want provideAnswerCmd")
		}
		if m.interaction.PendingCount() != 0 {
			t.Errorf("PendingCount = %d, want 0 (answer popped)", m.interaction.PendingCount())
		}
		drainCmd(t, cmd)
		if !agent.answerCalled || agent.lastAnswer != "neo" || agent.lastCallID != callID(3) || agent.lastLoopID != gateLoop {
			t.Errorf("ProvideAnswer = (called %v, answer %q, id %v, loop %v), want (true, %q, %v, %v)",
				agent.answerCalled, agent.lastAnswer, agent.lastCallID, agent.lastLoopID, "neo", callID(3), gateLoop)
		}
	})

	t.Run("choice selection dispatches ProvideAnswer", func(t *testing.T) {
		t.Parallel()
		primary := callID(1)
		gateLoop := callID(9)
		agent := &fakeAgent{primaryLoopID: primary}
		m := runningModern(t, agent)
		m = feedModern(t, m, event.UserInputRequested{
			Header:          hdr(gateLoop),
			ToolExecutionID: callID(4),
			Question:        "pick one",
			Choices:         []string{"alpha", "beta"},
		})
		if m.interaction.mode != modeChoicePrompt {
			t.Fatalf("mode = %d, want modeChoicePrompt", m.interaction.mode)
		}
		if !strings.Contains(m.bottomBoxView(), "alpha") {
			t.Errorf("bottom box missing the choice list; got %q", m.bottomBoxView())
		}

		m, cmd := updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("choice enter cmd = nil, want provideAnswerCmd")
		}
		drainCmd(t, cmd)
		if !agent.answerCalled || agent.lastAnswer != "alpha" || agent.lastLoopID != gateLoop {
			t.Errorf("ProvideAnswer = (called %v, answer %q, loop %v), want (true, %q, %v)",
				agent.answerCalled, agent.lastAnswer, agent.lastLoopID, "alpha", gateLoop)
		}
	})
}

// TestModernClearReopensAndResubscribes pins /clear parity: the slash command flips to Resetting
// and reopens the agent (via the core); the reopen result swaps in the fresh agent, RESETS the
// modern view (focus back to the fresh primary, viewport cleared), and re-subscribes with the
// ALL-LOOPS filter — a /clear must not silently narrow the modern scope to primary-only.
func TestModernClearReopensAndResubscribes(t *testing.T) {
	t.Parallel()

	old := &fakeAgent{primaryLoopID: callID(1)}
	fresh := &fakeAgent{primaryLoopID: callID(2)}
	m := newModernSized(t, old, 80, 24)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(callID(1)), Message: userMsg("q")})
	m = feedModern(t, m, stepDoneFrom(callID(1), aiMessage("", "old answer")))
	m = feedModern(t, m, event.TurnDone{Header: hdr(callID(1))}) // return to idle so /clear is allowed

	// /clear while idle flips to Resetting and returns the reopen cmd.
	m.interaction.input.SetValue("/clear")
	m, cmd := updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.status != StatusResetting {
		t.Fatalf("status = %d, want StatusResetting after /clear", m.status)
	}
	if cmd == nil {
		t.Fatal("/clear cmd = nil, want the reopen cmd")
	}

	// The reopen result swaps the agent and resets the modern presentation.
	m, cmd = updateModern(t, m, reopenResultMsg{agent: fresh})
	if m.Agent() != fresh {
		t.Errorf("agent = %p, want fresh %p", m.Agent(), fresh)
	}
	if m.focusedLoopID != fresh.PrimaryLoopID() {
		t.Errorf("focusedLoopID = %v, want the fresh primary %v (view must reset)", m.focusedLoopID, fresh.PrimaryLoopID())
	}
	if len(m.transcript.committed) != 0 {
		t.Errorf("committed = %d, want 0 (transcript reset)", len(m.transcript.committed))
	}
	if len(m.viewport.lines) != 0 {
		t.Errorf("viewport lines = %d, want 0 (viewport reset)", len(m.viewport.lines))
	}
	if cmd == nil {
		t.Fatal("reopen cmd = nil, want closeAgent + re-subscribe")
	}
	drainCmd(t, cmd)
	if fresh.subscribeCount != 1 {
		t.Errorf("fresh Subscribe count = %d, want 1 (/clear re-subscribes the new agent)", fresh.subscribeCount)
	}
	if !fresh.subFilter.Ephemeral.All || !fresh.subFilter.Enduring.All {
		t.Errorf("re-subscribe filter = %+v, want all-loops (modern must not narrow to primary-only)", fresh.subFilter)
	}
	if !old.closeCalled {
		t.Error("old agent not closed on /clear swap")
	}
}

// TestModernInterruptAndQuit pins the two globals: esc with NO prompt interrupts a running turn
// (flips to Interrupting + dispatches the bounded Interrupt), and ctrl+c closes the subscription
// and quits.
func TestModernInterruptAndQuit(t *testing.T) {
	t.Parallel()

	t.Run("esc interrupts a running turn", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{primaryLoopID: callID(1), interruptCancelled: true}
		m := runningModern(t, agent)
		m, cmd := updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
		if cmd == nil {
			t.Error("esc cmd = nil, want the bounded Interrupt")
		}
		if m.status != StatusInterrupting {
			t.Errorf("status = %d, want StatusInterrupting", m.status)
		}
	})

	t.Run("ctrl+c closes the subscription and quits", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{primaryLoopID: callID(1)}
		m := runningModern(t, agent)
		sub := m.sub
		m, cmd := updateModern(t, m, ctrlKey('c'))
		if cmd == nil {
			t.Fatal("ctrl+c cmd = nil, want closeAgent + quit")
		}
		if m.sub != nil {
			t.Error("ctrl+c did not drop the subscription reference")
		}
		if fs, ok := sub.(*fakeSubscription); ok && !fs.closed {
			t.Error("ctrl+c did not close the subscription")
		}
	})
}

// TestModernQueuedInputWhileRunning pins queued-input parity: submitting while a turn RUNS does
// not error (it fires the fire-and-forget SubmitToLoop the loop queues) and — Stage 2 — the
// submit targets the FOCUSED loop and stays there, so a submit from a subagent view runs the
// queued turn on THAT subagent without snapping focus back to the primary.
func TestModernQueuedInputWhileRunning(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	subA := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := runningModern(t, agent)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	m = feedModern(t, m, loopStarted(subA, "reviewer"))
	m.focusLoop(subA)

	committedBefore := len(m.transcript.committed)
	m.interaction.input.SetValue("queued while running")
	m, cmd := updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.status != StatusRunning {
		t.Errorf("status = %d, want StatusRunning (submit does not change the running turn)", m.status)
	}
	if m.focusedLoopID != subA {
		t.Errorf("focusedLoopID = %v, want subA %v (Stage 2: submit stays on the focused loop)", m.focusedLoopID, subA)
	}
	if len(m.transcript.committed) != committedBefore {
		t.Errorf("committed grew by %d, want 0 (a queued submit commits no error)", len(m.transcript.committed)-committedBefore)
	}
	drainCmd(t, cmd)
	if !agent.submitToLoopCalled {
		t.Error("SubmitToLoop not called for a queued-while-running message")
	}
	if agent.lastSubmitToLoopID != subA {
		t.Errorf("queued submit loopID = %v, want focused subagent %v", agent.lastSubmitToLoopID, subA)
	}
}

// TestModernImageRejectedAtBoundary pins image parity: an image @path on a text-only model
// (!AcceptsImages) is rejected at the SAME submit build boundary the core owns — the message
// commits as a plain user row plus a faint error notice the viewport surfaces, and the agent is
// never sent the message (no mid-turn failure).
func TestModernImageRejectedAtBoundary(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{primaryLoopID: callID(1), acceptsImage: false}
	m := newModernSized(t, agent, 80, 24)
	m.interaction.input.SetValue("@photo.png") // an image on a text-only model → ImageUnsupportedError

	m, _ = updateModern(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})

	if agent.submitCalled {
		t.Error("agent.Submit called on a rejected image, want no send")
	}
	rec := m.transcript.committed[len(m.transcript.committed)-1]
	if rec.Kind != kindNotice || rec.Level != noticeError {
		t.Errorf("last committed = (kind %d, level %d), want (kindNotice, noticeError)", rec.Kind, rec.Level)
	}
	// The rejection is surfaced in the viewport (repainted), not lost mid-turn.
	if len(m.viewport.lines) == 0 {
		t.Error("viewport did not repaint the rejection")
	}
}

// TestFormatElapsed pins the status-line elapsed formatter: whole seconds under a minute as
// "Ns", a minute or more as "Nm Ss", and a negative span floored to "0s".
func TestFormatElapsed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "zero", d: 0, want: "0s"},
		{name: "sub-second truncates to 0s", d: 900 * time.Millisecond, want: "0s"},
		{name: "eight seconds", d: 8 * time.Second, want: "8s"},
		{name: "boundary 59s", d: 59 * time.Second, want: "59s"},
		{name: "boundary 60s", d: 60 * time.Second, want: "1m 0s"},
		{name: "the example 2m 34s", d: 154 * time.Second, want: "2m 34s"},
		{name: "over an hour stays Nm Ss", d: 3661 * time.Second, want: "61m 1s"},
		{name: "negative floors to 0s", d: -5 * time.Second, want: "0s"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatElapsed(tt.d); got != tt.want {
				t.Errorf("formatElapsed(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

// hdrAt builds an event Header carrying a loop id and a creation timestamp — the two fields
// the status-line turn timer keys on (LoopID selects the loop, CreatedAt anchors elapsed).
func hdrAt(loopID uuid.UUID, at time.Time) event.Header {
	return event.Header{Coordinates: identity.Coordinates{LoopID: loopID}, CreatedAt: at}
}

// TestModernStatusTimerSuffix pins the live turn-elapsed suffix: it appears (as "(Nm Ss)")
// only while the focused loop is running with a known turn start, is driven by the tick's
// carried time (never a wall clock), and disappears once the turn ends. No real time is read
// — the turn start and "now" both come from event/tick timestamps.
func TestModernStatusTimerSuffix(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		drive       func(t *testing.T, m ModernScreen) ModernScreen
		wantSuffix  string // substring the status line must contain ("" = assert none)
		wantNoParen bool   // assert no "(" appears at all (idle)
	}{
		{
			name:        "idle: no suffix",
			drive:       func(_ *testing.T, m ModernScreen) ModernScreen { return m },
			wantNoParen: true,
		},
		{
			name: "running 154s: (2m 34s)",
			drive: func(t *testing.T, m ModernScreen) ModernScreen {
				m = feedModern(t, m, event.TurnStarted{Header: hdrAt(primary, base)})
				m, _ = updateModern(t, m, tickMsg{at: base.Add(154 * time.Second)})
				return m
			},
			wantSuffix: "(2m 34s)",
		},
		{
			name: "running 8s: (8s)",
			drive: func(t *testing.T, m ModernScreen) ModernScreen {
				m = feedModern(t, m, event.TurnStarted{Header: hdrAt(primary, base)})
				m, _ = updateModern(t, m, tickMsg{at: base.Add(8 * time.Second)})
				return m
			},
			wantSuffix: "(8s)",
		},
		{
			name: "turn ended: suffix gone",
			drive: func(t *testing.T, m ModernScreen) ModernScreen {
				m = feedModern(t, m, event.TurnStarted{Header: hdrAt(primary, base)})
				m, _ = updateModern(t, m, tickMsg{at: base.Add(20 * time.Second)})
				m = feedModern(t, m, event.TurnDone{Header: hdrAt(primary, base.Add(30*time.Second))})
				return m
			},
			wantNoParen: true,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: primary}
			m := newModernSized(t, agent, 80, 24)
			m = tt.drive(t, m)
			status := plainFromStyled(m.modernStatusLine())
			if tt.wantSuffix != "" && !strings.Contains(status, tt.wantSuffix) {
				t.Errorf("status = %q, want it to contain %q", status, tt.wantSuffix)
			}
			if tt.wantNoParen && strings.Contains(status, "(") {
				t.Errorf("status = %q, want no elapsed suffix", status)
			}
		})
	}
}

// TestModernTickLifecycle pins that the 1s tick starts when a turn becomes active and stops
// once the session goes idle, so the timer never ticks forever: a TurnStarted arms the tick;
// a tick while running re-arms; after the turn ends a final tick does NOT re-arm.
func TestModernTickLifecycle(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	if m.ticking {
		t.Fatal("tick armed before any turn started")
	}
	// A turn becoming active arms the tick and records the turn start.
	m, cmd := updateModern(t, m, eventMsg{ev: event.TurnStarted{Header: hdrAt(primary, base)}})
	if !m.ticking {
		t.Error("tick not armed after TurnStarted")
	}
	if cmd == nil {
		t.Error("handleEventModern returned nil cmd on TurnStarted, want a batch with the tick")
	}
	if _, ok := m.turnStartedAt[primary]; !ok {
		t.Error("turnStartedAt missing the running loop")
	}
	// A tick while the turn is still running re-arms (chain continues).
	m, cmd = updateModern(t, m, tickMsg{at: base.Add(time.Second)})
	if !m.ticking || cmd == nil {
		t.Errorf("tick did not re-arm while running: ticking=%v cmd=%v", m.ticking, cmd)
	}
	// The turn ends; its start is cleared.
	m = feedModern(t, m, event.TurnDone{Header: hdrAt(primary, base.Add(2*time.Second))})
	if _, ok := m.turnStartedAt[primary]; ok {
		t.Error("turnStartedAt still holds a finished loop")
	}
	// The in-flight tick fires with nothing running: it must NOT re-arm.
	m, cmd = updateModern(t, m, tickMsg{at: base.Add(3 * time.Second)})
	if m.ticking {
		t.Error("tick re-armed after the session went idle (would tick forever)")
	}
	if cmd != nil {
		t.Errorf("idle tick returned a non-nil cmd %v, want nil (chain stops)", cmd)
	}
}

// TestModernStatusGradientAnimates pins that the modern status label SHIMMERS: at two different
// anim-state frames the same status label renders DIFFERENT styled bytes (the flowing gradient),
// while the plain (ANSI-free) text — the findable "streaming…" substring — is unchanged. This is
// the visible payoff of threading m.anim.frame into modernStatusLine's phase.
func TestModernStatusGradientAnimates(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	// A live subagent streaming narration, focused, reads "streaming…" (mirrors
	// TestModernStatusReflectsFocusedLoop) — a real animated label to sample across frames.
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	m = feedModern(t, m, event.TurnDone{Header: hdr(primary)})
	m = feedModern(t, m, loopStarted(sub, "reviewer"))
	m = feedModern(t, m, event.TurnStarted{Header: hdr(sub), Message: userMsg("subtask")})
	m = feedModern(t, m, event.TokenDelta{Header: hdr(sub), Chunk: &content.TextChunk{Text: "sub streaming"}})
	m.focusLoop(sub)

	m.anim.frame = 0
	styled0 := m.modernStatusLine()
	m.anim.frame = 5
	styled5 := m.modernStatusLine()

	if styled0 == styled5 {
		t.Errorf("status line identical across anim frames 0 and 5 = %q; want the gradient to animate", styled0)
	}
	plain0, plain5 := plainFromStyled(styled0), plainFromStyled(styled5)
	if plain0 != plain5 {
		t.Errorf("plain status text changed across frames: %q vs %q; only the styling may differ", plain0, plain5)
	}
	if !strings.Contains(plain0, labelStreaming) {
		t.Errorf("status plain text = %q, want it to contain %q", plain0, labelStreaming)
	}
}

// TestModernAnimTick pins the continuous status-shimmer tick: it advances the gradient frame and
// re-arms EVERY tick (idle included — no per-turn gate), does NOT re-render the transcript buffer
// (a pure status-line recompose), and self-terminates on quit so no tick leaks past tea.Quit.
func TestModernAnimTick(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	t.Run("advances the frame and re-arms while idle", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{primaryLoopID: callID(1)}
		m := newModernSized(t, agent, 80, 24) // no turn active — idle still animates
		if m.anim.frame != 0 {
			t.Fatalf("fresh anim frame = %d, want 0", m.anim.frame)
		}
		m, cmd := updateModern(t, m, animMsg(base))
		if m.anim.frame != 1 {
			t.Errorf("anim frame after one tick = %d, want 1", m.anim.frame)
		}
		if cmd == nil {
			t.Error("anim tick did not re-arm while idle; want a continuous reschedule")
		}
		m, cmd = updateModern(t, m, animMsg(base.Add(blinkInterval)))
		if m.anim.frame != 2 || cmd == nil {
			t.Errorf("second anim tick: frame=%d cmd=%v, want frame=2 and a reschedule", m.anim.frame, cmd)
		}
	})

	t.Run("does not re-render the transcript", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{primaryLoopID: callID(1)}
		m := newModernSized(t, agent, 80, 24)
		// Commit an entry (the opening banner) so the viewport has a real buffer to guard.
		m, _ = updateModern(t, m, systemReadyMsg{})
		before := m.viewport.lines
		if len(before) == 0 {
			t.Fatal("expected committed viewport lines to test against")
		}
		m, cmd := updateModern(t, m, animMsg(base))
		after := m.viewport.lines
		// SetLines (the only re-render path) installs a FRESH slice; an anim tick must leave the
		// SAME backing buffer untouched. Same length AND same first-element address proves it.
		if len(after) != len(before) || &after[0] != &before[0] {
			t.Errorf("anim tick re-rendered the transcript: lines changed (len %d→%d); want the buffer untouched", len(before), len(after))
		}
		if cmd == nil {
			t.Error("anim tick did not re-arm; want a reschedule")
		}
	})

	t.Run("stops on quit", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{primaryLoopID: callID(1)}
		m := runningModern(t, agent)
		m, _ = updateModern(t, m, ctrlKey('c'))
		if !m.quitting {
			t.Fatal("ctrl+c did not latch quitting")
		}
		frameBefore := m.anim.frame
		m, cmd := updateModern(t, m, animMsg(base))
		if cmd != nil {
			t.Errorf("anim tick after quit returned cmd %v, want nil (chain stops, no leak)", cmd)
		}
		if m.anim.frame != frameBefore {
			t.Errorf("anim tick after quit advanced the frame %d→%d, want it frozen", frameBefore, m.anim.frame)
		}
	})
}

// realThinkDelta / realTextDelta build streaming TokenDeltas in the REAL harness shape: an
// Ephemeral delta on loopID carrying NO CreatedAt (the harness never stamps Ephemeral events
// — only Enduring events get a Factory CreatedAt). The modern shell must stamp them with its
// model clock (m.now) before the reducer folds them, or the committed thinking span reads 0.
func realThinkDelta(loopID uuid.UUID, s string) event.Event {
	return event.TokenDelta{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}},
		Chunk:  &content.ThinkingChunk{Thinking: s},
	}
}

func realTextDelta(loopID uuid.UUID, s string) event.Event {
	return event.TokenDelta{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}},
		Chunk:  &content.TextChunk{Text: s},
	}
}

// firstThinkDurIn returns the thinkDur of the first committed kindAssistant entry carrying a
// ThinkingBlock in entries, and whether one exists — the seam the modern thinking-duration
// test asserts a projection's (or the root fold's) captured span through.
func firstThinkDurIn(entries []entry) (time.Duration, bool) {
	for _, e := range entries {
		if e.Kind != kindAssistant {
			continue
		}
		for _, b := range e.Blocks {
			if _, ok := b.(*content.ThinkingBlock); ok {
				return e.thinkDur, true
			}
		}
	}
	return 0, false
}

// TestModernRealThinkingDurationFromModelClock is the duration=0 bug's regression test. It
// drives streaming thinking in the REAL harness shape (TokenDeltas with NO CreatedAt) through
// the modern shell, advancing the model clock via a tickMsg — the SAME clock the turn timer
// uses — and asserts the committed thinking entry carries the measured, NON-ZERO span rendered
// as the lowercase "thought for Nsec". Because the deltas are unstamped, the PRE-FIX reducer
// (timing from ev.CreatedAt) captures 0 → the bare "thought"; the fix stamps them from m.now.
// Both a PRIMARY loop (root fold) and a SUBAGENT loop (its own projection) are timed — the
// subagent's Ephemeral stream flows through handleEventModern too.
func TestModernRealThinkingDurationFromModelClock(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		loop    uuid.UUID     // the loop whose thinking is timed (primary → root, sub → projection)
		elapsed time.Duration // the model-clock span between the first thinking chunk and the text chunk
		wantHdr string        // the committed, collapsed header the viewport must show
	}{
		{name: "primary loop thinking timed from the model clock", loop: primary, elapsed: 8 * time.Second, wantHdr: "thought for 8sec"},
		{name: "subagent loop thinking timed too", loop: sub, elapsed: 25 * time.Second, wantHdr: "thought for 25sec"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: primary}
			m := newModernSized(t, agent, 80, 24)

			// TurnStarted seeds the model clock (m.now = base). The thinking chunks stream with
			// no CreatedAt; the shell stamps them with m.now. A tick advances m.now by elapsed,
			// so the first text chunk (which seals the thinking end) is stamped elapsed later.
			m = feedModern(t, m, event.TurnStarted{Header: hdrAt(tt.loop, base)})
			m = feedModern(t, m, realThinkDelta(tt.loop, "weigh "))
			m = feedModern(t, m, realThinkDelta(tt.loop, "the options"))
			m, _ = updateModern(t, m, tickMsg{at: base.Add(tt.elapsed)})
			m = feedModern(t, m, realTextDelta(tt.loop, "here is the plan"))
			m = feedModern(t, m, stepDoneFrom(tt.loop, aiMessage("weigh the options", "here is the plan")))

			committed, _ := m.transcript.projectionFor(tt.loop)
			gotDur, ok := firstThinkDurIn(committed)
			if !ok {
				t.Fatalf("no committed thinking entry for loop %v; committed=%+v", tt.loop, committed)
			}
			if gotDur != tt.elapsed {
				t.Errorf("committed thinkDur = %v, want %v (measured from the model clock, not the zero TokenDelta CreatedAt)", gotDur, tt.elapsed)
			}

			// Focus the loop and confirm the collapsed committed header reads the lowercase span.
			(&m).focusLoop(tt.loop)
			if !containsPlain(m.viewport.lines, tt.wantHdr) {
				t.Errorf("viewport = %q, want the committed header %q", plainAll(m.viewport.lines), tt.wantHdr)
			}
		})
	}
}

// TestModernLiveThinkingAlwaysExpanded pins the live-vs-committed collapse rule: while thinking
// STREAMS, the live segment renders fully EXPANDED (the "│ thinking" header PLUS every reasoning
// line) even though the global collapse default is COLLAPSED — the live tail is excluded from
// the fold. Once the step COMMITS, that same reasoning follows the collapse state: the default
// collapses it to the one-line "│ thought" summary with the body hidden.
func TestModernLiveThinkingAlwaysExpanded(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	if !m.collapse.globalCollapsed {
		t.Fatal("modern collapse should start collapsed (dense)")
	}

	// LIVE: thinking streams (no StepDone yet). Despite the collapsed default, the live tail
	// shows the present-tense "│ thinking" header AND the full multi-line reasoning body.
	m = feedModern(t, m, event.TurnStarted{Header: hdrAt(primary, base)})
	m = feedModern(t, m, realThinkDelta(primary, "reason line one\nreason line two"))

	if !containsPlain(m.viewport.lines, "│ thinking") {
		t.Errorf("live tail = %q, want the present-tense '│ thinking' header", plainAll(m.viewport.lines))
	}
	for _, body := range []string{"reason line one", "reason line two"} {
		if !containsPlain(m.viewport.lines, body) {
			t.Errorf("live tail = %q, want the expanded reasoning line %q (live is excluded from the collapse fold)", plainAll(m.viewport.lines), body)
		}
	}

	// COMMIT: the step's StepDone commits the thinking entry, which now follows the collapse
	// default — a single "│ thought" header, the reasoning body hidden.
	m = feedModern(t, m, stepDoneFrom(primary, aiMessage("reason line one\nreason line two", "the answer")))

	if !containsPlain(m.viewport.lines, "│ thought") {
		t.Errorf("committed = %q, want the collapsed '│ thought' header", plainAll(m.viewport.lines))
	}
	if containsPlain(m.viewport.lines, "reason line one") {
		t.Errorf("committed = %q, want the reasoning body HIDDEN once collapsed", plainAll(m.viewport.lines))
	}
}

// TestModernUserRowGrayBackground pins the MODERN user-message gray panel: the focused
// projection's user-row lines carry the gray background fill and are padded to the full
// content width, while the SAME entry rendered through the scrollback renderEntry carries no
// background (Screen stays bare). The ANSI-free plain text is unchanged, so selection/copy
// still extract clean text.
func TestModernUserRowGrayBackground(t *testing.T) {
	t.Parallel()

	const bgSGR = "\x1b[48;2;48;48;48m" // ModernPanelBg (#303030) truecolor background open
	const width = 40

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, width, 24)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("hi there")})

	// Locate the user row in the rendered viewport.
	var userLines []renderedLine
	for _, ln := range m.viewport.lines {
		if strings.Contains(ln.plain, "hi there") {
			userLines = append(userLines, ln)
		}
	}
	if len(userLines) == 0 {
		t.Fatalf("no user row rendered; viewport:\n%s", plainAll(m.viewport.lines))
	}
	for _, ln := range userLines {
		if !strings.Contains(ln.styled, bgSGR) {
			t.Errorf("modern user line missing gray background; styled=%q", ln.styled)
		}
		if got := lipgloss.Width(ln.styled); got != width {
			t.Errorf("modern user line width = %d, want %d (padded to content width)", got, width)
		}
		if strings.ContainsRune(ln.plain, 0x1b) {
			t.Errorf("modern user line plain text carries an escape; plain=%q", ln.plain)
		}
	}

	// The SAME entry via the scrollback renderer must NOT carry the background.
	var userEntry entry
	for _, e := range m.transcript.committed {
		if e.Kind == kindUser {
			userEntry = e
		}
	}
	for _, line := range renderEntry(userEntry, true, width) {
		if strings.Contains(line, bgSGR) {
			t.Errorf("scrollback user line unexpectedly carries the gray background; line=%q", line)
		}
	}
}

// TestModernComposerDefaultsToTwoLines pins the modern composer's 2-line default: the
// composer built by the MODERN path (NewModern → modernizeComposer → SetMinLines(2))
// reports a minimum visible height of modernComposerMinLines when empty, and the modern
// bottom box is at least that tall, while Screen's plain composer (newInteractionModel,
// unmodernized) stays at 1. It fails if the modern 2-line default regresses.
func TestModernComposerDefaultsToTwoLines(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{primaryLoopID: callID(1)}
	m := newModernSized(t, agent, 80, 24)

	if got := m.interaction.input.Height(); got != modernComposerMinLines {
		t.Errorf("modern composer empty height = %d, want %d", got, modernComposerMinLines)
	}
	// The rendered bottom box reflects the 2-line default too.
	if got := lipgloss.Height(m.bottomBoxView()); got < modernComposerMinLines {
		t.Errorf("modern bottom-box height = %d, want >= %d", got, modernComposerMinLines)
	}
	// Screen's plain (unmodernized) composer, sized the same way, stays at the historical
	// single line.
	plain := newInteractionModel()
	plain.input.Resize(80)
	if got := plain.input.Height(); got != 1 {
		t.Errorf("plain composer empty height = %d, want 1 (Screen unchanged)", got)
	}
}

// TestModernRenderFocusedPrimaryExcludesSubagentLeak is the end-to-end regression guard:
// with the primary loop focused (the default), renderFocused renders projectionFor(primary)
// = the root fold, so a CONCURRENT subagent's live Ephemeral stream (delivered under the
// modern AllLoopsEventFilter) must NOT appear in the primary-focused viewport. Before the
// root-fold guard, the subagent's TokenDelta and ToolCallStarted leaked into m.live and
// spliced into the orchestrator's live tail. Focusing the subagent still shows its OWN
// stream (its projection is unchanged), proving the guard only blocks the ROOT leak.
func TestModernRenderFocusedPrimaryExcludesSubagentLeak(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	// The PRIMARY orchestrator streams its own live narration.
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	m = feedModern(t, m, event.TokenDelta{Header: hdr(primary), Chunk: &content.TextChunk{Text: "PRIMARY narration here"}})

	// A concurrent subagent's live stream arrives on the all-loops firehose.
	m = feedModern(t, m, event.TurnStarted{Header: hdr(sub)})
	m = feedModern(t, m, event.TokenDelta{Header: hdr(sub), Chunk: &content.TextChunk{Text: "SUBAGENT leaked words"}})
	m = feedModern(t, m, event.ToolCallStarted{Header: hdr(sub), ToolExecutionID: callID(0x33), ToolName: "Bash", Summary: "subagent danger"})

	// (c) the PRIMARY-focused viewport shows only the primary's live content.
	lines := m.renderFocused()
	if !containsPlain(lines, "PRIMARY narration here") {
		t.Errorf("primary renderFocused missing primary narration; got %q", plainAll(lines))
	}
	if containsPlain(lines, "SUBAGENT leaked words") {
		t.Errorf("primary renderFocused LEAKED subagent narration; got %q", plainAll(lines))
	}
	if containsPlain(lines, "subagent danger") {
		t.Errorf("primary renderFocused LEAKED subagent tool card; got %q", plainAll(lines))
	}
	// The root live segment itself must carry no subagent tool card.
	if len(m.transcript.live.Calls) != 0 {
		t.Errorf("root live.Calls = %d, want 0 (subagent ToolCallStarted must not add a root card)", len(m.transcript.live.Calls))
	}

	// Focusing the subagent shows ITS OWN stream — projections still work.
	m.focusLoop(sub)
	subLines := m.renderFocused()
	if !containsPlain(subLines, "SUBAGENT leaked words") {
		t.Errorf("subagent renderFocused missing its own narration; got %q", plainAll(subLines))
	}
}

// TestModernBarActiveFilter pins the modern active-loops bar's filter (m.bar()): it shows only
// LIVE loops, ALWAYS keeps the FOCUSED loop (even when idle) so the current view is labeled,
// drops idle non-focused loops, and falls back to the primary when the filter would leave the
// bar empty.
func TestModernBarActiveFilter(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	subA := callID(2)
	subB := callID(3)

	tests := []struct {
		name    string
		drive   func(t *testing.T, m ModernScreen) ModernScreen
		present []uuid.UUID
		absent  []uuid.UUID
	}{
		{
			name: "idle non-focused loop drops off; live loops stay",
			drive: func(t *testing.T, m ModernScreen) ModernScreen {
				m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
				m = feedModern(t, m, loopStarted(subA, "a"))
				m = feedModern(t, m, loopStarted(subB, "b"))
				m = feedModern(t, m, event.LoopIdle{Header: hdr(subB)}) // subB parks idle, not focused
				return m
			},
			present: []uuid.UUID{primary, subA},
			absent:  []uuid.UUID{subB},
		},
		{
			name: "the focused loop is kept even when idle",
			drive: func(t *testing.T, m ModernScreen) ModernScreen {
				m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
				m = feedModern(t, m, loopStarted(subB, "b"))
				m = feedModern(t, m, event.LoopIdle{Header: hdr(subB)}) // subB idle...
				m.focusLoop(subB)                                       // ...but focused → kept
				return m
			},
			present: []uuid.UUID{primary, subB},
		},
		{
			name: "empty filter falls back to the primary",
			drive: func(t *testing.T, m ModernScreen) ModernScreen {
				m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
				m = feedModern(t, m, event.LoopIdle{Header: hdr(primary)}) // primary parks idle
				m.focusedLoopID = subB                                     // focus a loop absent from the table
				return m
			},
			present: []uuid.UUID{primary},
			absent:  []uuid.UUID{subB},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			agent := &fakeAgent{primaryLoopID: primary}
			m := newModernSized(t, agent, 80, 24)
			m = tt.drive(t, m)
			bar := m.bar()
			for _, id := range tt.present {
				if _, ok := barEntryFor(bar, id); !ok {
					t.Errorf("bar missing expected loop %v; entries=%+v", id, bar.entries)
				}
			}
			for _, id := range tt.absent {
				if _, ok := barEntryFor(bar, id); ok {
					t.Errorf("bar includes filtered-out loop %v; entries=%+v", id, bar.entries)
				}
			}
		})
	}
}

// TestModernBarMarkerAndFormat pins the modern bar's rendered form through m.bar(): the FOCUSED
// loop carries the filled ● mark and every other loop the hollow ○, each formatted as
// "<mark> <name> (<id4>)" (agent name then the short loop id in parentheses). Focus flips the
// marks.
func TestModernBarMarkerAndFormat(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	sub := callID(2)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("q")})
	m = feedModern(t, m, loopStarted(sub, "operator"))

	// Primary focused: the unfocused subagent shows "○ operator (<id4>)"; a filled ● appears.
	plain := stripANSI(m.bar().Render(m.width))
	if want := barSegOf(barUnfocusedMark, "operator", sub); !strings.Contains(plain, want) {
		t.Errorf("bar = %q, want it to contain %q (unfocused ○ name (id) format)", plain, want)
	}
	if !strings.Contains(plain, barFocusedMark) {
		t.Errorf("bar = %q, want it to contain the focused ● mark", plain)
	}

	// Focusing the subagent flips the marks: sub becomes ●.
	m.focusLoop(sub)
	plain = stripANSI(m.bar().Render(m.width))
	if want := barSegOf(barFocusedMark, "operator", sub); !strings.Contains(plain, want) {
		t.Errorf("bar = %q, want the focused subagent to carry the ● mark (%q)", plain, want)
	}
}

// runsByEntry groups rendered lines into contiguous runs sharing an entry id. Adjacent
// committed entries carry distinct monotonic displayIDs, so each run is exactly one entry's
// block — its own rendered lines plus its single trailing blank separator (modern spacing).
func runsByEntry(lines []renderedLine) [][]renderedLine {
	var runs [][]renderedLine
	for _, ln := range lines {
		if n := len(runs); n > 0 && runs[n-1][0].entry == ln.entry {
			runs[n-1] = append(runs[n-1], ln)
			continue
		}
		runs = append(runs, []renderedLine{ln})
	}
	return runs
}

// runHasPlain reports whether any line in run carries sub in its plain text.
func runHasPlain(run []renderedLine, sub string) bool {
	for _, ln := range run {
		if strings.Contains(ln.plain, sub) {
			return true
		}
	}
	return false
}

// TestModernBlankSeparatorBetweenEntries pins the modern viewport's breathing space: renderFocused
// appends exactly ONE blank renderedLine after every committed entry — the opening banner included —
// so banner→user→assistant are each set off by one empty row. The blank is provenance-tagged to the
// entry ABOVE it (its id, a non-header sub == last-sub+1) so a collapse-click on it never toggles and
// a selection spanning the gap includes the newline; and it carries NO styled bytes, so it never picks
// up the modern user gray fill. Scrollback's renderEntry spacing is a separate path and is untouched.
func TestModernBlankSeparatorBetweenEntries(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := NewModern(context.Background(), agent, fakeOpen(agent), AgentBanner{Name: "swe", Description: "test agent"})
	m, _ = updateModern(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = updateModern(t, m, systemReadyMsg{}) // commit the opening banner as the head entry

	// A user turn then its assistant reply: three committed entries (banner, user, assistant), no
	// live tail, so the only empty rows in the render are the trailing blank separators.
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary), Message: userMsg("hello there")})
	m = feedModern(t, m, stepDoneFrom(primary, aiMessage("", "hi back")))

	committed, _ := m.transcript.projectionFor(primary)
	if len(committed) < 3 {
		t.Fatalf("precondition: want >= 3 committed entries (banner, user, assistant), got %d", len(committed))
	}

	lines := m.renderFocused()
	runs := runsByEntry(lines)
	if len(runs) != len(committed) {
		t.Fatalf("got %d entry runs, want %d (one per committed entry); lines=%q", len(runs), len(committed), plainAll(lines))
	}

	for k, run := range runs {
		wantID := committed[k].ID
		if len(run) < 2 {
			t.Fatalf("entry %d run has %d line(s), want >= 2 (>= 1 real line + 1 trailing blank)", k, len(run))
		}
		blank := run[len(run)-1]
		// The run's LAST line is the trailing blank separator: empty plain (selection sees the gap
		// as a newline), empty styled (never carries the user gray fill), tagged to the entry above.
		if blank.entry != wantID {
			t.Errorf("entry %d blank.entry = %d, want %d (owned by the entry above)", k, blank.entry, wantID)
		}
		if blank.plain != "" {
			t.Errorf("entry %d trailing line plain = %q, want empty (a blank separator)", k, blank.plain)
		}
		if blank.styled != "" {
			t.Errorf("entry %d trailing blank styled = %q, want empty (no user gray fill)", k, blank.styled)
		}
		// A non-header sub (last-sub+1 == number of real lines >= 1) so a collapse-click resolves to
		// a body row and is a no-op.
		if want := len(run) - 1; blank.sub != want {
			t.Errorf("entry %d blank sub = %d, want %d (last real sub + 1, non-header)", k, blank.sub, want)
		}
		if blank.sub == 0 {
			t.Errorf("entry %d blank sub = 0 (header) — a click would toggle; want a non-header sub", k)
		}
		// The run's non-trailing lines are all real content, so there is exactly ONE blank per
		// entry (no double gap).
		for i := 0; i < len(run)-1; i++ {
			if run[i].plain == "" && run[i].styled == "" {
				t.Errorf("entry %d run line %d is an unexpected extra blank (double gap)", k, i)
			}
		}
	}

	// The banner (head entry) is set off from the first user message by exactly its trailing blank:
	// run[0] is the banner and ends in a blank; run[1] begins the first user row.
	if !runHasPlain(runs[0], "swe") {
		t.Errorf("first run is not the banner; got %q", plainAll(runs[0]))
	}
	if !runHasPlain(runs[1], "hello there") {
		t.Errorf("run after the banner's blank is not the first user message; got %q", plainAll(runs[1]))
	}
}

// TestModernSelectionSpansBlankSeparator proves a drag selection that spans the blank separator
// between two entries INCLUDES the gap's newline: the blank contributes its empty plain as a middle
// row, so the extracted text carries the "\n\n" break between the entries rather than swallowing the
// gap. The fixture is exactly the shape renderFocused emits — entry 1's line, its blankSeparator(1,1),
// then entry 2's line.
func TestModernSelectionSpansBlankSeparator(t *testing.T) {
	t.Parallel()

	lines := []renderedLine{
		{styled: "A", plain: "A", entry: 1, sub: 0},
		blankSeparator(1, 1), // entry 1's trailing blank separator
		{styled: "B", plain: "B", entry: 2, sub: 0},
	}
	vp := viewportModel{lines: lines, height: len(lines), atTail: true}
	vp.reclamp()

	vp.beginSelect(0, 0) // anchor at the start of "A"
	vp.moveCursor(1, 2)  // drag to just past "B"

	if got, want := vp.SelectedText(), "A\n\nB"; got != want {
		t.Errorf("SelectedText across the gap = %q, want %q (the blank contributes the gap newline)", got, want)
	}
}

// TestModernRendersQueuedInputs is the user-facing assertion: while a turn runs, the messages a
// user fires stack up as visible, faint, "queued"-marked rows in the modern viewport (they were
// invisible before). It also locks the no-duplicate rule — once a queued message's turn starts it
// commits as a real user row and stops showing as queued.
func TestModernRendersQueuedInputs(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)

	// A turn is running on the primary loop (no user row — just an active turn).
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})

	// The user fires three messages mid-turn; each records its submit (submitResultMsg) then the
	// loop's InputQueued reveals it — the real fire-and-forget order.
	msgs := []struct {
		id   uuid.UUID
		text string
	}{
		{callID(0x21), "first queued"},
		{callID(0x22), "second queued"},
		{callID(0x23), "third queued"},
	}
	for _, q := range msgs {
		m, _ = updateModern(t, m, submitResultMsg{inputID: q.id, blocks: userBlocks(q.text)})
		m = feedModern(t, m, queuedFor(q.id, primary))
	}

	// All three render as queued rows, in submit order, each faint (QueuedStyle) and marked queued.
	rows := queuedLines(m.viewport.lines)
	if len(rows) != len(msgs) {
		t.Fatalf("queued rows = %d, want %d\nviewport:\n%s", len(rows), len(msgs), plainAll(m.viewport.lines))
	}
	for i, q := range msgs {
		if !strings.Contains(rows[i].plain, q.text) {
			t.Errorf("queued row %d plain = %q, want to contain %q", i, rows[i].plain, q.text)
		}
		if !strings.Contains(rows[i].plain, modernQueuedTag) {
			t.Errorf("queued row %d plain = %q, want the %q marker", i, rows[i].plain, modernQueuedTag)
		}
		if !strings.Contains(rows[i].styled, "\x1b[2m") { // SGR 2 = faint, styles.QueuedStyle
			t.Errorf("queued row %d not faint (QueuedStyle); styled = %q", i, rows[i].styled)
		}
	}

	// Dequeue the first: its turn starts, committing the authoritative user row. It must stop
	// showing as queued (no duplicate) while the other two remain queued.
	m = feedModern(t, m, event.TurnStarted{
		Header:  event.Header{Coordinates: identity.Coordinates{LoopID: primary}, Cause: identity.Cause{CommandID: msgs[0].id}},
		Message: userMsg("first queued"),
	})
	rows = queuedLines(m.viewport.lines)
	if len(rows) != 2 {
		t.Fatalf("queued rows after dequeue = %d, want 2\nviewport:\n%s", len(rows), plainAll(m.viewport.lines))
	}
	for _, l := range rows {
		if strings.Contains(l.plain, "first queued") {
			t.Errorf("dequeued message still shown as queued (duplicate): %q", l.plain)
		}
	}
	// It now appears as a committed (non-queued, non-live-tail) user row instead.
	committed := false
	for _, l := range m.viewport.lines {
		if l.entry != queuedTailEntryID && l.entry != liveTailEntryID && strings.Contains(l.plain, "first queued") {
			committed = true
		}
	}
	if !committed {
		t.Errorf("dequeued message not committed as a user row; viewport:\n%s", plainAll(m.viewport.lines))
	}
}
