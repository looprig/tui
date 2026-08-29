package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/looprig/tui/styles"
)

// TestSessionCompleteRowTwoShowsLastUsedAndShortIDOnly pins the picker's second row to
// exactly the two things worth showing to pick a session to resume: when it was last used
// and its short id. Carbon's picker only ever lists Carbon's own sessions, so an agent
// kind like "carbon:carbon" is identical on every row and carries no information: showing
// it is pure noise. A loop count is likewise not a useful signal for choosing which session
// to resume.
func TestSessionCompleteRowTwoShowsLastUsedAndShortIDOnly(t *testing.T) {
	tray := NewSessionComplete([]SessionItem{
		{ID: "one", Title: "First", LastUsed: "2026-07-15", ShortID: "12345678"},
	})
	lines := strings.Split(tray.ViewWindow(80, 6), "\n")
	if len(lines) < trayHeaderHeight+2 {
		t.Fatalf("row count = %d, want header plus two record rows", len(lines))
	}
	row2 := ansi.Strip(lines[trayHeaderHeight+1])
	if !strings.Contains(row2, "2026-07-15") || !strings.Contains(row2, "12345678") {
		t.Fatalf("row 2 = %q, want the last-used date and short id", row2)
	}
	if strings.Contains(row2, "loop") {
		t.Errorf("row 2 = %q, want no loop count", row2)
	}
}

func TestSessionCompleteHeaderHasTopSpacer(t *testing.T) {
	t.Parallel()

	tray := NewSessionComplete([]SessionItem{{ID: "one", Title: "First", LastUsed: "2026-07-15", ShortID: "12345678"}})
	lines := strings.Split(tray.ViewWindow(80, 6), "\n")
	if got, want := len(lines), 6; got != want {
		t.Fatalf("row count = %d, want %d", got, want)
	}
	if spacer := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(lines[0]), styles.AccentBar)); spacer != "" {
		t.Errorf("top spacer = %q, want an empty rail-carrying row", spacer)
	}
	if title := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(lines[1]), styles.AccentBar)); title != "SESSIONS" {
		t.Errorf("header title = %q, want SESSIONS below the top spacer", title)
	}
}

func TestSessionCompleteRendersTwoRowsWithContinuousUnboxedRail(t *testing.T) {
	tray := NewSessionComplete([]SessionItem{
		{ID: "one", Title: "First", State: "idle", Activity: "2m ago", LastUsed: "2026-07-15", ShortID: "12345678"},
		{ID: "two", Title: "Second", State: "stopped", LastUsed: "2026-07-14", ShortID: "87654321"},
	})
	lines := strings.Split(tray.ViewWindow(80, 9), "\n")
	if len(lines) != 9 {
		t.Fatalf("row count = %d, want header + two + padding + two", len(lines))
	}
	for i, line := range lines {
		plain := ansi.Strip(line)
		if !strings.HasPrefix(plain, styles.AccentBar+" ") {
			t.Errorf("row %d has discontinuous rail: %q", i, plain)
		}
		if strings.ContainsAny(plain, "┌┐└┘╭╮╰╯") {
			t.Errorf("row %d contains a box corner: %q", i, plain)
		}
	}
	if strings.TrimSpace(strings.TrimPrefix(ansi.Strip(lines[6]), styles.AccentBar)) != "" {
		t.Fatalf("record padding row contains content: %q", ansi.Strip(lines[6]))
	}
}

func TestSessionCompleteNavigatesByRecord(t *testing.T) {
	tray := NewSessionComplete([]SessionItem{{ID: "one"}, {ID: "two"}})
	tray.Down()
	if got := tray.Selected().ID; got != "two" {
		t.Fatalf("Down selected %q, want two", got)
	}
	tray.Down()
	if got := tray.Selected().ID; got != "one" {
		t.Fatalf("wrapped Down selected %q, want one", got)
	}
	if tray.SelectWindowRow(6, 9) {
		t.Fatal("padding row changed the selected session")
	}
	if !tray.SelectWindowRow(7, 9) || tray.Selected().ID != "two" {
		t.Fatalf("second record mouse selection = %q, want two", tray.Selected().ID)
	}
}

func TestSessionCompleteStartsOnCurrentSession(t *testing.T) {
	t.Parallel()

	tray := NewSessionComplete([]SessionItem{
		{ID: "one", Title: "First"},
		{ID: "two", Title: "Second", Current: true},
		{ID: "three", Title: "Third"},
	})
	if got := tray.Selected().ID; got != "two" {
		t.Fatalf("initial selection = %q, want current session two", got)
	}
}

