package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/looprig/tui/styles"
)

var trayListRows = []completionTrayRow{
	{primary: "/clear", secondary: "start a new conversation"},
	{primary: "/compact", secondary: "compact the current conversation"},
	{primary: "/exit", secondary: "exit Looprig"},
}

// TestTrayListMatchesTheHandRolledTray is the gate on the whole migration. With the zero-value
// layout the engine must produce the hand-rolled tray's exact bytes, because the four panels
// swap onto it one at a time: any drift here would show up as one tray looking different from
// the other three, which is the drift the shared treatment exists to prevent.
func TestTrayListMatchesTheHandRolledTray(t *testing.T) {
	t.Parallel()

	for _, width := range []int{20, 44, 80} {
		for selected := range trayListRows {
			tray := newTrayList(trayListRows, width, trayLayout{})
			for range selected {
				tray.Down()
			}
			got := tray.View()
			if want := renderCompletionTray(trayListRows, selected, width); got != want {
				t.Errorf("width %d selected %d:\n got %q\nwant %q", width, selected, got, want)
			}
		}
	}
}

// TestTrayListCursorWraps pins InfiniteScrolling. list.Model clamps at the ends by default,
// and the hand-rolled panels all wrap with a modulo cursor, so the engine has to wrap too or
// every tray gains a dead end at both extremes.
func TestTrayListCursorWraps(t *testing.T) {
	t.Parallel()

	tray := newTrayList(trayListRows, 40, trayLayout{})
	tray.Up()
	if got, want := tray.Cursor(), len(trayListRows)-1; got != want {
		t.Errorf("cursor after up from the top = %d, want %d (wrap to the bottom)", got, want)
	}
	if got := tray.Selected().primary; got != "/exit" {
		t.Errorf("selected after wrapping up = %q, want %q", got, "/exit")
	}
	tray.Down()
	if got := tray.Cursor(); got != 0 {
		t.Errorf("cursor after down from the bottom = %d, want 0 (wrap to the top)", got)
	}
	if got := tray.Selected().primary; got != "/clear" {
		t.Errorf("selected after wrapping down = %q, want %q", got, "/clear")
	}
}

// TestTrayListStackedLayout covers the session tray's shape: two rows per item, PadV spacers
// between entries but not after the last, and a band that covers BOTH of the selected item's
// rows. A band on only the title row would read as a one-row entry with an orphan beneath it.
func TestTrayListStackedLayout(t *testing.T) {
	t.Parallel()

	const width = 40
	rows := []completionTrayRow{
		{primary: "First session", secondary: "yesterday - abc123"},
		{primary: "Second session", secondary: "today - def456"},
	}
	tray := newTrayList(rows, width, trayLayout{Stacked: true, PadV: 1})
	tray.Down() // select the second entry

	lines := strings.Split(tray.View(), "\n")
	// 2 items * 2 content rows + 1 spacer between them, and nothing after the last.
	if len(lines) != 5 {
		t.Fatalf("stacked view has %d rows, want 5:\n%q", len(lines), lines)
	}

	wantPlain := []string{"First session", "yesterday - abc123", "", "Second session", "today - def456"}
	for i, want := range wantPlain {
		plain := strings.TrimSpace(strings.TrimPrefix(stripANSI(lines[i]), styles.AccentBar))
		if plain != want {
			t.Errorf("row %d = %q, want %q", i, plain, want)
		}
		if got := lipgloss.Width(lines[i]); got != width {
			t.Errorf("row %d width = %d, want %d", i, got, width)
		}
	}
	// The spacer still carries the rail, so the tray's left edge runs unbroken.
	if !strings.HasPrefix(stripANSI(lines[2]), styles.AccentBar) {
		t.Errorf("spacer row = %q, want it to carry the rail", stripANSI(lines[2]))
	}

	band := selectedBandOpen(t)
	for i, line := range lines {
		banded := strings.HasPrefix(line, band)
		want := i == 3 || i == 4 // both rows of the selected entry, and only those
		if banded != want {
			t.Errorf("row %d banded = %v, want %v: %q", i, banded, want, line)
		}
	}
}

// TestTrayListPadH pins the horizontal inset: content moves in by PadH on BOTH sides while
// the row still fills the whole width, so a padded tray reads as an inset block rather than
// as a narrower one.
func TestTrayListPadH(t *testing.T) {
	t.Parallel()

	const (
		width = 30
		pad   = 3
	)
	rows := []completionTrayRow{{primary: strings.Repeat("x", 60)}}
	tray := newTrayList(rows, width, trayLayout{PadH: pad})

	line := tray.View()
	if got := lipgloss.Width(line); got != width {
		t.Errorf("row width = %d, want the full %d", got, width)
	}
	plain := stripANSI(line)
	// Rail, then the usual single space, then PadH more before any content.
	if want := styles.AccentBar + strings.Repeat(" ", 1+pad) + "x"; !strings.HasPrefix(plain, want) {
		t.Errorf("row = %q, want it to start %q", plain, want)
	}
	// The right inset is honored by clamping short of the width, so the trailing columns
	// stay blank even though the fill runs all the way.
	if got := lipgloss.Width(strings.TrimRight(plain, " ")); got > width-pad {
		t.Errorf("content reaches column %d, want it to stop by %d", got, width-pad)
	}
}

