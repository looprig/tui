package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/core/content"
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

// TestModernHeaderClickTogglesCollapse pins the per-entry fold: a plain click (no drag) on a
// thinking entry's row toggles just that entry's collapse via provenance, expanding it.
func TestModernHeaderClickTogglesCollapse(t *testing.T) {
	t.Parallel()

	primary := callID(1)
	agent := &fakeAgent{primaryLoopID: primary}
	m := newModernSized(t, agent, 80, 24)
	m = feedModern(t, m, event.TurnStarted{Header: hdr(primary)})
	m = feedModern(t, m, stepDoneFrom(primary, aiMessage("first reason\nsecond reason\nthird reason", "the answer")))

	before := len(m.viewport.lines)

	// A press then release with no intervening motion is a click, not a drag → toggle the row.
	m, _ = updateModern(t, m, tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m, _ = updateModern(t, m, tea.MouseReleaseMsg{X: 0, Y: 0, Button: tea.MouseLeft})

	if after := len(m.viewport.lines); after <= before {
		t.Errorf("header click did not expand the entry: before=%d after=%d", before, after)
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
