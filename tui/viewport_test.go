package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// keyPress builds a tea.KeyPressMsg for the nav keys the viewport handles (plus an
// arbitrary printable for the not-consumed case). Its .String() drives handleKey.
func keyPress(name string) tea.KeyPressMsg {
	switch name {
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "home":
		return tea.KeyPressMsg{Code: tea.KeyHome}
	case "end":
		return tea.KeyPressMsg{Code: tea.KeyEnd}
	default:
		return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
	}
}

// rl is a compact renderedLine constructor for viewport tests. styled is what View
// draws for an unselected line; plain is what selection measures/extracts.
func rl(id displayID, sub int, styled, plain string) renderedLine {
	return renderedLine{styled: styled, plain: plain, entry: id, sub: sub}
}

// plainLines builds n single-entry lines "line 0".."line n-1", each its own entry
// (id i+1, sub 0) with styled==plain, for scroll/offset tests where content identity
// does not matter.
func plainLines(n int) []renderedLine {
	out := make([]renderedLine, n)
	for i := 0; i < n; i++ {
		txt := "line " + string(rune('a'+i%26))
		out[i] = rl(displayID(i+1), 0, txt, txt)
	}
	return out
}

// TestViewportScrollBy locks the offset clamp at both ends and the atTail flag the
// scroll handler sets: landing at the bottom sets atTail; moving off it clears atTail;
// clamping never escapes [0, max(0, len-height)].
func TestViewportScrollBy(t *testing.T) {
	t.Parallel()

	// 20 lines, height 5 -> maxOffset 15.
	tests := []struct {
		name       string
		start      int
		delta      int
		wantOffset int
		wantTail   bool
	}{
		{name: "scroll up from middle", start: 10, delta: -3, wantOffset: 7, wantTail: false},
		{name: "scroll up clamps at 0", start: 2, delta: -10, wantOffset: 0, wantTail: false},
		{name: "scroll down to just below bottom", start: 10, delta: 3, wantOffset: 13, wantTail: false},
		{name: "scroll down lands at bottom sets tail", start: 13, delta: 5, wantOffset: 15, wantTail: true},
		{name: "scroll down clamps at bottom sets tail", start: 10, delta: 999, wantOffset: 15, wantTail: true},
		{name: "no-op stays put", start: 6, delta: 0, wantOffset: 6, wantTail: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := viewportModel{lines: plainLines(20), height: 5, offset: tt.start}
			m.scrollBy(tt.delta)
			if m.offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", m.offset, tt.wantOffset)
			}
			if m.atTail != tt.wantTail {
				t.Errorf("atTail = %v, want %v", m.atTail, tt.wantTail)
			}
		})
	}
}

// TestViewportScrollByShortContent locks that when content is shorter than the window
// maxOffset is 0, so any scroll pins at 0 and atTail stays true.
func TestViewportScrollByShortContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		delta int
	}{
		{name: "up", delta: -5},
		{name: "down", delta: 5},
		{name: "zero", delta: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := viewportModel{lines: plainLines(3), height: 10}
			m.scrollBy(tt.delta)
			if m.offset != 0 {
				t.Errorf("offset = %d, want 0", m.offset)
			}
			if !m.atTail {
				t.Errorf("atTail = false, want true (content fits window)")
			}
		})
	}
}

// TestViewportWheel locks that a wheel-up scrolls by -3 and a wheel-down by +3 through
// the mouse handler.
func TestViewportWheel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		button     tea.MouseButton
		start      int
		wantOffset int
	}{
		{name: "wheel up by 3", button: tea.MouseWheelUp, start: 10, wantOffset: 7},
		{name: "wheel down by 3", button: tea.MouseWheelDown, start: 10, wantOffset: 13},
		{name: "wheel up clamps", button: tea.MouseWheelUp, start: 1, wantOffset: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := viewportModel{lines: plainLines(20), height: 5, offset: tt.start}
			m, cmd := m.handleMouse(tea.MouseWheelMsg{Button: tt.button})
			if cmd != nil {
				t.Errorf("wheel returned a non-nil cmd")
			}
			if m.offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", m.offset, tt.wantOffset)
			}
		})
	}
}

