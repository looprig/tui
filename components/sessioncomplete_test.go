package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/looprig/tui/styles"
)

func TestSessionCompleteRendersTwoRowsWithContinuousUnboxedRail(t *testing.T) {
	tray := NewSessionComplete([]SessionItem{
		{ID: "one", Title: "First", State: "idle", Activity: "2m ago", Kind: "coderig", Loops: 3, Created: "2026-07-15", ShortID: "12345678"},
		{ID: "two", Title: "Second", State: "stopped", Kind: "coderig", Loops: 1, Created: "2026-07-14", ShortID: "87654321"},
	})
	lines := strings.Split(tray.ViewWindowBackground(80, 6, styles.TraySelectedBg), "\n")
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
