package components

import (
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/looprig/tui/styles"
)

// trayLayout configures how a tray lays out its rows. The zero value is the one-row-per-item
// tray the slash, @path and model pickers use; the session picker sets Stacked and PadV.
type trayLayout struct {
	// Stacked renders Secondary on its OWN row beneath Primary instead of inline in a
	// second column.
	Stacked bool
	// PadV is blank rail-carrying rows inserted BELOW each item, separating entries in a
	// stacked tray. "Below each item" means BETWEEN items: the pad under the LAST item is
	// omitted, because it would only push the tray off the composer, and because the
	// session tray this models has always spaced between records rather than after them.
	PadV int
	// PadH is columns of padding inside the rail, left and right.
	PadH int
}

// rowsPerItem is how many CONTENT rows one item occupies, before its pad.
func (l trayLayout) rowsPerItem() int {
	if l.Stacked {
		return 2
	}
	return 1
}

// rowsPerEntry is one item's full vertical footprint: its content rows plus its pad. The pad
// is folded into the ITEM rather than declared as the delegate's Spacing because list.Model
// separates items with plain newlines, and a tray's spacer has to carry the rail so the left
// edge runs unbroken down the panel.
func (l trayLayout) rowsPerEntry() int { return l.rowsPerItem() + max(0, l.PadV) }

// visibleEntries is how many whole ENTRIES fit in maxRows visual rows. A maxRows of zero or
// less means "as many as there are".
//
// n entries occupy n*rowsPerItem + (n-1)*PadV visual rows -- one pad BETWEEN each pair, none
// after the last -- so the exact fit is (maxRows+PadV)/rowsPerEntry. The +PadV before the
// divide is that missing final pad, and it is what lets a window hold one more entry than a
// naive division would allow: at PadV 1 a stacked tray fits two records in five rows, where
// dividing by three would buy one and waste the rest.
func (l trayLayout) visibleEntries(items, maxRows int) int {
	if maxRows <= 0 {
		return items
	}
	return min(items, (maxRows+max(0, l.PadV))/l.rowsPerEntry())
}

// singlePageHeight is the list height that keeps list.Model on ONE page, whatever a filter
// later leaves behind. That is the whole of trayList's relationship with list.Model's
// pagination, and it is deliberate: on a single page Paginator.Page is always zero, so
// list.Model.Index is an ABSOLUTE index into the visible items rather than an offset into
// whichever page the cursor last landed on.
//
// The window the user actually sees is chosen by trayList.windowStart and drawn by
// trayList.render, because a tray SLIDES: it follows the cursor by one entry, where a
// paginator jumps a whole page and leaves a short last page under-filled — a tray that
// visibly shrinks under the cursor.
func (l trayLayout) singlePageHeight(items int) int { return max(1, items) * l.rowsPerEntry() }

// trayItem is one row's content in the list-backed tray. Its fields mirror
// completionTrayRow's exactly, so the two convert directly and a row cannot pick up a field
// the other lacks.
type trayItem struct{ primary, secondary string }

// FilterValue is the PRIMARY only, so list.MatchesForItem's rune indices map 1:1 onto the
// column trayDelegate underlines. Including the secondary would let a match land on a rune
// that column never draws, and every underline after it would slide.
func (t trayItem) FilterValue() string { return t.primary }

// trayMatchStyle marks the runes the active filter matched. Underline rather than a color:
// the tray already spends color on the rail and the selection band, and an underline
// survives on any terminal palette.
//
// It is lost on the SELECTED row, because styles.SelectedRow strips inner styling on its
// light fill. That is the same trade the bold accelerator makes, and it costs nothing the
// user needs: the selected row is already unambiguous.
var trayMatchStyle = lipgloss.NewStyle().Underline(true)

// trayFrame is the per-render context list.Model cannot supply, because it would answer from
// the current PAGE and the tray slides. trayList picks its own window and hands it here.
type trayFrame struct {
	// render composes a physical row — width, description column, inset, panel fill —
	// derived once per render rather than once per row, per the note on
	// styles.FillLineBackgroundWith.
	render trayRowRender
	// last is the absolute index of the LAST entry in the window: the one entry that draws
	// no pad, since a pad separates entries rather than trailing them.
	last int
}

