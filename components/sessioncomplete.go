package components

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
)

// SessionItem is the already-formatted, secret-free view data for one session record.
type SessionItem struct {
	ID    string
	Title string
	// Current marks the session backing the active Agent. It remains independent from the
	// cursor after the user navigates to a different resume target.
	Current bool
	// Description is filter-only session context. It stays out of the compact two-row
	// picker so the visible row remains a quick scan of title, activity, date, and short ID.
	Description string
	State       string
	Activity    string
	LastUsed    string
	ShortID     string
}

// sessionTrayLayout is the picker's shape: each record is two STACKED rows -- the title, then
// its metadata beneath -- with one rail-carrying spacer between records.
//
// PadH stays zero. The session tray has never been inset, and the spacer alone separates
// records; a horizontal inset here would make this the one tray whose rail sits further from
// its text than the other three.
var sessionTrayLayout = trayLayout{Stacked: true, PadV: 1}

// SessionComplete is the /resume session picker, backed by the shared tray engine in its
// stacked layout. It used to render itself through a bespoke renderSessionLine that duplicated
// the rail, the background fill and the truncation; that renderer is gone, and with it the
// panel's private selection fill. The selected record is now banded with styles.SelectedRow
// like every other tray, which is a deliberate and visible change.
//
// It keeps the SessionItem slice alongside the tray because the tray's rows are DISPLAY strings
// only. Selected must hand back the record's ID for the caller to parse as a UUID, and an ID
// never appears on screen, so it cannot be recovered from a rendered row.
type SessionComplete struct {
	tray  *trayList
	items []SessionItem
	query string
}

// NewSessionComplete builds the picker, or returns nil when there is nothing to pick. Nil means
// "panel hidden" to the caller, so an empty tray must never be a non-nil object that renders to
// the empty string: the surface tests for nil, not for "".
func NewSessionComplete(items []SessionItem) *SessionComplete {
	if len(items) == 0 {
		return nil
	}
	kept := append([]SessionItem(nil), items...)
	rows := make([]completionTrayRow, len(kept))
	for i, item := range kept {
		rows[i] = completionTrayRow{
			primary:   item.Title + joinMetadata(item.State, item.Activity),
			secondary: fmt.Sprintf("%s · %s", item.LastUsed, item.ShortID),
			filter:    item.Title,
			current:   item.Current,
		}
	}
	// Width zero: every render supplies its own width, and this panel never calls the
	// width-less View.
	tray := newTrayList(rows, 0, sessionTrayLayout)
	tray.SetFilterFunc(sessionFilter(kept))
	return &SessionComplete{tray: tray, items: kept}
}

// Selected resolves through the tray's original-item cursor so filtering may reorder or hide
// records without changing the opaque session ID that resume receives.
func (s *SessionComplete) Selected() SessionItem {
	if s.tray.ChoiceLen() == 0 {
		return SessionItem{}
	}
	i := s.tray.UnfilteredCursor()
	if i < 0 || i >= len(s.items) {
		return SessionItem{}
	}
	return s.items[i]
}

// Len is the number of sessions that can currently be selected.
func (s *SessionComplete) Len() int { return s.tray.ChoiceLen() }

// Cursor is the selected RECORD's index, not its row. A record spans two rows plus a spacer, so
// the two numbers diverge as soon as the cursor leaves the first record.
func (s *SessionComplete) Cursor() int { return s.tray.Cursor() }

// Up moves to the previous record, wrapping to the last.
func (s *SessionComplete) Up() { s.tray.Up() }

// Down moves to the next record, wrapping to the first.
func (s *SessionComplete) Down() { s.tray.Down() }

// Filter applies the inline search text. A blank query restores the unfiltered projection
// rather than asking Bubbles to filter on "", whose cursor mapping is unsuitable for payload
// selection (see trayList.ResetFilter).
func (s *SessionComplete) Filter(query string) {
	s.query = query
	if strings.TrimSpace(query) == "" {
		s.tray.ResetFilter()
		return
	}
	s.tray.Filter(query)
}

func (s *SessionComplete) summary() string {
	matched, total := s.tray.Len(), len(s.items)
	noun := "sessions"
	if total == 1 {
		noun = "session"
	}
	if strings.TrimSpace(s.query) == "" {
		return fmt.Sprintf("%d %s", total, noun)
	}
	return fmt.Sprintf("%d of %d %s", matched, total, noun)
}

// SelectWindowRow moves the cursor to the record occupying a clicked VISUAL row of the current
// maxRows window, reporting whether the selection changed.
//
// Either of a record's two content rows selects it: the metadata row is part of the record the
// user pointed at rather than a target of its own, so clicking the date picks the session whose
// date it is. The spacer between records is inert, because it belongs to neither neighbour and
// picking one would move the cursor somewhere the user did not point.
func (s *SessionComplete) SelectWindowRow(row, maxRows int) bool {
	if row < trayHeaderHeight || maxRows <= trayHeaderHeight {
		return false
	}
	return s.tray.SelectWindowRow(row-trayHeaderHeight, maxRows-trayHeaderHeight)
}

// ViewWindow renders the picker into at most maxRows SCREEN rows, not maxRows records: two
// screen rows buy one record, and each further record costs three more, because only the rows
// BETWEEN records carry a spacer. A maxRows too small for one record's two content rows renders
// nothing rather than a title with its metadata cut off.
func (s *SessionComplete) ViewWindow(width, maxRows int) string {
	if width <= 0 || maxRows < trayHeaderHeight {
		return ""
	}
	header := renderTrayHeader(width, "SESSIONS", s.summary())
	bodyRows := maxRows - trayHeaderHeight
	if body := s.tray.ViewWindow(width, bodyRows); body != "" {
		return header + "\n" + body
	}
	if strings.TrimSpace(s.query) != "" && bodyRows > 0 {
		return header + "\n" + renderTrayHint(width, "No matching sessions")
	}
	return header
}

// sessionFilter keeps title hits on the engine's ordinary fuzzy matcher, whose indices map
// safely onto the rendered title, and adds description or identifier substring hits without
// indices. Those fields are intentionally not drawn, so underlining a title rune for them
// would be a lie.
func sessionFilter(items []SessionItem) list.FilterFunc {
	return func(term string, labels []string) []list.Rank {
		ranks := list.DefaultFilter(term, labels)
		matched := make(map[int]bool, len(ranks))
		for _, rank := range ranks {
			matched[rank.Index] = true
		}
		needle := strings.ToLower(strings.TrimSpace(term))
		if needle == "" {
			return ranks
		}
		for i, item := range items {
			if matched[i] {
				continue
			}
			haystack := strings.ToLower(strings.Join([]string{item.Description, item.ID, item.ShortID}, "\n"))
			if strings.Contains(haystack, needle) {
				ranks = append(ranks, list.Rank{Index: i})
			}
		}
		return ranks
	}
}

// joinMetadata appends the non-empty parts to the title row as " · "-separated trailers. It
// outlived the retired renderer because it composes CONTENT, not layout: the state and the
// activity share the title's row, so they have to be one string before the tray ever sees them.
func joinMetadata(parts ...string) string {
	var kept []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return " · " + strings.Join(kept, " · ")
}
