package presentation

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/looprig/core/uuid"
)

// barSegOf builds the expected ANSI-free segment text for a mark/name/id, mirroring
// loopBar.segBody ("<mark> <name> (<id4>)") so the render assertions stay pinned to the
// component's own layout.
func barSegOf(mark, name string, id uuid.UUID) string {
	return mark + barMarkSep + name + barIDOpen + shortLoopID(id) + barIDClose
}

// TestLoopBarRender covers the bar's one-line render: the leading focused ● / unfocused ○
// marks, the trailing gate "!", the visible cap + "… +N" overflow marker, and the degenerate
// empty / zero-width frames.
func TestLoopBarRender(t *testing.T) {
	t.Parallel()

	l0, l1, l2, l3, l4 := loopID(0x01), loopID(0x02), loopID(0x03), loopID(0x04), loopID(0x05)

	tests := []struct {
		name    string
		bar     loopBar
		width   int
		contain []string
		absent  []string
	}{
		{
			name: "focused ● vs unfocused ○ marks",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "main", live: true},
					{id: l1, name: "reviewer", live: true},
					{id: l2, name: "tester", live: false},
				},
				focused: l0,
			},
			width: 80,
			contain: []string{
				barSegOf(barFocusedMark, "main", l0),
				barSegOf(barUnfocusedMark, "reviewer", l1),
				barSegOf(barUnfocusedMark, "tester", l2),
			},
			absent: []string{"… +"},
		},
		{
			name: "pending gate renders a trailing !",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "main", live: true},
					{id: l1, name: "reviewer", live: true, gate: true},
				},
				focused: l0,
			},
			width:   80,
			contain: []string{barSegOf(barUnfocusedMark, "reviewer", l1) + barGateMark},
		},
		{
			name: "visible cap folds the rest into … +N (focused + live kept)",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "main", live: true},
					{id: l1, name: "a", live: true},
					{id: l2, name: "b", live: false},
					{id: l3, name: "c", live: false},
					{id: l4, name: "d", live: false},
				},
				focused: l0,
				max:     2,
			},
			width: 80,
			contain: []string{
				barSegOf(barFocusedMark, "main", l0),
				barSegOf(barUnfocusedMark, "a", l1),
				overflowText(3),
			},
			absent: []string{
				barSegOf(barUnfocusedMark, "b", l2),
				barSegOf(barUnfocusedMark, "c", l3),
				barSegOf(barUnfocusedMark, "d", l4),
			},
		},
		{
			// The focused loop and most-recent live loop survive.
			name: "focused and recent live loops survive",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "main", live: false},
					{id: l1, name: "a", live: true},
					{id: l2, name: "b", live: true},
					{id: l3, name: "c", live: true},
					{id: l4, name: "d", live: true},
				},
				focused: l1,
				max:     2,
			},
			width: 80,
			contain: []string{
				barSegOf(barFocusedMark, "a", l1), // focused kept
				barSegOf(barUnfocusedMark, "d", l4),
				overflowText(3),
			},
			absent: []string{
				barSegOf(barUnfocusedMark, "b", l2),
				barSegOf(barUnfocusedMark, "c", l3),
				barSegOf(barUnfocusedMark, "main", l0),
			},
		},
		{
			name:   "empty bar renders nothing",
			bar:    loopBar{},
			width:  80,
			absent: []string{barFocusedMark, barUnfocusedMark},
		},
		{
			name: "non-positive width renders nothing",
			bar: loopBar{
				entries: []loopBarEntry{{id: l0, name: "main", live: true}},
				focused: l0,
			},
			width:  0,
			absent: []string{"main"},
		},
		{
			name: "max<=0 shows all with no overflow",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "main", live: true},
					{id: l1, name: "a", live: false},
					{id: l2, name: "b", live: false},
				},
				focused: l0,
				max:     0,
			},
			width: 80,
			contain: []string{
				barSegOf(barFocusedMark, "main", l0),
				barSegOf(barUnfocusedMark, "a", l1),
				barSegOf(barUnfocusedMark, "b", l2),
			},
			absent: []string{"… +"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plain := stripANSI(tt.bar.Render(tt.width))
			for _, w := range tt.contain {
				if !strings.Contains(plain, w) {
					t.Errorf("Render(%d) = %q, want to contain %q", tt.width, plain, w)
				}
			}
			for _, w := range tt.absent {
				if strings.Contains(plain, w) {
					t.Errorf("Render(%d) = %q, want NOT to contain %q", tt.width, plain, w)
				}
			}
		})
	}
}