func TestSessionCompleteFiltersDescriptionAndIDBelowAHeader(t *testing.T) {
	t.Parallel()

	tray := NewSessionComplete([]SessionItem{
		{ID: "aaaaaaaa-0000-4000-8000-000000000001", Title: "Migration", Description: "billing data backfill", LastUsed: "2026-08-23", ShortID: "aaaaaaaa"},
		{ID: "bbbbbbbb-0000-4000-8000-000000000002", Title: "Release", Description: "deployment verification", LastUsed: "2026-08-22", ShortID: "bbbbbbbb"},
	})

	tray.Filter("billing")
	if got := tray.Selected().ID; got != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Fatalf("description match selected %q, want the original Migration payload", got)
	}
	lines := strings.Split(tray.ViewWindow(80, 8), "\n")
	if got, want := len(lines), 6; got != want {
		t.Fatalf("filtered tray rendered %d rows, want header + spacer + one record = %d:\n%q", got, want, ansi.Strip(strings.Join(lines, "\n")))
	}
	if title := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(lines[1]), styles.AccentBar)); title != "SESSIONS" {
		t.Errorf("header title = %q, want SESSIONS", title)
	}
	if count := ansi.Strip(lines[2]); !strings.Contains(count, "1 of 2 sessions") {
		t.Errorf("header count = %q, want the matching and total session counts", count)
	}
	if spacer := strings.TrimSpace(strings.TrimPrefix(ansi.Strip(lines[3]), styles.AccentBar)); spacer != "" {
		t.Errorf("header spacer = %q, want an empty rail-carrying row", spacer)
	}
	if got := ansi.Strip(strings.Join(lines[4:], "\n")); !strings.Contains(got, "Migration") || strings.Contains(got, "Release") {
		t.Errorf("filtered records = %q, want Migration only", got)
	}

	tray.Filter("bbbbbbbb-0000")
	if got := tray.Selected().ID; got != "bbbbbbbb-0000-4000-8000-000000000002" {
		t.Errorf("full ID match selected %q, want the original Release payload", got)
	}
}

// TestSessionCompleteHidesWhenEmpty pins the nil contract. The surface decides whether the
// picker exists by testing the pointer against nil, so an empty list must yield nil rather than
// a live object that happens to render "": the latter would keep the panel "open" with nothing
// in it, and Escape would be the only way out of a tray the user can neither see nor use.
func TestSessionCompleteHidesWhenEmpty(t *testing.T) {
	t.Parallel()

	if got := NewSessionComplete(nil); got != nil {
		t.Errorf("NewSessionComplete(nil) = %v, want nil", got)
	}
	if got := NewSessionComplete([]SessionItem{}); got != nil {
		t.Errorf("NewSessionComplete(empty) = %v, want nil", got)
	}
}

// TestSessionCompleteRowsCarryTitleThenMetadata pins which field lands on which of a record's
// two rows: the title with its state and activity inline, then the last-used date and the SHORT
// id beneath. It also pins what is deliberately absent -- the full session ID is what the caller
// parses as a UUID, and it must stay off screen; only its short form is fit to show.
func TestSessionCompleteRowsCarryTitleThenMetadata(t *testing.T) {
	t.Parallel()

	tray := NewSessionComplete([]SessionItem{
		{ID: "6f1c4e2a-0000-4000-8000-000000000001", Title: "First", State: "idle", Activity: "2m ago", LastUsed: "2026-07-15", ShortID: "12345678"},
	})
	lines := strings.Split(tray.ViewWindow(80, 6), "\n")
	if len(lines) != 6 {
		t.Fatalf("one record rendered %d rows, want header + 2:\n%q", len(lines), lines)
	}

	title, meta := ansi.Strip(lines[trayHeaderHeight]), ansi.Strip(lines[trayHeaderHeight+1])
	for _, want := range []string{"First", "idle", "2m ago"} {
		if !strings.Contains(title, want) {
			t.Errorf("title row = %q, want it to contain %q", title, want)
		}
	}
	for _, want := range []string{"2026-07-15", "12345678"} {
		if !strings.Contains(meta, want) {
			t.Errorf("metadata row = %q, want it to contain %q", meta, want)
		}
	}
	// The state belongs to the title row; repeating it below would waste the only other row.
	if strings.Contains(meta, "idle") {
		t.Errorf("metadata row = %q, want the state left on the title row", meta)
	}
	if view := ansi.Strip(tray.ViewWindow(80, 6)); strings.Contains(view, "6f1c4e2a-0000") {
		t.Errorf("view leaks the full session id: %q", view)
	}
}

