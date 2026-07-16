package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestValueCompleteReturnsOpaqueIDAndFiltersAliases(t *testing.T) {
	tray := NewValueComplete([]ValueItem{
		{ID: "base", Label: "Default", Aliases: []string{"normal"}},
		{ID: "review", Label: "Review", Description: "read carefully"},
	}, "normal")
	if tray == nil || tray.Selected().ID != "base" {
		t.Fatalf("selected = %#v, want opaque base id", tray)
	}
	if NewValueComplete([]ValueItem{{ID: "x", Label: "Alpha"}}, "missing") != nil {
		t.Fatal("zero-result query returned a visible tray")
	}
}

func TestValueCompleteNavigationWrapsAndViewClamps(t *testing.T) {
	tray := NewValueComplete([]ValueItem{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta"}}, "")
	tray.Up()
	if tray.Selected().ID != "b" {
		t.Fatalf("Up() selected %q, want b", tray.Selected().ID)
	}
	tray.Down()
	if tray.Selected().ID != "a" {
		t.Fatalf("Down() selected %q, want a", tray.Selected().ID)
	}
	for _, line := range strings.Split(tray.ViewWindow(8, 2), "\n") {
		if ansi.StringWidth(line) > 8 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	if !tray.SelectWindowRow(1, 2) || tray.Selected().ID != "b" {
		t.Fatalf("mouse row selection = %q, want b", tray.Selected().ID)
	}
}
