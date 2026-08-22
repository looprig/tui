package components

import (
	"strings"
	"unicode"
)

// FileItem is one @path completion candidate. Path is the value to complete to (e.g.
// "src" or "src/main.go"); IsDir drives the trailing "/" affordance and whether
// selecting it keeps the panel open to drill in.
type FileItem struct {
	Path  string
	IsDir bool
}

// FileComplete is a filtered file list with a wrapping cursor — the @path completion
// panel, the disk-backed sibling of SlashComplete. It is display-only: package tui
// computes the candidate list (the filesystem read) and feeds it here.
type FileComplete struct {
	// items is kept beside the engine because Selected hands back the ORIGINAL FileItem:
	// the engine only carries the sanitized label, while completion needs the exact bytes
	// that came off disk. This panel never filters, so an engine index is always an items
	// index.
	items []FileItem
	// list owns the cursor, its wrap, and how a row is drawn.
	list *trayList
	// windowStart and windowPinned are the sliding window, which the engine cannot
	// supply: list.Model paginates, jumping a whole page once the cursor leaves it, while
	// every completion panel slides, pinning the selection to the window edge and
	// scrolling a row at a time. The pin then holds that window still across a mouse pick,
	// so clicking the top row cannot scroll the list out from under the pointer.
	windowStart  int
	windowPinned bool
}

// NewFileComplete returns a completer over items, or nil when empty (nil = hidden).
func NewFileComplete(items []FileItem) *FileComplete {
	if len(items) == 0 {
		return nil
	}
	f := &FileComplete{items: items}
	rows := f.trayRows()
	f.list = newTrayList(rows, completionTrayNaturalWidth(rows), trayLayout{})
	return f
}

// Selected returns the item under the cursor.
func (f *FileComplete) Selected() FileItem { return f.items[f.list.Cursor()] }

// Cursor returns the absolute selected index in the filtered list.
func (f *FileComplete) Cursor() int { return f.list.Cursor() }

// Up moves the cursor up, wrapping to the bottom.
func (f *FileComplete) Up() {
	f.list.Up()
	f.windowPinned = false
}

// Down moves the cursor down, wrapping to the top.
func (f *FileComplete) Down() {
	f.list.Down()
	f.windowPinned = false
}

// SelectWindowRow moves the cursor to a row in the currently rendered maxRows window.
// It returns whether the selection changed; rows outside the visible window are ignored.
func (f *FileComplete) SelectWindowRow(row, maxRows int) bool {
	visible := min(len(f.items), maxRows)
	if row < 0 || row >= visible {
		return false
	}
	start := f.visibleWindowStart(maxRows)
	next := start + row
	if next == f.list.Cursor() {
		return false
	}
	f.selectIndex(next)
	f.windowStart = start
	f.windowPinned = true
	return true
}

// selectIndex walks the engine's cursor to an absolute index. trayList moves only
// relatively, because the panels drive it from a keyboard; a pointed-at row is always
// inside the visible window, so the walk is one screenful at worst. Both loops are counted
// ranges rather than conditions, so an out-of-range index can never spin.
func (f *FileComplete) selectIndex(index int) {
	for range index - f.list.Cursor() {
		f.list.Down()
	}
	for range f.list.Cursor() - index {
		f.list.Up()
	}
}

func (f *FileComplete) visibleWindowStart(maxRows int) int {
	visible := min(len(f.items), maxRows)
	cursor := f.list.Cursor()
	if f.windowPinned && visible > 0 {
		start := max(0, min(f.windowStart, len(f.items)-visible))
		if cursor >= start && cursor < start+visible {
			return start
		}
	}
	return completionTrayWindowStart(len(f.items), cursor, maxRows)
}

// label is the complete displayed @path, plus a trailing "/" for a directory so a
// folder reads as drillable at a glance.
func (i FileItem) label() string {
	name := "@" + sanitizeFilePath(i.Path)
	if i.IsDir {
		return name + "/"
	}
	return name
}

// sanitizeFilePath makes an arbitrary filesystem name safe to paint in a terminal.
// It is deliberately display-only: FileItem.Path remains byte-for-byte unchanged for
// selection and completion. Replacing every Unicode control rune also neutralizes the
// ESC, C1 CSI/OSC, and terminator bytes used by terminal control sequences.
func sanitizeFilePath(path string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '\uFFFD'
		}
		return r
	}, path)
}

func (f *FileComplete) trayRows() []completionTrayRow {
	rows := make([]completionTrayRow, len(f.items))
	for i, item := range f.items {
		rows[i] = completionTrayRow{primary: item.label()}
	}
	return rows
}

// View renders the filtered list at its natural content width, which is the width the
// engine was built with: the rows never change, so neither does it.
func (f *FileComplete) View() string { return f.list.View() }

// ViewWidth renders the filtered list as a tray whose rows are padded or clamped
// ANSI-safely to width display columns.
func (f *FileComplete) ViewWidth(width int) string { return f.list.ViewWidth(width) }

// ViewWindow renders a full-width tray capped to maxRows and keeps the selected path in the
// visible window. View and ViewWidth remain the unbounded variants.
//
// It deliberately does NOT call trayList.ViewWindow, which shows the cursor's PAGE. This
// window slides, so its rows seldom line up with a page boundary. The slice is handed to a
// throwaway engine instead, which draws it through the same delegate and so emits the same
// bytes an unbounded tray of those rows would.
func (f *FileComplete) ViewWindow(width, maxRows int) string {
	if maxRows <= 0 {
		return ""
	}
	if maxRows >= len(f.items) {
		return f.list.ViewWidth(width)
	}
	start := f.visibleWindowStart(maxRows)
	window := newTrayList(f.trayRows()[start:start+maxRows], width, trayLayout{})
	for range f.list.Cursor() - start {
		window.Down()
	}
	return window.ViewWidth(width)
}
