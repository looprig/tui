package components

import "strings"

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

var slashRelatedWords = map[string][]string{
	"/clear":   {"new", "reset", "restart"},
	"/compact": {"compress", "context", "shorten"},
	"/exit":    {"quit", "close", "leave"},
}

// SlashComplete is a filtered command list with a wrapping cursor.
type SlashComplete struct {
	items  []SlashCmd
	cursor int
}

// NewSlashComplete returns a case-insensitive, relevance-ranked completer for prefix.
// The optional leading slash is ignored while matching. Returns nil when nothing
// matches (nil = panel hidden).
func NewSlashComplete(prefix string) *SlashComplete {
	query := strings.ToLower(strings.TrimPrefix(prefix, "/"))
	if query == "" {
		return &SlashComplete{items: append([]SlashCmd(nil), SlashCommands...)}
	}

	var matches []SlashCmd
	for rank := 0; rank < 5; rank++ {
		for _, command := range SlashCommands {
			if slashMatchRank(command, query) == rank {
				matches = append(matches, command)
			}
		}
	}
	if len(matches) == 0 {
		return nil
	}
	return &SlashComplete{items: matches, cursor: 0}
}

func slashMatchRank(command SlashCmd, query string) int {
	name := strings.ToLower(strings.TrimPrefix(command.Name, "/"))
	switch {
	case name == query:
		return 0
	case strings.HasPrefix(name, query):
		return 1
	}

	related := slashRelatedWords[command.Name]
	for _, word := range related {
		word = strings.ToLower(word)
		if word == query {
			return 2
		}
	}
	for _, word := range related {
		word = strings.ToLower(word)
		if strings.Contains(word, query) {
			return 3
		}
	}
	if strings.Contains(name, query) {
		return 4
	}
	return -1
}

// Selected returns the item under the cursor.
func (s *SlashComplete) Selected() SlashCmd {
	return s.items[s.cursor]
}

// Up moves the cursor up, wrapping to the bottom.
func (s *SlashComplete) Up() {
	s.cursor = (s.cursor - 1 + len(s.items)) % len(s.items)
}

// Down moves the cursor down, wrapping to the top.
func (s *SlashComplete) Down() {
	s.cursor = (s.cursor + 1) % len(s.items)
}

func (s *SlashComplete) trayRows() []completionTrayRow {
	rows := make([]completionTrayRow, len(s.items))
	for i, item := range s.items {
		rows[i] = completionTrayRow{primary: item.Name, secondary: item.Desc}
	}
	return rows
}

// View renders the filtered list at its natural content width.
func (s *SlashComplete) View() string {
	rows := s.trayRows()
	return renderCompletionTray(rows, s.cursor, completionTrayNaturalWidth(rows))
}

// ViewWidth renders the filtered list as a tray whose rows are padded or clamped
// ANSI-safely to width display columns.
func (s *SlashComplete) ViewWidth(width int) string {
	return renderCompletionTray(s.trayRows(), s.cursor, width)
}
