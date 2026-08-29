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

// TestTrayListSkipsNonSelectableRows is the provider-group invariant for the model picker:
// group headings are visible structure, never values an operator can select. The shared tray
// has to enforce it so keyboard and pointer navigation agree instead of letting Enter land on
// a provider heading with no model payload behind it.
func TestTrayListSkipsNonSelectableRows(t *testing.T) {
	t.Parallel()

	tray := newTrayList([]completionTrayRow{
		{primary: "ANTHROPIC", kind: trayRowHeading},
		{primary: "Claude Sonnet 4.5"},
		{primary: "Claude Opus 4.1"},
	}, 60, trayLayout{})

	if got := tray.Selected().primary; got != "Claude Sonnet 4.5" {
		t.Fatalf("initial selection = %q, want first model rather than the provider heading", got)
	}
	tray.Up()
	if got := tray.Selected().primary; got != "Claude Opus 4.1" {
		t.Fatalf("Up() selected %q, want wrapped model and no provider heading", got)
	}
	tray.Down()
	if got := tray.Selected().primary; got != "Claude Sonnet 4.5" {
		t.Fatalf("Down() selected %q, want first model and no provider heading", got)
	}
	if tray.SelectWindowRow(0, 3) {
		t.Fatal("SelectWindowRow(provider heading) reported a move, want provider headings inert")
	}
	if !tray.SelectWindowRow(2, 3) || tray.Selected().primary != "Claude Opus 4.1" {
		t.Fatalf("SelectWindowRow(model) selected %q, want Claude Opus 4.1", tray.Selected().primary)
	}
}

// TestTrayListCurrentChoiceStartsSelectedAndVisible pins the distinction between the live
// value and the cursor: a current row is where a newly opened tray starts, and the sliding
// window must follow that cursor even when the row is below the first screenful.
func TestTrayListCurrentChoiceStartsSelectedAndVisible(t *testing.T) {
	t.Parallel()

	rows := []completionTrayRow{
		{primary: "one"},
		{primary: "two"},
		{primary: "three"},
		{primary: "four"},
		{primary: "five", current: true},
	}
	tray := newTrayList(rows, 40, trayLayout{})

	if got := tray.Selected().primary; got != "five" {
		t.Fatalf("initial selection = %q, want current choice five", got)
	}
	if view := stripANSI(tray.ViewWindow(40, 2)); !strings.Contains(view, "five") {
		t.Errorf("initial two-row window = %q, want current choice five visible", view)
	}
}

// TestTrayListCurrentChoiceStaysBlueAfterCursorMoves proves current and selected are
// independent. The selected row owns the full-width band; after it moves away, the active
// row keeps a blue primary so the user can still tell which value is in force.
func TestTrayListCurrentChoiceStaysBlueAfterCursorMoves(t *testing.T) {
	t.Parallel()

	tray := newTrayList([]completionTrayRow{
		{primary: "one"},
		{primary: "two", current: true},
		{primary: "three"},
	}, 40, trayLayout{})
	tray.Down()

	lines := strings.Split(tray.View(), "\n")
	if got := tray.Selected().primary; got != "three" {
		t.Fatalf("selection after Down = %q, want three", got)
	}
	if want := styles.CurrentChoiceStyle.Render("two"); !strings.Contains(lines[1], want) {
		t.Errorf("current unselected row = %q, want blue primary %q", lines[1], want)
	}
	if band := selectedBandOpen(t); !strings.HasPrefix(lines[2], band) {
		t.Errorf("selected row = %q, want selection band", lines[2])
	}
}