// TestViewportKeys locks the page/home/end key deltas and that a non-nav key is not
// consumed (so Task 7 can route it elsewhere).
func TestViewportKeys(t *testing.T) {
	t.Parallel()

	// 20 lines, height 5 -> page = height-1 = 4, maxOffset 15.
	tests := []struct {
		name         string
		key          string
		start        int
		wantOffset   int
		wantTail     bool
		wantConsumed bool
	}{
		{name: "pgup", key: "pgup", start: 10, wantOffset: 6, wantTail: false, wantConsumed: true},
		{name: "pgdown", key: "pgdown", start: 10, wantOffset: 14, wantTail: false, wantConsumed: true},
		{name: "pgdown to bottom sets tail", key: "pgdown", start: 13, wantOffset: 15, wantTail: true, wantConsumed: true},
		{name: "home", key: "home", start: 10, wantOffset: 0, wantTail: false, wantConsumed: true},
		{name: "end", key: "end", start: 2, wantOffset: 15, wantTail: true, wantConsumed: true},
		{name: "unrelated key not consumed", key: "x", start: 7, wantOffset: 7, wantTail: false, wantConsumed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := viewportModel{lines: plainLines(20), height: 5, offset: tt.start}
			m, consumed := m.handleKey(keyPress(tt.key))
			if consumed != tt.wantConsumed {
				t.Fatalf("consumed = %v, want %v", consumed, tt.wantConsumed)
			}
			if m.offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", m.offset, tt.wantOffset)
			}
			if m.atTail != tt.wantTail {
				t.Errorf("atTail = %v, want %v", m.atTail, tt.wantTail)
			}
		})
	}
}

// TestViewportSetLines locks auto-follow: when atTail the offset snaps to the new
// bottom (pinned to live output); otherwise the previous offset is preserved (clamped),
// and atTail is never recomputed from counts.
func TestViewportSetLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		startLines int
		startOff   int
		atTail     bool
		newLines   int
		wantOffset int
	}{
		{name: "pinned follows new bottom", startLines: 20, startOff: 15, atTail: true, newLines: 30, wantOffset: 25},
		{name: "detached preserves offset", startLines: 20, startOff: 4, atTail: false, newLines: 30, wantOffset: 4},
		{name: "detached offset clamped when content shrinks", startLines: 30, startOff: 25, atTail: false, newLines: 8, wantOffset: 3},
		{name: "pinned to bottom of short content is 0", startLines: 20, startOff: 15, atTail: true, newLines: 3, wantOffset: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := viewportModel{lines: plainLines(tt.startLines), height: 5, offset: tt.startOff, atTail: tt.atTail}
			m.SetLines(plainLines(tt.newLines))
			if m.offset != tt.wantOffset {
				t.Errorf("offset = %d, want %d", m.offset, tt.wantOffset)
			}
			if m.atTail != tt.atTail {
				t.Errorf("atTail = %v, want %v (must not be recomputed)", m.atTail, tt.atTail)
			}
		})
	}
}

