package components

import (
	"fmt"
	"strings"
)

// SessionItem is the already-formatted, secret-free view data for one session record.
type SessionItem struct {
	ID       string
	Title    string
	State    string
	Activity string
	LastUsed string
	ShortID  string
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
// panel's private styles.TraySelectedBg band. The selected record is now banded with
// styles.SelectedRow like every other tray, which is a deliberate and visible change.
//
// It keeps the SessionItem slice alongside the tray because the tray's rows are DISPLAY strings
// only. Selected must hand back the record's ID for the caller to parse as a UUID, and an ID
// never appears on screen, so it cannot be recovered from a rendered row.
type SessionComplete struct {
	tray  *trayList
	items []SessionItem
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
		}
	}
	// Width zero: every render supplies its own width, and this panel never calls the
	// width-less View.
	return &SessionComplete{tray: newTrayList(rows, 0, sessionTrayLayout), items: kept}
}

// Selected is the record under the cursor. It indexes the ORIGINAL slice by the tray's cursor,
// which is sound because this picker never filters, so the tray's visible items and its items
// are the same list in the same order.
//
// The bounds check is not that invariant guarded twice: trayList.Selected is itself fail-safe,
// and a panel that panicked where its engine returns a zero value would be the more surprising
// of the two.
func (s *SessionComplete) Selected() SessionItem {
	i := s.tray.Cursor()
	if i < 0 || i >= len(s.items) {
		return SessionItem{}
	}
	return s.items[i]
}

// Cursor is the selected RECORD's index, not its row. A record spans two rows plus a spacer, so
// the two numbers diverge as soon as the cursor leaves the first record.
func (s *SessionComplete) Cursor() int { return s.tray.Cursor() }

// Up moves to the previous record, wrapping to the last.
func (s *SessionComplete) Up() { s.tray.Up() }

// Down moves to the next record, wrapping to the first.
func (s *SessionComplete) Down() { s.tray.Down() }

// SelectWindowRow moves the cursor to the record occupying a clicked VISUAL row of the current
// maxRows window, reporting whether the selection changed.
//
// Either of a record's two content rows selects it: the metadata row is part of the record the
// user pointed at rather than a target of its own, so clicking the date picks the session whose
// date it is. The spacer between records is inert, because it belongs to neither neighbour and
// picking one would move the cursor somewhere the user did not point.
func (s *SessionComplete) SelectWindowRow(row, maxRows int) bool {
	return s.tray.SelectWindowRow(row, maxRows)
}

// ViewWindow renders the picker into at most maxRows SCREEN rows, not maxRows records: two
// screen rows buy one record, and each further record costs three more, because only the rows
// BETWEEN records carry a spacer. A maxRows too small for one record's two content rows renders
// nothing rather than a title with its metadata cut off.
func (s *SessionComplete) ViewWindow(width, maxRows int) string {
	return s.tray.ViewWindow(width, maxRows)
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