// TestTrayListCurrentChoiceSkipsGroupChrome ensures a grouped model tray can start on a
// current model without ever treating its provider heading as the active selectable row.
func TestTrayListCurrentChoiceSkipsGroupChrome(t *testing.T) {
	t.Parallel()

	tray := newTrayList([]completionTrayRow{
		{primary: "OPENAI", kind: trayRowHeading},
		{primary: "GPT-5.4"},
		{primary: "GPT-5.6", current: true},
	}, 40, trayLayout{})
	if got := tray.Selected().primary; got != "GPT-5.6" {
		t.Fatalf("initial grouped selection = %q, want current model GPT-5.6", got)
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
	tray.Filter("c")

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
	tray.Filter("conversation")
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

// trayListWindowRows is the five-row fixture the window tests slide over. Distinct primaries
// so a row can be identified in a stripped render, and no secondaries so the assertions are
// about the WINDOW rather than about column alignment.
var trayListWindowRows = []completionTrayRow{
	{primary: "one"}, {primary: "two"}, {primary: "three"}, {primary: "four"}, {primary: "five"},
}

// trayWindowText is the tray's visible rows, stripped of ANSI and of the rail, one per line.
func trayWindowText(t *testing.T, view string) []string {
	t.Helper()
	if view == "" {
		return nil
	}
	lines := strings.Split(view, "\n")
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = strings.TrimSpace(strings.TrimPrefix(stripANSI(line), styles.AccentBar))
	}
	return out
}

// TestTrayListWindowSlidesRatherThanPaging is the behaviour the whole consolidation exists
// for. list.Model PAGINATES: at maxRows 2 over five items it treats rows 4-5 as a page of
// their own, so a cursor on item 2 shows "three, four" and a cursor on the last item shows
// "five" ALONE -- a tray that shrinks under the cursor. A tray SLIDES: the window follows the
// cursor by one row and stays full.
func TestTrayListWindowSlidesRatherThanPaging(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		cursor int
		want   []string
	}{
		{cursor: 0, want: []string{"one", "two"}},
		{cursor: 1, want: []string{"one", "two"}},
		{cursor: 2, want: []string{"two", "three"}},
		{cursor: 3, want: []string{"three", "four"}},
		{cursor: 4, want: []string{"four", "five"}},
	} {
		tray := newTrayList(trayListWindowRows, 40, trayLayout{})
		for range tc.cursor {
			tray.Down()
		}
		got := trayWindowText(t, tray.ViewWindow(40, 2))
		if len(got) != len(tc.want) || got[0] != tc.want[0] || got[1] != tc.want[1] {
			t.Errorf("cursor %d window = %q, want %q", tc.cursor, got, tc.want)
		}
	}
}

// trayListStackedRows is the five-record fixture for the stacked layout: a title row and a
// metadata row per entry, both non-blank, so a blank row in a render can only be a pad.
var trayListStackedRows = []completionTrayRow{
	{primary: "T0", secondary: "M0"},
	{primary: "T1", secondary: "M1"},
	{primary: "T2", secondary: "M2"},
	{primary: "T3", secondary: "M3"},
	{primary: "T4", secondary: "M4"},
}

// TestTrayListWindowNeverUnderFills is the guard the paginating window could not give. While
// entries remain outside the window, it must hold every entry it was budgeted for. A short
// final page under-fills the window AND makes the tray shrink as the cursor reaches the end,
// which is the concrete /resume bug: five records with eleven rows free rendered ONE.
func TestTrayListWindowNeverUnderFills(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		layout trayLayout
		rows   []completionTrayRow
	}{
		{layout: trayLayout{}, rows: trayListWindowRows},
		{layout: trayLayout{Stacked: true, PadV: 1}, rows: trayListStackedRows},
	} {
		for maxRows := 1; maxRows <= 20; maxRows++ {
			want := tc.layout.visibleEntries(len(tc.rows), maxRows)
			for cursor := range tc.rows {
				tray := newTrayList(tc.rows, 40, tc.layout)
				for range cursor {
					tray.Down()
				}
				got := trayWindowText(t, tray.ViewWindow(40, maxRows))
				if len(got) > maxRows {
					t.Fatalf("%+v maxRows %d cursor %d: rendered %d rows, over budget", tc.layout, maxRows, cursor, len(got))
				}
				// An entry is drawn iff its FIRST row is; the pads between them are blank.
				entries := 0
				for i := 0; i < len(got); i += tc.layout.rowsPerEntry() {
					if got[i] != "" {
						entries++
					}
				}
				if entries != want {
					t.Errorf("%+v maxRows %d cursor %d: drew %d entries, want %d (the window under-filled)",
						tc.layout, maxRows, cursor, entries, want)
				}
			}
		}
	}
}

