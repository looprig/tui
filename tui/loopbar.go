package tui

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/looprig/cli/tui/styles"
	"github.com/looprig/core/uuid"
)

// loopBarEntry is one loop's assembled row for the modern active-loops bar: its id (the
// focus target a click resolves to), its display name, and its liveness + gate flags. It is
// PURE presentation data — the caller (Task 7) maps the transcript's loops() + interaction
// state into these, so the bar depends on no transcript/agent type. live and gate are
// independent: a loop can be live-and-gated, idle-and-gated, etc.
type loopBarEntry struct {
	id         uuid.UUID
	name       string
	live, gate bool
}

// loopBar renders the session's loops as one clickable bar line and hit-tests a click
// column back to a loop id (design §Active-loops bar). It is a PURE view: entries are the
// assembled rows in stable (creation) order, focused is the currently focused loop id, and
// max is the VISIBLE CAP — at most max entries render, the rest fold into a "… +N" overflow
// marker so the bar never grows unbounded. A max <= 0 means no count cap. The segment layout
// is width-INDEPENDENT, so HitTest (which takes no width) can never disagree with Render.
type loopBar struct {
	entries []loopBarEntry
	focused uuid.UUID
	max     int
}

// Bar glyphs (design §Active-loops bar): a leading mark per segment — focused wins over live
// wins over idle — plus a trailing gate mark. The id short form and separators keep the row
// compact.
const (
	barFocusedMark = "▸"  // this loop is focused
	barLiveMark    = "•"  // this loop is live (running / mid-turn)
	barIdleMark    = "∘"  // this loop is parked idle
	barGateMark    = "!"  // this loop has a pending gate awaiting the user
	barIDSep       = "·"  // between a loop's name and its short id
	barSep         = "  " // between adjacent segments (and before the overflow marker)
	barIDLen       = 4    // leading hex chars of the loop id shown as its short form
)

// barSeg is one rendered segment's provenance: the loop id it maps to and the half-open
// cell-column span [start, end) it occupies in the rendered line. HitTest scans these so a
// click maps to exactly the loop whose segment covers the column; separators, the overflow
// marker, and columns past the last segment carry no barSeg and hit-test to (_, false).
type barSeg struct {
	id    uuid.UUID
	start int
	end   int
}

// Render draws the bar as a single line: each visible loop's "<mark><name>·<id4>" (plus a
// trailing "!" when gated), joined by a small gap, followed by a "… +N" overflow marker when
// the visible cap hides loops. The visible set is bounded by max (not width), so a wide frame
// and a narrow one draw the same segments in the same columns — which is exactly why HitTest
// can resolve a click without a width. A non-positive width (a degenerate frame) draws
// nothing; an empty bar renders "". The row is returned unpadded/untruncated so the caller
// can compose it with a right-aligned status; the max cap keeps it within a normal frame and
// the terminal clips any overflow (from the tail, which carries no clicks).
func (b loopBar) Render(width int) string {
	if width <= 0 {
		return ""
	}
	_, line := b.layout()
	return line
}

// HitTest maps cell column x to the loop id whose rendered segment covers it, returning
// (id, true) for an in-segment column and (uuid.UUID{}, false) for a gap between segments,
// the "… +N" overflow marker, or any column past the last segment. It computes the SAME
// width-independent layout Render draws (segment widths via lipgloss.Width), so the returned
// id is always the loop actually drawn at x.
func (b loopBar) HitTest(x int) (uuid.UUID, bool) {
	segs, _ := b.layout()
	for _, s := range segs {
		if x >= s.start && x < s.end {
			return s.id, true
		}
	}
	return uuid.UUID{}, false
}

// cycle moves focus by dir over the VISIBLE order — the bar's entries in their stable
// creation order — wrapping at both ends, and returns the newly focused loop id. dir > 0
// steps forward, dir < 0 steps back, dir == 0 leaves focus unchanged. When the current focus
// is not among the entries it seeds at the first entry (forward) or the last (backward). An
// empty bar returns the current focused id unchanged (nothing to cycle). Focusing a
// currently-overflowed loop is fine — the focused loop is always kept visible by layout, so
// it comes into view on the next Render.
func (b loopBar) cycle(dir int) uuid.UUID {
	n := len(b.entries)
	if n == 0 || dir == 0 {
		return b.focused
	}
	step := 1
	if dir < 0 {
		step = -1
	}
	cur := -1
	for i := range b.entries {
		if b.entries[i].id == b.focused {
			cur = i
			break
		}
	}
	if cur < 0 {
		if step > 0 {
			return b.entries[0].id
		}
		return b.entries[n-1].id
	}
	next := ((cur+step)%n + n) % n
	return b.entries[next].id
}

// layout is the single source of the bar's geometry, shared by Render and HitTest so a drawn
// column and a hit-tested column can never disagree. It selects the kept set — prioritizing
// the focused loop, then live loops, then the most-recent idle loops, capped at max — then
// renders those loops (in stable display order, with their cell spans) plus the "… +N"
// overflow marker for the hidden loops. It returns the kept segments and the rendered line.
func (b loopBar) layout() ([]barSeg, string) {
	if len(b.entries) == 0 {
		return nil, ""
	}
	return b.render(b.keptByPriority())
}

