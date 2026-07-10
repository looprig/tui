package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
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

// TestModernRegionAt pins the frame's region hit-testing (content vs status vs bar vs box),
// the routing the mouse handler depends on.
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
		{name: "bar row", y: lay.barY, want: regionBar},
		{name: "box row", y: lay.boxTop, want: regionBox},
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

// TestScreenSubscribeUsesPrimaryOnlyFilter is the contrast case: the scrollback Screen keeps
// the primary-only Ephemeral scope after the filter-injection refactor — its behavior is
// unchanged by the shared seam.
func TestScreenSubscribeUsesPrimaryOnlyFilter(t *testing.T) {
	t.Parallel()

	primary := callID(4)
	agent := &fakeAgent{primaryLoopID: primary}
	m := New(context.Background(), agent, fakeOpen(agent), AgentBanner{})

	_ = m.subscribe()()

	if agent.subFilter.Ephemeral.All {
		t.Error("scrollback Ephemeral scope = All, want primary-only")
	}
	if _, ok := agent.subFilter.Ephemeral.Loops[primary]; !ok {
		t.Errorf("scrollback Ephemeral scope missing the primary loop; got %+v", agent.subFilter.Ephemeral)
	}
}

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
	m := newModernSized(t, agent, 80, 8) // small height so content scrolls

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
