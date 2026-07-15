package components

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
	items  []FileItem
	cursor int
}

// NewFileComplete returns a completer over items, or nil when empty (nil = hidden).
func NewFileComplete(items []FileItem) *FileComplete {
	if len(items) == 0 {
		return nil
	}
	return &FileComplete{items: items, cursor: 0}
}

// Selected returns the item under the cursor.
func (f *FileComplete) Selected() FileItem { return f.items[f.cursor] }

// Up moves the cursor up, wrapping to the bottom.
func (f *FileComplete) Up() { f.cursor = (f.cursor - 1 + len(f.items)) % len(f.items) }

// Down moves the cursor down, wrapping to the top.
func (f *FileComplete) Down() { f.cursor = (f.cursor + 1) % len(f.items) }

// label is the complete displayed @path, plus a trailing "/" for a directory so a
// folder reads as drillable at a glance.
func (i FileItem) label() string {
	name := "@" + i.Path
	if i.IsDir {
		return name + "/"
	}
	return name
}

func (f *FileComplete) trayRows() []completionTrayRow {
	rows := make([]completionTrayRow, len(f.items))
	for i, item := range f.items {
		rows[i] = completionTrayRow{primary: item.label()}
	}
	return rows
}

// View renders the filtered list at its natural content width.
func (f *FileComplete) View() string {
	rows := f.trayRows()
	return renderCompletionTray(rows, f.cursor, completionTrayNaturalWidth(rows))
}

// ViewWidth renders the filtered list as a tray whose rows are padded or clamped
// ANSI-safely to width display columns.
func (f *FileComplete) ViewWidth(width int) string {
	return renderCompletionTray(f.trayRows(), f.cursor, width)
}