// keptByPriority returns the loops to display, in stable creation order, capped at max
// (max <= 0 means no cap). When the cap bites, it keeps the highest-priority loops: priority
// is focused first, then live, then the most-recent idle (higher index = more recent), so the
// focused and live loops are the ones that survive the cut (design §bar).
func (b loopBar) keptByPriority() []loopBarEntry {
	if b.max <= 0 || len(b.entries) <= b.max {
		return append([]loopBarEntry(nil), b.entries...)
	}
	order := b.priorityOrder()
	keep := make(map[int]bool, b.max)
	for _, idx := range order[:b.max] {
		keep[idx] = true
	}
	var out []loopBarEntry
	for i := range b.entries {
		if keep[i] {
			out = append(out, b.entries[i])
		}
	}
	return out
}

// priorityOrder returns entry indices ordered highest-priority first (focused, then live,
// then most-recent idle), a stable ordering used to pick the kept set under a cap. It never
// reorders the DISPLAY — only which loops survive the cut — so the bar itself stays in
// creation order.
func (b loopBar) priorityOrder() []int {
	idx := make([]int, len(b.entries))
	for i := range idx {
		idx[i] = i
	}
	// Insertion sort by descending priority: n is tiny (a capped handful) and this avoids a
	// sort import while staying stable (equal priorities keep creation order).
	for i := 1; i < len(idx); i++ {
		for j := i; j > 0 && b.priority(idx[j]) > b.priority(idx[j-1]); j-- {
			idx[j], idx[j-1] = idx[j-1], idx[j]
		}
	}
	return idx
}

// priority scores entry i for the visible cap: focused ranks above every live loop, which
// ranks above every idle loop; within a band the more-recent loop (higher index) ranks higher
// so recency breaks ties. The bands are spaced by len(entries) so no index crosses a band.
func (b loopBar) priority(i int) int {
	base := i // recency: later loops rank higher within a band
	switch {
	case b.entries[i].id == b.focused:
		return 2*len(b.entries) + base
	case b.entries[i].live:
		return len(b.entries) + base
	default:
		return base
	}
}

// render builds the kept loops into styled segments (with their plain cell spans recorded for
// HitTest) plus the trailing overflow marker, and returns both the spans and the joined line.
// Cell spans are measured on the PLAIN text (styling is zero-width ANSI), so the spans stay
// valid over the styled line — the same plain-measured / styled-drawn split viewport.go uses.
func (b loopBar) render(kept []loopBarEntry) ([]barSeg, string) {
	var (
		segs []barSeg
		sb   strings.Builder
		col  int
	)
	for i := range kept {
		if i > 0 {
			sb.WriteString(barSep)
			col += lipgloss.Width(barSep)
		}
		plain := b.segPlain(kept[i])
		w := lipgloss.Width(plain)
		segs = append(segs, barSeg{id: kept[i].id, start: col, end: col + w})
		sb.WriteString(b.segStyled(kept[i]))
		col += w
	}
	if hidden := len(b.entries) - len(kept); hidden > 0 {
		sb.WriteString(barSep)
		sb.WriteString(styles.StatusStyle.Render(overflowText(hidden)))
	}
	return segs, sb.String()
}

// segPlain is a loop segment's ANSI-free text — "<mark><name>·<id4>" plus a trailing "!" when
// gated — the form HitTest measures cell spans on. The leading mark encodes focus/live/idle
// (leadMark); the gate mark is a SUFFIX so it never shifts the leading mark.
func (b loopBar) segPlain(e loopBarEntry) string {
	s := b.leadMark(e) + e.name + barIDSep + shortLoopID(e.id)
	if e.gate {
		s += barGateMark
	}
	return s
}

// segStyled is segPlain with tasteful styling: the focused loop bold (it is the one the
// viewport tracks), every other loop faint (subordinate context), and a pending gate's "!" in
// the warn color so it reads as action-required. Styling is zero-width, so segStyled has the
// same display width as segPlain and the recorded cell spans stay valid.
func (b loopBar) segStyled(e loopBarEntry) string {
	body := b.leadMark(e) + e.name + barIDSep + shortLoopID(e.id)
	style := styles.StatusStyle
	if e.id == b.focused {
		style = styles.HeadlineStyle
	}
	out := style.Render(body)
	if e.gate {
		out += styles.NoticeWarnStyle.Render(barGateMark)
	}
	return out
}

// leadMark is a segment's leading glyph: focused wins over live wins over idle (design §bar).
// It reads the bar's focused id, so segPlain and segStyled agree on the mark and HitTest's
// cell spans line up with the drawn marks.
func (b loopBar) leadMark(e loopBarEntry) string {
	switch {
	case e.id == b.focused:
		return barFocusedMark
	case e.live:
		return barLiveMark
	default:
		return barIdleMark
	}
}

// overflowText is the "… +N" marker shown when the visible cap hides N loops.
func overflowText(hidden int) string {
	return "… +" + strconv.Itoa(hidden)
}

// shortLoopID is a loop id's compact bar form: the leading barIDLen hex chars of its uuid
// string (e.g. "a1b2"), a stable, terse id for a bar segment. A shorter string (should not
// happen for a real uuid) is returned as-is.
func shortLoopID(id uuid.UUID) string {
	s := id.String()
	if len(s) >= barIDLen {
		return s[:barIDLen]
	}
	return s
}