// TestSelectedText locks cell-based extraction from plain across same-line, multi-line,
// wide-rune (CJK + emoji, width 2), and empty selections.
func TestSelectedText(t *testing.T) {
	t.Parallel()

	multi := []renderedLine{
		rl(1, 0, "line one", "line one"),
		rl(1, 1, "line two", "line two"),
		rl(2, 0, "line three", "line three"),
	}

	tests := []struct {
		name string
		buf  []renderedLine
		sel  *selection
		want string
	}{
		{
			name: "same line substring by cell",
			buf:  []renderedLine{rl(1, 0, "hello world", "hello world")},
			sel:  &selection{anchor: selPoint{1, 0, 0}, cursor: selPoint{1, 0, 5}},
			want: "hello",
		},
		{
			name: "same line reversed anchor/cursor normalizes",
			buf:  []renderedLine{rl(1, 0, "hello world", "hello world")},
			sel:  &selection{anchor: selPoint{1, 0, 11}, cursor: selPoint{1, 0, 6}},
			want: "world",
		},
		{
			name: "multi line first mid last",
			buf:  multi,
			sel:  &selection{anchor: selPoint{1, 0, 5}, cursor: selPoint{2, 0, 4}},
			want: "one\nline two\nline",
		},
		{
			name: "cjk wide rune selected by cells",
			buf:  []renderedLine{rl(1, 0, "a世b", "a世b")},
			sel:  &selection{anchor: selPoint{1, 0, 1}, cursor: selPoint{1, 0, 3}},
			want: "世",
		},
		{
			name: "emoji wide rune selected by cells",
			buf:  []renderedLine{rl(1, 0, "x🚀y", "x🚀y")},
			sel:  &selection{anchor: selPoint{1, 0, 1}, cursor: selPoint{1, 0, 3}},
			want: "🚀",
		},
		{
			name: "empty selection anchor equals cursor",
			buf:  []renderedLine{rl(1, 0, "hello", "hello")},
			sel:  &selection{anchor: selPoint{1, 0, 2}, cursor: selPoint{1, 0, 2}},
			want: "",
		},
		{
			name: "nil selection",
			buf:  []renderedLine{rl(1, 0, "hello", "hello")},
			sel:  nil,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := viewportModel{lines: tt.buf, height: 10, sel: tt.sel}
			if got := m.SelectedText(); got != tt.want {
				t.Errorf("SelectedText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSelectionSurvivesReflowDuringDrag locks that once a drag is active the selection
// reads the FROZEN snapshot, so a mid-drag SetLines reflow does not move the extracted
// text.
func TestSelectionSurvivesReflowDuringDrag(t *testing.T) {
	t.Parallel()

	buf := []renderedLine{
		rl(1, 0, "alpha", "alpha"),
		rl(1, 1, "bravo", "bravo"),
		rl(2, 0, "charlie", "charlie"),
	}
	m := viewportModel{lines: buf, height: 10, offset: 0}
	m.beginSelect(0, 0) // anchor at row 0 (entry 1, sub 0), cell 0
	m.moveCursor(7, 2)  // cursor at row 2 (entry 2, sub 0), cell 7
	before := m.SelectedText()
	if before == "" {
		t.Fatalf("expected non-empty selection before reflow")
	}

	// A reflow prepends two lines; frozen must keep the selection stable.
	reflowed := []renderedLine{
		rl(9, 0, "new header", "new header"),
		rl(9, 1, "more header", "more header"),
		rl(1, 0, "alpha", "alpha"),
		rl(1, 1, "bravo", "bravo"),
		rl(2, 0, "charlie", "charlie"),
	}
	m.SetLines(reflowed)
	if got := m.SelectedText(); got != before {
		t.Errorf("during drag SelectedText() = %q, want frozen %q", got, before)
	}
}

// TestSelectionSurvivesReflowAfterRelease locks that after release (frozen cleared) the
// selection is resolved against lines by (entry, sub) identity, so a reflow that moves
// the anchored lines to new indices still extracts the same text.
func TestSelectionSurvivesReflowAfterRelease(t *testing.T) {
	t.Parallel()

	buf := []renderedLine{
		rl(1, 0, "alpha", "alpha"),
		rl(1, 1, "bravo", "bravo"),
		rl(2, 0, "charlie", "charlie"),
	}
	m := viewportModel{lines: buf, height: 10, offset: 0}
	m.beginSelect(0, 0)
	m.moveCursor(7, 2)
	want := m.SelectedText()
	m, _ = m.handleMouse(tea.MouseReleaseMsg{X: 7, Y: 2, Button: tea.MouseLeft})
	if m.frozen != nil {
		t.Fatalf("frozen must be cleared after release")
	}

	reflowed := []renderedLine{
		rl(9, 0, "new header", "new header"),
		rl(1, 0, "alpha", "alpha"),
		rl(1, 1, "bravo", "bravo"),
		rl(2, 0, "charlie", "charlie"),
	}
	m.SetLines(reflowed)
	if got := m.SelectedText(); got != want {
		t.Errorf("after release SelectedText() = %q, want %q (resolved by entry,sub)", got, want)
	}
}

// TestReleaseCopiesSelection locks that releasing a non-empty drag returns a copy cmd
// and clears the drag, while an empty click-release returns nil (no clipboard clobber).
func TestReleaseCopiesSelection(t *testing.T) {
	t.Parallel()

	buf := []renderedLine{rl(1, 0, "hello world", "hello world")}

	t.Run("non-empty drag copies", func(t *testing.T) {
		t.Parallel()
		m := viewportModel{lines: buf, height: 5}
		m, _ = m.handleMouse(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
		m, _ = m.handleMouse(tea.MouseMotionMsg{X: 5, Y: 0, Button: tea.MouseLeft})
		m, cmd := m.handleMouse(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft})
		if cmd == nil {
			t.Errorf("expected a copy cmd for a non-empty selection")
		}
		if m.frozen != nil {
			t.Errorf("frozen must be nil after release")
		}
		if m.sel == nil {
			t.Errorf("selection must remain visible after release")
		}
	})

	t.Run("empty click-release copies nothing", func(t *testing.T) {
		t.Parallel()
		m := viewportModel{lines: buf, height: 5}
		m, _ = m.handleMouse(tea.MouseClickMsg{X: 3, Y: 0, Button: tea.MouseLeft})
		_, cmd := m.handleMouse(tea.MouseReleaseMsg{X: 3, Y: 0, Button: tea.MouseLeft})
		if cmd != nil {
			t.Errorf("empty selection must return nil cmd, got non-nil")
		}
	})
}

// TestBeginSelectOutsideContent locks that a click below the content (no line there)
// starts no selection and takes no frozen snapshot.
func TestBeginSelectOutsideContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		x, y int
	}{
		{name: "below content", x: 0, y: 8},
		{name: "negative row", x: 0, y: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := viewportModel{lines: plainLines(3), height: 10}
			m.beginSelect(tt.x, tt.y)
			if m.sel != nil || m.frozen != nil {
				t.Errorf("click outside content started a selection (sel=%v frozen=%v)", m.sel, m.frozen)
			}
		})
	}
}

// TestViewSelectionHighlight locks that a selected line is drawn by re-styling its plain
// with the reverse style (never by splicing into styled, which would corrupt escapes),
// and an unselected line is drawn from its styled string verbatim.
func TestViewSelectionHighlight(t *testing.T) {
	t.Parallel()

	buf := []renderedLine{
		rl(1, 0, "\x1b[31mhello\x1b[0m", "hello"),
		rl(2, 0, "\x1b[32mworld\x1b[0m", "world"),
	}
	m := viewportModel{
		lines:  buf,
		height: 2,
		sel:    &selection{anchor: selPoint{1, 0, 0}, cursor: selPoint{1, 0, 5}},
	}
	rows := strings.Split(m.View(), "\n")
	if len(rows) != 2 {
		t.Fatalf("View() produced %d rows, want 2", len(rows))
	}

	wantSelected := selectionStyle.Render("hello")
	if rows[0] != wantSelected {
		t.Errorf("selected row = %q, want %q", rows[0], wantSelected)
	}
	if strings.Contains(rows[0], "[31m") {
		t.Errorf("selected row leaked the original styled color escape: %q", rows[0])
	}
	if rows[1] != buf[1].styled {
		t.Errorf("unselected row = %q, want styled verbatim %q", rows[1], buf[1].styled)
	}
}

// TestViewNoSelectionVerbatim locks that with no selection every visible line is drawn
// from its styled string, and the window respects offset/height.
func TestViewNoSelectionVerbatim(t *testing.T) {
	t.Parallel()

	buf := []renderedLine{
		rl(1, 0, "\x1b[31mone\x1b[0m", "one"),
		rl(2, 0, "\x1b[32mtwo\x1b[0m", "two"),
		rl(3, 0, "\x1b[33mthree\x1b[0m", "three"),
		rl(4, 0, "\x1b[34mfour\x1b[0m", "four"),
	}
	m := viewportModel{lines: buf, height: 2, offset: 1}
	want := buf[1].styled + "\n" + buf[2].styled
	if got := m.View(); got != want {
		t.Errorf("View() = %q, want %q", got, want)
	}
}

// TestCellToRune locks the cell->rune mapping used by selection: ASCII is one cell per
// rune, wide runes (CJK/emoji) count two, and a target past the end clamps to len.
func TestCellToRune(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		plain string
		cell  int
		want  int
	}{
		{name: "zero cell is rune 0", plain: "abc", cell: 0, want: 0},
		{name: "ascii cell equals rune", plain: "abc", cell: 2, want: 2},
		{name: "ascii past end clamps", plain: "abc", cell: 99, want: 3},
		{name: "after single wide rune", plain: "世界", cell: 2, want: 1},
		{name: "mid wide rune snaps past it", plain: "世界", cell: 1, want: 1},
		{name: "narrow then wide", plain: "a世b", cell: 3, want: 2},
		{name: "emoji after", plain: "x🚀y", cell: 3, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cellToRune(tt.plain, tt.cell); got != tt.want {
				t.Errorf("cellToRune(%q, %d) = %d, want %d", tt.plain, tt.cell, got, tt.want)
			}
		})
	}
	// Guard the invariant the mapping rests on: a wide rune is two cells.
	if w := lipgloss.Width("世"); w != 2 {
		t.Fatalf("precondition: lipgloss.Width(世) = %d, want 2", w)
	}
}
