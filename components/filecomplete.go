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
	// list owns the cursor, its wrap, the sliding window and how a row is drawn.
	list *trayList
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
func (f *FileComplete) Up() { f.list.Up() }

// Down moves the cursor down, wrapping to the top.
func (f *FileComplete) Down() { f.list.Down() }

// SelectWindowRow moves the cursor to a row in the currently rendered maxRows window.
// It returns whether the selection changed; rows outside the visible window are ignored.
func (f *FileComplete) SelectWindowRow(row, maxRows int) bool {
	return f.list.SelectWindowRow(row, maxRows)
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
func (f *FileComplete) ViewWindow(width, maxRows int) string {
	return f.list.ViewWindow(width, maxRows)
}
