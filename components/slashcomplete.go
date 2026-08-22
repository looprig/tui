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
// The items, the cursor, the filter and the sliding window all live in the shared trayList
// engine, so this type is a translation layer: SlashCmd in, trayItem out, and back again.
type SlashComplete struct {
	list *trayList
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
	list.Filter(strings.TrimPrefix(prefix, "/"))

	matched := list.Rows()
	if len(matched) == 0 {
		return nil
	}
	// The natural width is measured from the MATCHES rather than from the catalog: the tray
	// is only ever as wide as the rows it draws.
	list.SetWidth(completionTrayNaturalWidth(matched))
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
func (s *SlashComplete) Up() { s.list.Up() }

// Down moves the cursor down, wrapping to the top.
func (s *SlashComplete) Down() { s.list.Down() }

// SelectWindowRow moves the cursor to a row in the currently rendered maxRows window.
// It returns whether the selection changed; rows outside the visible window are ignored.
func (s *SlashComplete) SelectWindowRow(row, maxRows int) bool {
	return s.list.SelectWindowRow(row, maxRows)
}

// View renders the filtered list at its natural content width.
func (s *SlashComplete) View() string { return s.list.View() }

// ViewWidth renders the filtered list as a tray whose rows are padded or clamped
// ANSI-safely to width display columns.
func (s *SlashComplete) ViewWidth(width int) string { return s.list.ViewWidth(width) }

// ViewWindow renders a full-width tray capped to maxRows and keeps the selected command in
// the visible window. View and ViewWidth remain the unbounded variants.
func (s *SlashComplete) ViewWindow(width, maxRows int) string {
	return s.list.ViewWindow(width, maxRows)
}