// TestSessionCompleteBandsBothRowsOfTheSelectedRecord is the assertion the migration exists to
// make good on. A record is two rows, so the selection has to cover both: banding only the title
// would read as a one-row entry with an orphaned date stuck beneath it. The spacer above the
// selected record stays unbanded, because it is the gap beside the selection, not part of it.
func TestSessionCompleteBandsBothRowsOfTheSelectedRecord(t *testing.T) {
	t.Parallel()

	tray := NewSessionComplete([]SessionItem{
		{ID: "one", Title: "First", LastUsed: "2026-07-15", ShortID: "12345678"},
		{ID: "two", Title: "Second", LastUsed: "2026-07-14", ShortID: "87654321"},
	})
	tray.Down() // select the second record: rows 7 and 8 after the header

	lines := strings.Split(tray.ViewWindow(80, 9), "\n")
	if len(lines) != 9 {
		t.Fatalf("two records rendered %d rows, want header + 5:\n%q", len(lines), lines)
	}
	band := selectedBandOpen(t)
	for i, line := range lines {
		banded := strings.HasPrefix(line, band)
		want := i == 7 || i == 8
		if banded != want {
			t.Errorf("row %d banded = %v, want %v: %q", i, banded, want, line)
		}
	}
}

// TestSessionCompleteViewWindowCountsScreenRowsNotRecords pins the maxRows arithmetic. maxRows
// is a budget of SCREEN rows: the first record costs two, and every record after it costs three,
// because only the gaps BETWEEN records carry a spacer. So an odd budget like 5 buys two whole
// records (2 + 1 + 2) where dividing by three would have bought one and wasted the rest.
//
// A budget too small for both of a record's rows renders nothing at all: a title with its date
// cut off tells the user less than an absent tray does.
func TestSessionCompleteViewWindowCountsScreenRowsNotRecords(t *testing.T) {
	t.Parallel()

	items := make([]SessionItem, 4)
	for i := range items {
		items[i] = SessionItem{ID: fmt.Sprint(i), Title: fmt.Sprintf("Session %d", i), LastUsed: "2026-07-15", ShortID: "1234abcd"}
	}

	for _, tc := range []struct {
		maxRows, wantRows int
	}{
		{maxRows: 0, wantRows: 0},
		{maxRows: 1, wantRows: 0},
		{maxRows: 2, wantRows: 0},
		{maxRows: 3, wantRows: 0},
		{maxRows: 4, wantRows: 4},
		{maxRows: 5, wantRows: 4},
		{maxRows: 6, wantRows: 6},
		{maxRows: 7, wantRows: 6},
		{maxRows: 8, wantRows: 6},
		{maxRows: 9, wantRows: 9},
		{maxRows: 10, wantRows: 9},
		{maxRows: 12, wantRows: 12},
		{maxRows: 15, wantRows: 15},
		{maxRows: 99, wantRows: 15},
	} {
		view := NewSessionComplete(items).ViewWindow(80, tc.maxRows)
		got := 0
		if view != "" {
			got = len(strings.Split(view, "\n"))
		}
		if got != tc.wantRows {
			t.Errorf("ViewWindow(80, %d) rendered %d rows, want %d", tc.maxRows, got, tc.wantRows)
		}
		// The budget is a cap, never merely a hint: overshooting would push the composer
		// off the bottom of the screen.
		if tc.maxRows > 0 && got > tc.maxRows {
			t.Errorf("ViewWindow(80, %d) rendered %d rows, over its budget", tc.maxRows, got)
		}
	}
	if got := NewSessionComplete(items).ViewWindow(0, 7); got != "" {
		t.Errorf("ViewWindow(width=0) = %q, want empty", got)
	}
}

