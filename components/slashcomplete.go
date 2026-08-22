package components

import (
	"strings"
)

// SlashCmd is one slash command's display metadata. The action is dispatched by
// package tui keyed on Name; this widget only filters and displays.
type SlashCmd struct {
	Name string // e.g. "/clear"
	Desc string // e.g. "clear the conversation"
}

// SlashCommands is the canonical list (exported so package tui can map Name→action).
var SlashCommands = []SlashCmd{
	{"/clear", "start a new conversation"},
	{"/compact", "compact the current conversation"},
	{"/exit", "exit Looprig"},
}

// SlashComplete is a filtered command list with a wrapping cursor.
//
// The items, the cursor and the filter all live in the shared trayList engine, so this type
// is a translation layer: SlashCmd in, trayItem out, and back again.
//
// The two window fields are the one thing the engine cannot express. list.Model PAGINATES --
// a cursor stepping past the last visible row jumps a whole page -- while the completion
// trays SLIDE, scrolling a row at a time with the selection pinned to the window's edge. See
// visibleWindowStart.
type SlashComplete struct {
	list *trayList
	// windowStart and windowPinned freeze the window a pointer last hit. Without them,
	// pointing at the top row of a scrolled tray would select an item, re-derive the window
	// from the new cursor, and slide the list out from under the pointer -- so the row the
	// user aimed at is not the row that ends up under them.
	windowStart  int
	windowPinned bool
}

// NewSlashComplete returns a fuzzy, case-insensitive completer for prefix over the canonical
// catalog. Returns nil when nothing matches (nil = panel hidden).
func NewSlashComplete(prefix string) *SlashComplete {
	return NewSlashCompleteWithCommands(prefix, SlashCommands)
}

// NewSlashCompleteWithCommands builds a completer from an immutable caller-owned catalog.
// The commands are copied into the list engine, so a live tray cannot change underneath
// keyboard input.
//
// Matching is the engine's fuzzy filter over command NAMES: the query's runes need only
// appear in order, so "sbx" finds /sandbox and "ear" still finds /clear. Descriptions are
// deliberately NOT searched -- see trayItem.FilterValue -- so a word that reads well in a
// description can no longer drag an unrelated command into the tray. Case is folded by the
// filter itself, so the query goes through as typed.
//
// Returns nil when nothing matches, INCLUDING for an empty catalog: nil means "panel hidden",
// and a panel with no rows would be a tray that reserves height to draw nothing.
func NewSlashCompleteWithCommands(prefix string, commands []SlashCmd) *SlashComplete {
	rows := make([]completionTrayRow, len(commands))
	for i, command := range commands {
		rows[i] = completionTrayRow{primary: command.Name, secondary: command.Desc}
	}

	// The width is set below, once the matches are known; there is nothing to measure yet.
	list := newTrayList(rows, 0, trayLayout{})
	// The leading slash is optional in the query. The user has already typed it and every
	// name carries one, so matching it would only ever be a no-op that costs a rune.
	list.m.SetFilterText(strings.TrimPrefix(prefix, "/"))

	matched := trayPageRows(list.m.VisibleItems(), trayLayout{})
	if len(matched) == 0 {
		return nil
	}
	// The natural width is measured from the MATCHES rather than from the catalog: the tray
	// is only ever as wide as the rows it draws.
	list.m.SetWidth(completionTrayNaturalWidth(matched))
	return &SlashComplete{list: list}
}

// Selected returns the item under the cursor.
func (s *SlashComplete) Selected() SlashCmd {
	item := s.list.Selected()
	return SlashCmd{Name: item.primary, Desc: item.secondary}
}

// Cursor returns the absolute selected index in the filtered list.
func (s *SlashComplete) Cursor() int { return s.list.Cursor() }

// Up moves the cursor up, wrapping to the bottom.
func (s *SlashComplete) Up() {
	s.list.Up()
	s.windowPinned = false
}

// Down moves the cursor down, wrapping to the top.
func (s *SlashComplete) Down() {
	s.list.Down()
	s.windowPinned = false
}

// SelectWindowRow moves the cursor to a row in the currently rendered maxRows window.
// It returns whether the selection changed; rows outside the visible window are ignored.
func (s *SlashComplete) SelectWindowRow(row, maxRows int) bool {
	visible := min(s.matches(), maxRows)
	if row < 0 || row >= visible {
		return false
	}
	start := s.visibleWindowStart(maxRows)
	next := start + row
	if next == s.Cursor() {
		return false
	}
	s.list.m.Select(next)
	s.windowStart = start
	s.windowPinned = true
	return true
}

// matches is how many commands survived the filter: the rows the tray actually draws.
func (s *SlashComplete) matches() int { return len(s.list.m.VisibleItems()) }

// visibleWindowStart is the first row of the maxRows window, sliding rather than paging: the
// window follows the cursor by the least it can, which keeps the neighbouring commands on
// screen instead of swapping a whole page out from under an arrow key.
//
// A window pinned by SelectWindowRow wins while the cursor is still inside it, so pointing at
// a row does not scroll the tray. A pin that no longer contains the cursor is ignored rather
// than cleared, because a render must not mutate the panel.
func (s *SlashComplete) visibleWindowStart(maxRows int) int {
	count := s.matches()
	visible := min(count, maxRows)
	if s.windowPinned && visible > 0 {
		start := max(0, min(s.windowStart, count-visible))
		if s.Cursor() >= start && s.Cursor() < start+visible {
			return start
		}
	}
	return completionTrayWindowStart(count, s.Cursor(), maxRows)
}

// View renders the filtered list at its natural content width.
func (s *SlashComplete) View() string { return s.list.View() }

// ViewWidth renders the filtered list as a tray whose rows are padded or clamped
// ANSI-safely to width display columns.
func (s *SlashComplete) ViewWidth(width int) string { return s.list.ViewWidth(width) }

// ViewWindow renders a full-width tray capped to maxRows and keeps the selected command in
// the visible window. View and ViewWidth remain the unbounded variants.
//
// It renders every row and keeps a slice of the result rather than asking the engine for a
// window, because trayList.ViewWindow pages where this tray slides. Cutting rendered lines is
// exact here and only here: the slash tray's layout is one row per item with no padding, so a
// line index IS an item index.
//
// Rendering the whole list first also means the description column is measured across every
// match rather than across the visible slice. That is deliberate: the descriptions then hold
// still while the window scrolls, instead of stepping sideways whenever the widest command
// leaves the window, and the underlines the fuzzy filter draws survive into the windowed view
// -- which is the only view the composer actually renders.
func (s *SlashComplete) ViewWindow(width, maxRows int) string {
	if maxRows <= 0 {
		return ""
	}
	view := s.list.ViewWidth(width)
	if view == "" {
		return ""
	}
	lines := strings.Split(view, "\n")
	if len(lines) <= maxRows {
		return view
	}
	start := s.visibleWindowStart(maxRows)
	return strings.Join(lines[start:start+maxRows], "\n")
}
