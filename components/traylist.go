package components

import (
	"image/color"
	"io"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/looprig/tui/styles"
)

// trayLayout configures how a tray lays out its rows. The zero value is today's
// one-row-per-item tray, so existing callers are unaffected by construction.
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

// rowsPerEntry is one item's full vertical footprint: its content rows plus its pad. It is
// the unit list.Model paginates in, which is why the pad is folded into the ITEM rather than
// declared as the delegate's Spacing: list.Model separates items with plain newlines, and a
// tray's spacer has to carry the rail so the left edge runs unbroken down the panel.
func (l trayLayout) rowsPerEntry() int { return l.rowsPerItem() + max(0, l.PadV) }

// viewRows is how many VISUAL rows entries entries occupy: their content rows plus one pad
// between each pair. The LAST entry carries no pad, which is why this is not simply
// entries*rowsPerEntry.
func (l trayLayout) viewRows(entries int) int {
	if entries <= 0 {
		return 0
	}
	return entries*l.rowsPerItem() + (entries-1)*max(0, l.PadV)
}

// height is the list height that makes list.Model page the way the tray wants. list.Model
// derives PerPage from height divided by one entry's footprint, so the height is always a
// whole number of entries. maxRows caps it to the visual rows the surface can spare; zero or
// less means "as many as the items need".
//
// The +PadV before the divide is the mirror of viewRows dropping the last pad: it is what
// lets a window fit one more entry than a naive division would allow.
func (l trayLayout) height(items, maxRows int) int {
	visible := items
	if maxRows > 0 {
		visible = min(visible, (maxRows+max(0, l.PadV))/l.rowsPerEntry())
	}
	return max(1, visible) * l.rowsPerEntry()
}

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

// trayDelegate draws one item. list.DefaultDelegate is deliberately not used: it renders
// nothing at all unless items implement list.DefaultItem, and its two-line title/description
// shape is not the tray's.
type trayDelegate struct{ layout trayLayout }

// Height is the item's declared footprint INCLUDING its pad, since the pad is drawn as part
// of the item (see rowsPerEntry). The last item on a page draws one pad fewer than it
// declares, so a page can come up short -- never long -- and trayList trims the difference.
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

	items := m.VisibleItems()
	start, end := m.Paginator.GetSliceBounds(len(items))
	render := newTrayRowRender(trayPageRows(items[start:end], d.layout), m.Width(), d.layout.PadH)
	selected := index == m.Index()

	primary := it.primary
	if matches := m.MatchesForItem(index); len(matches) > 0 {
		primary = lipgloss.StyleRunes(primary, matches, trayMatchStyle, lipgloss.NewStyle())
	}

	rows := make([]string, 0, d.layout.rowsPerEntry())
	if d.layout.Stacked {
		rows = append(rows, render.row(primary, selected))
		rows = append(rows, render.row(styles.CardHintStyle.Render(it.secondary), selected))
	} else {
		rows = append(rows, render.line(completionTrayRow{primary: primary, secondary: it.secondary}, selected))
	}
	// The pad belongs to the item, and is never banded: a spacer is not part of the
	// selection, it is the gap beside it.
	if index < end-1 {
		for range max(0, d.layout.PadV) {
			rows = append(rows, render.row("", false))
		}
	}
	_, _ = io.WriteString(w, strings.Join(rows, "\n"))
}

// trayPageRows is the page's items as tray rows, for computing the shared description
// column. A stacked tray has no second column -- the secondary is on its own row -- so the
// secondaries are dropped and the column comes out zero.
func trayPageRows(items []list.Item, layout trayLayout) []completionTrayRow {
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
// the runtime model list and /resume sessions. It owns cursor, paging and filter state;
// trayDelegate owns how a row looks.
type trayList struct {
	m      list.Model
	layout trayLayout
}

func newTrayList(rows []completionTrayRow, width int, layout trayLayout) *trayList {
	items := make([]list.Item, len(rows))
	for i, row := range rows {
		items[i] = trayItem(row)
	}

	l := list.New(items, trayDelegate{layout: layout}, width, layout.height(len(items), 0))
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
	// every hand-rolled panel already has.
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

// Cursor is the absolute selected index within the FILTERED items, matching what the
// hand-rolled panels return.
func (t *trayList) Cursor() int { return t.m.Index() }

// Up moves the cursor up, wrapping to the bottom.
func (t *trayList) Up() { t.m.CursorUp() }

// Down moves the cursor down, wrapping to the top.
func (t *trayList) Down() { t.m.CursorDown() }

// SelectWindowRow moves the cursor to a VISUAL row of the currently rendered maxRows window
// and reports whether the selection changed. Rows outside the window, and the pad rows
// between entries, are inert: a spacer belongs to no item, so clicking one must not move the
// cursor to a neighbour the user did not point at.
func (t *trayList) SelectWindowRow(row, maxRows int) bool {
	items := len(t.m.VisibleItems())
	if row < 0 || maxRows <= 0 || items == 0 {
		return false
	}
	if row%t.layout.rowsPerEntry() >= t.layout.rowsPerItem() {
		return false
	}
	t.m.SetHeight(t.layout.height(items, maxRows))
	start, end := t.m.Paginator.GetSliceBounds(items)
	next := start + row/t.layout.rowsPerEntry()
	if next >= end || next == t.m.Index() {
		return false
	}
	t.m.Select(next)
	return true
}

// View renders every row at the width the tray was built with.
func (t *trayList) View() string { return t.render(t.m.Width(), 0) }

// ViewWidth renders every row at width columns.
func (t *trayList) ViewWidth(width int) string { return t.render(width, 0) }

// ViewWindow renders at most maxRows visual rows, keeping the cursor's entry on screen. A
// maxRows of zero or less renders NOTHING rather than everything, matching the hand-rolled
// panels: a surface with no rows to spare is asking for no tray, not an unbounded one.
func (t *trayList) ViewWindow(width, maxRows int) string {
	if maxRows <= 0 {
		return ""
	}
	return t.render(width, maxRows)
}

// ViewWindowBackground renders the bounded tray. selectedBg is IGNORED: styles.SelectedRow
// is one-argument and owns the selection fill so no surface can drift to its own shade. The
// parameter survives only so this engine can be dropped into the four panels without
// touching their call sites.
func (t *trayList) ViewWindowBackground(width, maxRows int, selectedBg color.Color) string {
	return t.ViewWindow(width, maxRows)
}

// render draws the tray at width columns, showing at most maxRows visual rows (zero or less
// means "as many as the items need").
//
// It works on a COPY of the model: a render must not move the cursor, and sizing the list is
// how list.Model is told which page to show.
func (t *trayList) render(width, maxRows int) string {
	items := len(t.m.VisibleItems())
	if width <= 0 || items == 0 {
		return ""
	}
	if maxRows > 0 && maxRows < t.layout.rowsPerItem() {
		return "" // not even one entry's content fits
	}

	m := t.m
	m.SetWidth(width)
	m.SetHeight(t.layout.height(items, maxRows))

	// list.Model pads its view out to the height it was given, with blank rows that carry
	// no rail. The height is a whole number of ENTRIES while the last entry on a page draws
	// no pad, so the tail is exactly PadV such rows. They are cut by counting the rows the
	// page owes rather than by matching on what they look like: lipgloss pads with runs of
	// spaces, not with empty lines, so a trailing-whitespace trim would be guessing.
	lines := strings.Split(m.View(), "\n")
	if want := t.layout.viewRows(m.Paginator.ItemsOnPage(items)); want > 0 && want < len(lines) {
		lines = lines[:want]
	}
	return strings.Join(lines, "\n")
}