// TestSessionCompleteSelectWindowRowMapsRowsToRecords pins click-to-record mapping now that a
// record spans several rows. Either content row selects its own record -- clicking the date
// picks the session whose date it is, because the date is part of that record and not a target
// of its own -- while the spacer between records is inert, since it belongs to neither
// neighbour and choosing one would move the cursor somewhere the user did not point.
func TestSessionCompleteSelectWindowRowMapsRowsToRecords(t *testing.T) {
	t.Parallel()

	items := []SessionItem{
		{ID: "one", Title: "First", LastUsed: "2026-07-15", ShortID: "aaaaaaaa"},
		{ID: "two", Title: "Second", LastUsed: "2026-07-14", ShortID: "bbbbbbbb"},
		{ID: "three", Title: "Third", LastUsed: "2026-07-13", ShortID: "cccccccc"},
	}
	// Rows: 0-3 header | 4,5 = first | 6 = spacer | 7,8 = second | 9 = spacer |
	// 10,11 = third.
	const maxRows = 12
	if got := len(strings.Split(NewSessionComplete(items).ViewWindow(80, maxRows), "\n")); got != maxRows {
		t.Fatalf("the window under test renders %d rows, want %d", got, maxRows)
	}

	for _, tc := range []struct {
		row      int
		wantID   string // the record selected afterwards
		wantMove bool
	}{
		{row: 0, wantID: "three", wantMove: false}, // top spacer: inert
		{row: 1, wantID: "three", wantMove: false}, // header title: inert
		{row: 2, wantID: "three", wantMove: false}, // header count: inert
		{row: 3, wantID: "three", wantMove: false}, // header spacer: inert
		{row: 4, wantID: "one", wantMove: true},    // title of the first
		{row: 5, wantID: "one", wantMove: true},    // its date row selects it too
		{row: 6, wantID: "three", wantMove: false}, // spacer: inert
		{row: 7, wantID: "two", wantMove: true},
		{row: 8, wantID: "two", wantMove: true},     // date row of the second
		{row: 9, wantID: "three", wantMove: false},  // spacer: inert
		{row: 10, wantID: "three", wantMove: false}, // already selected: no change
		{row: 11, wantID: "three", wantMove: false}, // its date row, likewise no change
		{row: 9, wantID: "three", wantMove: false},  // past the window
		{row: 99, wantID: "three", wantMove: false},
		{row: -1, wantID: "three", wantMove: false},
	} {
		tray := NewSessionComplete(items)
		tray.Up() // start on the LAST record, so a move is visible from every row
		if got := tray.Selected().ID; got != "three" {
			t.Fatalf("Up() from the first record selected %q, want three (wrapping)", got)
		}
		if got := tray.SelectWindowRow(tc.row, maxRows); got != tc.wantMove {
			t.Errorf("SelectWindowRow(%d) = %v, want %v", tc.row, got, tc.wantMove)
		}
		if got := tray.Selected().ID; got != tc.wantID {
			t.Errorf("after SelectWindowRow(%d) selected %q, want %q", tc.row, got, tc.wantID)
		}
	}
}

// TestSessionCompleteCursorCountsRecordsNotRows guards the number the surface reads back. The
// cursor indexes RECORDS, so it stays 0..len-1 however many rows each record draws; a cursor
// that had drifted onto row numbers would still look plausible on a one-record list and break
// on every longer one.
func TestSessionCompleteCursorCountsRecordsNotRows(t *testing.T) {
	t.Parallel()

	tray := NewSessionComplete([]SessionItem{{ID: "one"}, {ID: "two"}, {ID: "three"}})
	if got := tray.Cursor(); got != 0 {
		t.Errorf("initial cursor = %d, want 0", got)
	}
	tray.Down()
	if got := tray.Cursor(); got != 1 {
		t.Errorf("cursor after one Down = %d, want 1 (records, not rows)", got)
	}
	tray.Up()
	tray.Up()
	if got, want := tray.Cursor(), 2; got != want {
		t.Errorf("cursor after wrapping up = %d, want %d", got, want)
	}
	if got := tray.Selected().ID; got != "three" {
		t.Errorf("selected after wrapping up = %q, want three", got)
	}
}

// TestSessionCompleteFillsItsBudgetOnTheLastRecord is the concrete failure the shared sliding
// window fixes. Five records with eleven rows to spare and the cursor on the LAST one used to
// render two rows -- one record -- because the engine paginated: the cursor sat alone on a
// short final page, so the tray SHRANK under it and stranded eleven rows of free space. A
// sliding window fills what it is given and scrolls the records above the cursor into view.
func TestSessionCompleteFillsItsBudgetOnTheLastRecord(t *testing.T) {
	t.Parallel()

	items := make([]SessionItem, 5)
	for i := range items {
		items[i] = SessionItem{ID: fmt.Sprint(i), Title: fmt.Sprintf("Rec%d", i), LastUsed: "2026-07-15", ShortID: "1234abcd"}
	}
	tray := NewSessionComplete(items)
	tray.Up() // wrap to the last record
	if got := tray.Selected().Title; got != "Rec4" {
		t.Fatalf("Up() from the first record selected %q, want Rec4", got)
	}

	// 15 rows is the four-row header plus exactly four records: 4 + 2 + 1 + 2 + 1 + 2 + 1 + 2.
	view := tray.ViewWindow(80, 15)
	lines := strings.Split(view, "\n")
	if len(lines) != 15 {
		t.Fatalf("ViewWindow(80, 15) rendered %d rows, want 15:\n%q", len(lines), ansi.Strip(view))
	}
	plain := ansi.Strip(view)
	for _, want := range []string{"Rec1", "Rec2", "Rec3", "Rec4"} {
		if !strings.Contains(plain, want) {
			t.Errorf("window = %q, want it to hold the last four records including %q", plain, want)
		}
	}
	// Rec0 is the one record that does NOT fit, and it is the one scrolled off the top.
	if strings.Contains(plain, "Rec0") {
		t.Errorf("window = %q, want Rec0 scrolled off: only four records fit", plain)
	}
}