// trayDelegate draws one item. list.DefaultDelegate is deliberately not used: it renders
// nothing at all unless items implement list.DefaultItem, and its two-line title/description
// shape is not the tray's.
//
// Render is driven by trayList.render, one entry at a time, and NOT by list.Model.View —
// which the tray never calls, because it would draw the cursor's page rather than the
// cursor's window. list.Model still needs a delegate for its Height, which is what sizes the
// pagination singlePageHeight collapses.
type trayDelegate struct {
	layout trayLayout
	frame  trayFrame
}

// Height is the item's declared footprint INCLUDING its pad, since the pad is drawn as part
// of the item (see rowsPerEntry).
func (d trayDelegate) Height() int { return d.layout.rowsPerEntry() }

// Spacing is zero: the gap between entries is drawn by Render as rail-carrying rows, not
// left to list.Model, which would separate them with bare newlines.
func (d trayDelegate) Spacing() int { return 0 }

// Update does nothing. The tray's keys are owned by the surface that hosts it, which calls
// Up, Down and SelectWindowRow directly; there is no item-level state to advance.
func (d trayDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d trayDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(trayItem)
	if !ok {
		return // an item this delegate did not put in the list draws nothing
	}

	selected := index == m.Index()

	primary := it.primary
	if matches := m.MatchesForItem(index); len(matches) > 0 {
		primary = lipgloss.StyleRunes(primary, matches, trayMatchStyle, lipgloss.NewStyle())
	}

	rows := make([]string, 0, d.layout.rowsPerEntry())
	if d.layout.Stacked {
		rows = append(rows, d.frame.render.row(primary, selected))
		rows = append(rows, d.frame.render.row(styles.CardHintStyle.Render(it.secondary), selected))
	} else {
		rows = append(rows, d.frame.render.line(completionTrayRow{primary: primary, secondary: it.secondary}, selected))
	}
	// The pad belongs to the item, and is never banded: a spacer is not part of the
	// selection, it is the gap beside it.
	//
	// The LAST entry of the window draws no pad, and this guard is the ONLY thing enforcing
	// that. Nothing downstream trims it: render composes exactly the rows it wants and never
	// goes through list.Model.View, so there is no height filler to cut afterwards. Delete
	// this and the tray trails a blank row onto the top of the composer.
	if index < d.frame.last {
		for range max(0, d.layout.PadV) {
			rows = append(rows, d.frame.render.row("", false))
		}
	}
	_, _ = io.WriteString(w, strings.Join(rows, "\n"))
}

// trayWindowRows is the window's items as tray rows, for computing the shared description
// column. A stacked tray has no second column -- the secondary is on its own row -- so the
// secondaries are dropped and the column comes out zero.
func trayWindowRows(items []list.Item, layout trayLayout) []completionTrayRow {
	rows := make([]completionTrayRow, 0, len(items))
	for _, item := range items {
		it, ok := item.(trayItem)
		if !ok {
			continue
		}
		row := completionTrayRow(it)
		if layout.Stacked {
			row.secondary = ""
		}
		rows = append(rows, row)
	}
	return rows
}

// trayList is the list-backed engine behind the completion trays: slash commands, @paths,
// the runtime model list and /resume sessions. It owns cursor, filter and WINDOW state;
// trayDelegate owns how a row looks.
type trayList struct {
	m      list.Model
	layout trayLayout
	// pinnedStart is the window a pointer last landed in, and pinned says whether that pin
	// is live. Without it, pointing at the top row of a scrolled tray would select an item,
	// re-derive the window from the new cursor, and slide the list out from under a
	// motionless pointer — so the row the user aimed at is not the row they end up on.
	pinnedStart int
	pinned      bool
}