// TestTrayListStackedWindowNeverSplitsAnEntry pins the trap in sliding a STACKED tray. The
// window is chosen in ENTRIES and only then converted to rows; an implementation that slid by
// slicing RENDERED LINES would be right for the one-row-per-item trays and would cut a session
// record in half here -- a title with no date beneath it, or a date with no title above it.
func TestTrayListStackedWindowNeverSplitsAnEntry(t *testing.T) {
	t.Parallel()

	layout := trayLayout{Stacked: true, PadV: 1}
	for maxRows := 2; maxRows <= 20; maxRows++ {
		for cursor := range trayListStackedRows {
			tray := newTrayList(trayListStackedRows, 40, layout)
			for range cursor {
				tray.Down()
			}
			got := trayWindowText(t, tray.ViewWindow(40, maxRows))
			if len(got) == 0 {
				continue
			}
			// n whole entries are 3n-1 rows: title, metadata, pad -- with no pad after the
			// last. Anything else is a split entry or a trailing pad.
			if len(got)%3 != 2 {
				t.Fatalf("maxRows %d cursor %d: window is %d rows, want a whole number of entries (2, 5, 8, 11...):\n%q",
					maxRows, cursor, len(got), got)
			}
			for i := 0; i < len(got); i += 3 {
				title, meta := got[i], got[i+1]
				if !strings.HasPrefix(title, "T") {
					t.Fatalf("maxRows %d cursor %d: row %d = %q, want a title row", maxRows, cursor, i, title)
				}
				if meta != "M"+strings.TrimPrefix(title, "T") {
					t.Fatalf("maxRows %d cursor %d: %q is followed by %q, want its own metadata row",
						maxRows, cursor, title, meta)
				}
				if i+2 < len(got) && got[i+2] != "" {
					t.Fatalf("maxRows %d cursor %d: row %d = %q, want the pad between entries", maxRows, cursor, i+2, got[i+2])
				}
			}
		}
	}
}

// TestTrayListWindowIsStableAtItsOwnHeight pins the property the frame layout depends on.
// internal/presentation measures the tray by rendering it at the row BUDGET, then draws it
// again at that measured height. With a paginating window those two renders disagreed: a
// short final page measured shorter than the budget, and re-rendering at the shorter height
// picked a different, shorter page again -- so the layout reserved rows the view never drew
// and every region below the tray shifted. A sliding window is a fixed point, because it
// always holds the most entries that fit.
func TestTrayListWindowIsStableAtItsOwnHeight(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		layout trayLayout
		rows   []completionTrayRow
	}{
		{layout: trayLayout{}, rows: trayListWindowRows},
		{layout: trayLayout{Stacked: true, PadV: 1}, rows: trayListStackedRows},
	} {
		for budget := 1; budget <= 20; budget++ {
			for cursor := range tc.rows {
				tray := newTrayList(tc.rows, 40, tc.layout)
				for range cursor {
					tray.Down()
				}
				atBudget := tray.ViewWindow(40, budget)
				if atBudget == "" {
					continue
				}
				height := len(strings.Split(atBudget, "\n"))
				if atHeight := tray.ViewWindow(40, height); atHeight != atBudget {
					t.Errorf("%+v budget %d cursor %d: re-rendering at the measured height %d changed the tray\n at budget %q\n at height %q",
						tc.layout, budget, cursor, height, stripANSI(atBudget), stripANSI(atHeight))
				}
			}
		}
	}
}

