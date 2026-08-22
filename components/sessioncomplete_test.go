package components

import (
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
	lines := strings.Split(tray.ViewWindow(80, 3), "\n")
	if len(lines) < 2 {
		t.Fatalf("row count = %d, want at least 2", len(lines))
	}
	row2 := ansi.Strip(lines[1])
	if !strings.Contains(row2, "2026-07-15") || !strings.Contains(row2, "12345678") {
		t.Fatalf("row 2 = %q, want the last-used date and short id", row2)
	}
	if strings.Contains(row2, "loop") {
		t.Errorf("row 2 = %q, want no loop count", row2)
	}
}

func TestSessionCompleteRendersTwoRowsWithContinuousUnboxedRail(t *testing.T) {
	tray := NewSessionComplete([]SessionItem{
		{ID: "one", Title: "First", State: "idle", Activity: "2m ago", LastUsed: "2026-07-15", ShortID: "12345678"},
		{ID: "two", Title: "Second", State: "stopped", LastUsed: "2026-07-14", ShortID: "87654321"},
	})
	lines := strings.Split(tray.ViewWindow(80, 6), "\n")
	if len(lines) != 5 {
		t.Fatalf("row count = %d, want 5 (two + padding + two)", len(lines))
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
	if strings.TrimSpace(strings.TrimPrefix(ansi.Strip(lines[2]), styles.AccentBar)) != "" {
		t.Fatalf("padding row contains content: %q", ansi.Strip(lines[2]))
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
	if tray.SelectWindowRow(2, 6) {
		t.Fatal("padding row changed the selected session")
	}
	if !tray.SelectWindowRow(3, 6) || tray.Selected().ID != "two" {
		t.Fatalf("second record mouse selection = %q, want two", tray.Selected().ID)
	}
}