func newTrayList(rows []completionTrayRow, width int, layout trayLayout) *trayList {
	items := make([]list.Item, len(rows))
	for i, row := range rows {
		items[i] = trayItem(row)
	}

	l := list.New(items, trayDelegate{layout: layout}, width, layout.singlePageHeight(len(items)))
	// Every piece of list chrome is either something the composer and status line already
	// own or noise in a popup that sits on top of the transcript. The tray is the rows.
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	// The host surface owns the keyboard; a tray must never be the thing that quits.
	l.DisableQuitKeybindings()
	// list.Model clamps at the ends by default. The trays wrap, matching the modulo cursor
	// every hand-rolled panel already had.
	l.InfiniteScrolling = true
	return &trayList{m: l, layout: layout}
}

// Selected is the item under the cursor, or the zero trayItem when the list is empty. It is
// fail-safe rather than panicking, because a tray can be filtered down to nothing between a
// keypress and the render that follows it.
func (t *trayList) Selected() trayItem {
	it, _ := t.m.SelectedItem().(trayItem)
	return it
}

// Cursor is the absolute selected index within the FILTERED items.
func (t *trayList) Cursor() int { return t.m.Index() }

// UnfilteredCursor is the selected item's index in the rows the tray was BUILT with, before
// any filter reordered or dropped rows. A panel carrying a payload beside the tray — an
// opaque model id, a session UUID — resolves it through this rather than through Cursor, so
// the payload survives however the matcher shuffled the rows.
func (t *trayList) UnfilteredCursor() int { return t.m.GlobalIndex() }

// Len is how many items survived the filter: the rows the tray actually draws.
func (t *trayList) Len() int { return len(t.m.VisibleItems()) }

// Rows is the visible items as tray rows, for a panel that has to measure what it is about
// to draw — a natural width, say — without reaching into the list.
func (t *trayList) Rows() []completionTrayRow { return trayWindowRows(t.m.VisibleItems(), t.layout) }

// SetWidth sets the width the width-less View renders at. It exists for the panels that can
// only measure their natural width AFTER a filter has decided which rows survive.
func (t *trayList) SetWidth(width int) { t.m.SetWidth(width) }

// SetFilterFunc replaces the match rule Filter applies. It is a method rather than a field
// because list.Model consults the rule on every filter pass; a panel wanting something other
// than plain fuzzy-over-the-primary (see valueFilter) installs it once, at build time, before
// any query is applied.
func (t *trayList) SetFilterFunc(f list.FilterFunc) { t.m.Filter = f }

// Filter narrows the tray to the items matching query and puts the cursor back on the first
// of them.
//
// A blank query is left UNFILTERED rather than filtered by "": list.Model treats an empty
// term as "everything matches" but still switches into its filtered projection, in which
// every row reports an unfiltered index of zero — and UnfilteredCursor would then hand back
// the first item whatever the cursor is on.
func (t *trayList) Filter(query string) {
	if strings.TrimSpace(query) == "" {
		return
	}
	t.m.SetFilterText(query)
	t.pinned = false
}

// Up moves the cursor up, wrapping to the bottom.
func (t *trayList) Up() {
	t.m.CursorUp()
	t.pinned = false
}

// Down moves the cursor down, wrapping to the top.
func (t *trayList) Down() {
	t.m.CursorDown()
	t.pinned = false
}

// Select moves the cursor to an absolute index and reports whether the cursor actually
// moved, matching SelectWindowRow's shape.
//
// The index is in the FILTERED space — the space Cursor reports and Len counts, NOT the
// UnfilteredCursor space a payload lookup uses. The two coincide only while no filter is
// applied, so the distinction is stated here rather than left for each caller to rediscover.
// An index outside that space is ignored rather than clamped: a caller asking for a row that
// is not there has a bug, and silently landing on a neighbour would hide it.
//
// A move releases any pinned window: a pin exists to hold the tray still under a pointer, and
// a cursor that has gone elsewhere is the signal that the window should follow it again.
func (t *trayList) Select(index int) bool {
	if index < 0 || index >= t.Len() || index == t.m.Index() {
		return false
	}
	t.m.Select(index)
	t.pinned = false
	return true
}