// TestTrayListUnderlinesFuzzyMatches pins the filter affordance. It asserts on an UNSELECTED
// row on purpose: styles.SelectedRow strips inner styling on its light fill, so the selected
// row never carries an underline and asserting there would test the band, not the match.
func TestTrayListUnderlinesFuzzyMatches(t *testing.T) {
	t.Parallel()

	tray := newTrayList(trayListRows, 60, trayLayout{})
	// "c" matches /clear and /compact, so the render has an unselected matched row to
	// assert on; the filter puts the cursor back on the first.
	tray.m.SetFilterText("c")

	lines := strings.Split(tray.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("filter matched %d rows, want at least 2 so one is unselected:\n%q", len(lines), lines)
	}

	// The matched rune as the tray draws it. Composed through the same style rather than
	// spelled as a raw escape, so the assertion cannot drift from what lipgloss emits.
	wantMatch := trayMatchStyle.Render("c")
	var checked int
	for i, line := range lines {
		if i == tray.Cursor() {
			continue // banded: SelectedRow strips the underline, by design
		}
		checked++
		if !strings.Contains(line, wantMatch) {
			t.Errorf("unselected filtered row %d does not underline its match: %q", i, line)
		}
	}
	if checked == 0 {
		t.Fatal("every matched row was the selected one; the test asserted nothing")
	}

	// Underlining is the FILTER's affordance: an unfiltered tray must not draw it.
	if unfiltered := newTrayList(trayListRows, 60, trayLayout{}).View(); strings.Contains(unfiltered, wantMatch) {
		t.Errorf("unfiltered tray underlines runes: %q", unfiltered)
	}
}

// TestTrayListFiltersOnThePrimaryOnly pins the other half of trayItem.FilterValue. The
// secondary is deliberately NOT filterable: list.MatchesForItem hands back rune indices into
// whatever FilterValue returns, and trayDelegate underlines them in the primary, so a match
// that landed in the secondary would underline an unrelated rune of the primary instead.
func TestTrayListFiltersOnThePrimaryOnly(t *testing.T) {
	t.Parallel()

	// "conversation" appears in a secondary and in no primary, not even as a subsequence.
	tray := newTrayList(trayListRows, 60, trayLayout{})
	tray.m.SetFilterText("conversation")
	if got := tray.View(); got != "" {
		t.Errorf("a term found only in a secondary matched %q, want no rows", got)
	}
}

// TestTrayListSelectWindowRow covers pointing at a row: an entry's content rows select it,
// the spacer between entries is inert, and rows past the window are ignored.
func TestTrayListSelectWindowRow(t *testing.T) {
	t.Parallel()

	rows := []completionTrayRow{
		{primary: "First", secondary: "one"},
		{primary: "Second", secondary: "two"},
	}
	tray := newTrayList(rows, 40, trayLayout{Stacked: true, PadV: 1})

	// Row 4 is the second content row of entry 2; both of an entry's rows select it.
	if !tray.SelectWindowRow(4, 5) {
		t.Error("SelectWindowRow(second row of entry 2) reported no change, want a move")
	}
	if got := tray.Selected().primary; got != "Second" {
		t.Errorf("selected = %q, want %q", got, "Second")
	}
	if tray.SelectWindowRow(4, 5) {
		t.Error("re-selecting the current entry reported a change, want none")
	}
	// Row 2 is the spacer BELOW entry 1. The cursor is on entry 2, so an implementation
	// that let a spacer fall through to its owning entry would move -- which is exactly
	// what must not happen: a spacer belongs to no item.
	if tray.SelectWindowRow(2, 5) {
		t.Error("SelectWindowRow(spacer) reported a change, want the spacer inert")
	}
	if got := tray.Selected().primary; got != "Second" {
		t.Errorf("clicking the spacer moved the cursor to %q, want it left on %q", got, "Second")
	}
	if tray.SelectWindowRow(99, 5) {
		t.Error("SelectWindowRow past the window reported a change, want none")
	}
}

// TestTrayListViewWindow caps the tray to the rows the surface can spare.
func TestTrayListViewWindow(t *testing.T) {
	t.Parallel()

	tray := newTrayList(trayListRows, 40, trayLayout{})
	if got := lipgloss.Height(tray.ViewWindow(40, 2)); got != 2 {
		t.Errorf("ViewWindow(maxRows=2) height = %d, want 2", got)
	}
	if got := tray.ViewWindow(40, 0); got != "" {
		t.Errorf("ViewWindow(maxRows=0) = %q, want empty", got)
	}
	if got := tray.ViewWidth(0); got != "" {
		t.Errorf("ViewWidth(0) = %q, want empty", got)
	}
	// A stacked entry that cannot fit its content rows renders nothing rather than half an
	// entry: a title with its metadata cut off is worse than no tray.
	stacked := newTrayList(trayListRows, 40, trayLayout{Stacked: true})
	if got := stacked.ViewWindow(40, 1); got != "" {
		t.Errorf("stacked ViewWindow(maxRows=1) = %q, want empty", got)
	}
	// The bg argument is dead; ViewWindowBackground must agree with ViewWindow.
	if got, want := tray.ViewWindowBackground(40, 2, lipgloss.Color("#112233")), tray.ViewWindow(40, 2); got != want {
		t.Errorf("ViewWindowBackground honored the provided fill:\n got %q\nwant %q", got, want)
	}
}

// TestTrayListEmpty pins the degenerate cases. An empty tray renders nothing rather than
// list.Model's "No items." placeholder, and Selected stays safe to call.
func TestTrayListEmpty(t *testing.T) {
	t.Parallel()

	tray := newTrayList(nil, 40, trayLayout{})
	if got := tray.View(); got != "" {
		t.Errorf("empty tray View() = %q, want empty", got)
	}
	if got := tray.Selected(); got != (trayItem{}) {
		t.Errorf("empty tray Selected() = %+v, want the zero item", got)
	}
	if tray.SelectWindowRow(0, 4) {
		t.Error("SelectWindowRow on an empty tray reported a change, want none")
	}
}