// TestTrayListSelectWindowRowPinsTheWindow pins the hover behaviour the engine now owns. A
// window the pointer is aimed into must not scroll: re-deriving it from the new cursor would
// slide a different row under a motionless pointer, so the row the user aimed at is not the
// row they land on. A keyboard move releases the pin, because that IS the user asking the
// tray to follow the cursor again.
func TestTrayListSelectWindowRowPinsTheWindow(t *testing.T) {
	t.Parallel()

	tray := newTrayList(trayListWindowRows, 40, trayLayout{})
	tray.Up() // wrap to "five": the window is scrolled to the bottom
	if got, want := trayWindowText(t, tray.ViewWindow(40, 2)), []string{"four", "five"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("fixture window = %q, want %q", got, want)
	}

	if !tray.SelectWindowRow(0, 2) {
		t.Fatal("SelectWindowRow(top row of the scrolled window) reported no change, want a move")
	}
	if got := tray.Selected().primary; got != "four" {
		t.Errorf("selected = %q, want %q: the window slides, it does not page", got, "four")
	}
	if got, want := trayWindowText(t, tray.ViewWindow(40, 2)), []string{"four", "five"}; got[0] != want[0] || got[1] != want[1] {
		t.Errorf("window after pointing at its top row = %q, want it pinned at %q", got, want)
	}

	// A keyboard move releases the pin, so the window follows the cursor again: it slides up
	// to sit the cursor on its LAST row. Were the pin still held, "four, five" would stay.
	tray.Up() // from "four" to "three"
	if got, want := trayWindowText(t, tray.ViewWindow(40, 2)), []string{"two", "three"}; got[0] != want[0] || got[1] != want[1] {
		t.Errorf("window after a keyboard move = %q, want the pin released and %q", got, want)
	}
}

// TestTrayListSelectMovesToAnAbsoluteIndex pins the primitive whose absence made every panel
// walk the cursor with counted Up/Down loops. The index is in the FILTERED space -- the space
// Cursor reports -- and an index outside it is ignored rather than clamped onto a neighbour.
func TestTrayListSelectMovesToAnAbsoluteIndex(t *testing.T) {
	t.Parallel()

	tray := newTrayList(trayListWindowRows, 40, trayLayout{})
	if !tray.Select(3) || tray.Selected().primary != "four" {
		t.Fatalf("Select(3) selected %q, want four", tray.Selected().primary)
	}
	if tray.Select(3) {
		t.Error("Select of the current index reported a move, want none")
	}
	for _, out := range []int{-1, len(trayListWindowRows), 99} {
		if tray.Select(out) {
			t.Errorf("Select(%d) reported a move, want an out-of-range index ignored", out)
		}
		if got := tray.Selected().primary; got != "four" {
			t.Errorf("Select(%d) moved the cursor to %q, want it left on four", out, got)
		}
	}

	// A move RELEASES a pinned window, so the tray follows the cursor again. Without this,
	// a panel that moved the cursor by any route other than a pointer would leave the window
	// frozen where the pointer last left it.
	pinned := newTrayList(trayListWindowRows, 40, trayLayout{})
	pinned.Up() // wrap to "five": the window is scrolled to the bottom, showing three..five
	if !pinned.SelectWindowRow(0, 3) || pinned.Selected().primary != "three" {
		t.Fatalf("SelectWindowRow(top row) selected %q, want three", pinned.Selected().primary)
	}
	if !pinned.Select(3) {
		t.Fatal("Select(3) reported no move, want the cursor on four")
	}
	// The cursor is still INSIDE the pinned window, so a pin that survived Select would hold
	// the window at three..five. A released pin slides it to sit the cursor on the last row.
	if got, want := trayWindowText(t, pinned.ViewWindow(40, 3)), []string{"two", "three", "four"}; got[0] != want[0] || got[2] != want[2] {
		t.Errorf("window after Select = %q, want %q: Select must release the pin", got, want)
	}

	// The filtered space is the one that counts: "c" leaves /clear and /compact, so index 1
	// is /compact and index 2 is out of range even though the catalog has three commands.
	filtered := newTrayList(trayListRows, 40, trayLayout{})
	filtered.Filter("c")
	if !filtered.Select(1) || filtered.Selected().primary != "/compact" {
		t.Errorf("Select(1) under a filter selected %q, want /compact", filtered.Selected().primary)
	}
	if filtered.Select(2) {
		t.Error("Select(2) moved past the end of the FILTERED items, want it ignored")
	}
}