// TestLoopBarPriorityActiveFocus proves the visible-cap priority is focused → active →
// live → most-recent idle, including coincident focused/active selection.
func TestLoopBarPriorityActiveFocus(t *testing.T) {
	t.Parallel()

	l0, l1, l2 := loopID(0x01), loopID(0x02), loopID(0x03)
	l3, l4, l5 := loopID(0x04), loopID(0x05), loopID(0x06)

	tests := []struct {
		name    string
		bar     loopBar
		width   int
		contain []string
		absent  []string
	}{
		{
			// Focused and active survive with the most-recent live loop.
			name: "focused active and recent live survive",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "idle0", live: false},
					{id: l1, name: "active", live: false},
					{id: l2, name: "focused", live: false},
					{id: l3, name: "live1", live: true},
					{id: l4, name: "live2", live: true},
					{id: l5, name: "idle", live: false},
				},
				focused: l2, active: l1, max: 3,
			},
			width: 120,
			contain: []string{
				barSegOf(barFocusedMark, "focused", l2),
				barSegOf(barUnfocusedMark, "active", l1),
				barSegOf(barUnfocusedMark, "live2", l4),
				overflowText(3),
			},
			absent: []string{"idle0", "live1"},
		},
		{
			// Coincident focus and active selection still survives the tightest cap.
			name: "coincident focused and active survive the cap",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "hub", live: false},
					{id: l1, name: "live1", live: true},
					{id: l2, name: "live2", live: true},
					{id: l3, name: "live3", live: true},
				},
				focused: l0, active: l0, max: 1,
			},
			width:   120,
			contain: []string{barSegOf(barFocusedMark, "hub", l0), overflowText(3)},
			absent:  []string{"live1", "live2", "live3"},
		},
		{
			// Focused and active occupy the two privileged bands.
			name: "focused and active outrank idle under the cap",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "idle0", live: false},
					{id: l1, name: "active", live: false},
					{id: l2, name: "focused", live: false},
				},
				focused: l2, active: l1, max: 2,
			},
			width: 120,
			contain: []string{
				barSegOf(barFocusedMark, "focused", l2),
				barSegOf(barUnfocusedMark, "active", l1),
				overflowText(1),
			},
			absent: []string{barSegOf(barUnfocusedMark, "idle0", l0)},
		},
		{
			// No focused/active privilege among these (they point off-list), so the live
			// loop and the most-recent idle survive by band then recency — live outranks idle.
			name: "live outranks idle within the lower bands",
			bar: loopBar{
				entries: []loopBarEntry{
					{id: l0, name: "idleOld", live: false},
					{id: l1, name: "liveMid", live: true},
					{id: l2, name: "idleNew", live: false},
				},
				focused: loopID(0xFF), active: loopID(0xFE), max: 2,
			},
			width: 120,
			contain: []string{
				barSegOf(barUnfocusedMark, "liveMid", l1),
				barSegOf(barUnfocusedMark, "idleNew", l2),
			},
			absent: []string{"idleOld"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			plain := stripANSI(tt.bar.Render(tt.width))
			for _, w := range tt.contain {
				if !strings.Contains(plain, w) {
					t.Errorf("Render(%d) = %q, want to contain %q", tt.width, plain, w)
				}
			}
			for _, w := range tt.absent {
				if strings.Contains(plain, w) {
					t.Errorf("Render(%d) = %q, want NOT to contain %q", tt.width, plain, w)
				}
			}
		})
	}
}