// SelectWindowRow moves the cursor to a VISUAL row of the currently rendered maxRows window
// and reports whether the selection changed. Rows outside the window, and the pad rows
// between entries, are inert: a spacer belongs to no item, so pointing at one must not move
// the cursor to a neighbour the user did not point at.
func (t *trayList) SelectWindowRow(row, maxRows int) bool {
	count := t.Len()
	if row < 0 || maxRows <= 0 || count == 0 {
		return false
	}
	if row%t.layout.rowsPerEntry() >= t.layout.rowsPerItem() {
		return false
	}
	visible := t.layout.visibleEntries(count, maxRows)
	start := t.windowStart(count, visible)
	if next := start + row/t.layout.rowsPerEntry(); next >= start+visible || !t.Select(next) {
		return false
	}
	// Select released the pin; this one is taken back deliberately. The window the pointer
	// just aimed into is precisely the window that must not move under it.
	t.pinnedStart, t.pinned = start, true
	return true
}

// View renders every row at the width the tray was built with.
func (t *trayList) View() string { return t.render(t.m.Width(), 0) }

// ViewWidth renders every row at width columns.
func (t *trayList) ViewWidth(width int) string { return t.render(width, 0) }

// ViewWindow renders at most maxRows visual rows, keeping the cursor's entry on screen. A
// maxRows of zero or less renders NOTHING rather than everything: a surface with no rows to
// spare is asking for no tray, not an unbounded one.
func (t *trayList) ViewWindow(width, maxRows int) string {
	if maxRows <= 0 {
		return ""
	}
	return t.render(width, maxRows)
}

// windowStart is the first entry of the visible window, SLIDING rather than paging: the
// window follows the cursor by the least it can, so an arrow key scrolls the tray by one
// entry instead of swapping a whole page out from under it.
//
// It never under-fills. The window always holds `visible` entries — the most that fit — so a
// cursor near the end of a long list keeps the entries above it on screen, rather than
// stranding the tray on a short final page that shrinks under the cursor.
//
// A window pinned by SelectWindowRow wins while the cursor is still inside it, so pointing at
// a row cannot scroll the tray out from under the pointer. The clamp on the pin is load
// bearing: the row budget can shrink between the pin and the render (a resize), and a pin
// taken against a taller window would otherwise start past the end. A pin the cursor has
// since left is ignored rather than cleared, because a render must not mutate the panel.
func (t *trayList) windowStart(count, visible int) int {
	if visible >= count {
		return 0
	}
	cursor := t.m.Index()
	if t.pinned {
		if start := min(max(0, t.pinnedStart), count-visible); cursor >= start && cursor < start+visible {
			return start
		}
	}
	return max(0, cursor-visible+1)
}

// render draws the tray at width columns, showing at most maxRows visual rows (zero or less
// means "as many as the items need").
//
// It does NOT go through list.Model.View. list.Model paginates; the tray slides, and a
// sliding window seldom lines up with a page boundary. So the window is chosen here and each
// entry in it is drawn through trayDelegate — one row-composing path, whatever the window
// turns out to be.
func (t *trayList) render(width, maxRows int) string {
	items := t.m.VisibleItems()
	if width <= 0 || len(items) == 0 {
		return ""
	}
	visible := t.layout.visibleEntries(len(items), maxRows)
	if visible <= 0 {
		return "" // not even one entry's content rows fit
	}
	start := t.windowStart(len(items), visible)
	window := items[start : start+visible]

	d := trayDelegate{
		layout: t.layout,
		frame: trayFrame{
			render: newTrayRowRender(trayWindowRows(window, t.layout), width, t.layout.PadH),
			last:   start + visible - 1,
		},
	}
	entries := make([]string, 0, visible)
	var b strings.Builder
	for i, item := range window {
		b.Reset()
		d.Render(&b, t.m, start+i, item)
		entries = append(entries, b.String())
	}
	return strings.Join(entries, "\n")
}