// TestLoopBarHitTest proves HitTest's cell spans line up with what Render draws: each
// segment's drawn column span equals its plain text, the span ends map back to the loop id,
// and gaps / the overflow marker / past-the-end / negative columns resolve to nothing.
func TestLoopBarHitTest(t *testing.T) {
	t.Parallel()

	l0, l1, l2 := loopID(0x01), loopID(0x02), loopID(0x03)
	bar := loopBar{
		entries: []loopBarEntry{
			{id: l0, name: "main", live: true},
			{id: l1, name: "reviewer", live: true, gate: true},
			{id: l2, name: "tester", live: false},
		},
		focused: l0,
	}

	segs, line := bar.layout()
	plain := stripANSI(line)
	if len(segs) != 3 {
		t.Fatalf("layout segs = %d, want 3", len(segs))
	}

	for i, s := range segs {
		want := bar.segPlain(bar.entries[i])
		if got := cellSpan(plain, s.start, s.end); got != want {
			t.Errorf("seg %d drawn span [%d,%d) = %q, want %q (HitTest spans must line up with Render)", i, s.start, s.end, got, want)
		}
		if id, ok := bar.HitTest(s.start); !ok || id != s.id {
			t.Errorf("HitTest(%d start) = (%v,%v), want (%v,true)", s.start, id, ok, s.id)
		}
		if id, ok := bar.HitTest(s.end - 1); !ok || id != s.id {
			t.Errorf("HitTest(%d end-1) = (%v,%v), want (%v,true)", s.end-1, id, ok, s.id)
		}
	}

	// The separator gap between two segments hit-tests to nothing.
	gap := segs[0].end
	if segs[1].start <= gap {
		t.Fatalf("expected a gap between seg0.end=%d and seg1.start=%d", segs[0].end, segs[1].start)
	}
	if id, ok := bar.HitTest(gap); ok {
		t.Errorf("HitTest(gap %d) = (%v,true), want (_,false)", gap, id)
	}

	// Past the last segment, far past, and a negative column all hit-test to nothing.
	past := segs[len(segs)-1].end
	for _, x := range []int{past, past + 100, -1} {
		if id, ok := bar.HitTest(x); ok {
			t.Errorf("HitTest(%d) = (%v,true), want (_,false)", x, id)
		}
	}

	// The overflow marker is not clickable: a click on "… +N" resolves to nothing.
	capped := loopBar{
		entries: []loopBarEntry{
			{id: l0, name: "main", live: true},
			{id: l1, name: "a", live: false},
			{id: l2, name: "b", live: false},
		},
		focused: l0,
		max:     1,
	}
	csegs, cline := capped.layout()
	if len(csegs) != 1 {
		t.Fatalf("capped segs = %d, want 1", len(csegs))
	}
	markerCol := lipgloss.Width(stripANSI(cline)) - 1 // the last column falls inside the marker
	if markerCol <= csegs[0].end {
		t.Fatalf("expected the marker past seg0.end=%d, got last col %d", csegs[0].end, markerCol)
	}
	if id, ok := capped.HitTest(markerCol); ok {
		t.Errorf("HitTest(overflow marker %d) = (%v,true), want (_,false)", markerCol, id)
	}
}

// TestLoopBarCycle covers focus cycling over the visible (creation) order with wrapping at
// both ends, the direction sign, the unknown-focus seed, and the empty-bar no-op.
func TestLoopBarCycle(t *testing.T) {
	t.Parallel()

	a, b, c := loopID(0x0A), loopID(0x0B), loopID(0x0C)
	x := loopID(0xFF) // not among the entries
	entries := []loopBarEntry{{id: a, name: "a"}, {id: b, name: "b"}, {id: c, name: "c"}}

	tests := []struct {
		name    string
		entries []loopBarEntry
		focused uuid.UUID
		dir     int
		want    uuid.UUID
	}{
		{name: "forward", entries: entries, focused: a, dir: 1, want: b},
		{name: "forward wraps at the end", entries: entries, focused: c, dir: 1, want: a},
		{name: "backward", entries: entries, focused: b, dir: -1, want: a},
		{name: "backward wraps at the start", entries: entries, focused: a, dir: -1, want: c},
		{name: "dir 0 is a no-op", entries: entries, focused: b, dir: 0, want: b},
		{name: "unknown focus seeds first on forward", entries: entries, focused: x, dir: 1, want: a},
		{name: "unknown focus seeds last on backward", entries: entries, focused: x, dir: -1, want: c},
		{name: "any positive dir steps exactly once", entries: entries, focused: a, dir: 5, want: b},
		{name: "any negative dir steps exactly once", entries: entries, focused: b, dir: -9, want: a},
		{name: "empty bar returns focus unchanged", entries: nil, focused: x, dir: 1, want: x},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bar := loopBar{entries: tt.entries, focused: tt.focused}
			if got := bar.cycle(tt.dir); got != tt.want {
				t.Errorf("cycle(%d) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}

// TestShortLoopID covers the compact bar id form (the leading 4 hex chars of the uuid).
func TestShortLoopID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   uuid.UUID
		want string
	}{
		{name: "leading hex group", in: loopID(0xAB), want: "ab00"},
		{name: "zero id", in: uuid.UUID{}, want: "0000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := shortLoopID(tt.in); got != tt.want {
				t.Errorf("shortLoopID(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
