package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/looprig/cli/tui/styles"
)

func TestFileCompleteSelectionWrap(t *testing.T) {
	t.Parallel()

	f := NewFileComplete([]FileItem{{Path: "src/main.go"}, {Path: "src/internal", IsDir: true}})
	if f == nil {
		t.Fatal("NewFileComplete() = nil, want non-nil")
	}
	if got := f.Selected().Path; got != "src/main.go" {
		t.Fatalf("initial Selected().Path = %q, want src/main.go", got)
	}
	f.Up()
	if got := f.Selected().Path; got != "src/internal" {
		t.Errorf("Up() Selected().Path = %q, want src/internal", got)
	}
	f.Down()
	if got := f.Selected().Path; got != "src/main.go" {
		t.Errorf("Down() Selected().Path = %q, want src/main.go", got)
	}
}

func TestFileCompleteViewWidthShowsFullAtPathsAndTraySelection(t *testing.T) {
	t.Parallel()

	const width = 40
	f := NewFileComplete([]FileItem{
		{Path: "tui/screen.go"},
		{Path: "tui/components", IsDir: true},
	})
	if f == nil {
		t.Fatal("NewFileComplete() = nil, want non-nil")
	}
	f.Down()

	type widthViewer interface {
		ViewWidth(int) string
	}
	viewer, ok := any(f).(widthViewer)
	if !ok {
		t.Fatal("FileComplete does not implement ViewWidth(int)")
	}
	lines := strings.Split(viewer.ViewWidth(width), "\n")
	if len(lines) != 2 {
		t.Fatalf("ViewWidth() row count = %d, want 2", len(lines))
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Errorf("row %d width = %d, want %d", i, got, width)
		}
		plain := stripANSI(line)
		if !strings.HasPrefix(plain, styles.AccentBar) {
			t.Errorf("row %d = %q, want leading tray rail", i, plain)
		}
		if strings.Contains(plain, ">") || strings.Contains(plain, "▸") {
			t.Errorf("row %d = %q, want no selection arrow", i, plain)
		}
	}
	if plain := stripANSI(lines[0]); !strings.Contains(plain, "@tui/screen.go") {
		t.Errorf("file row = %q, want complete @path", plain)
	}
	if plain := stripANSI(lines[1]); !strings.Contains(plain, "@tui/components/") {
		t.Errorf("directory row = %q, want complete @path with trailing slash", plain)
	}

	panelOpen, _ := styles.DeriveBackgroundSGR(styles.PanelBg)
	selectedOpen, _ := styles.DeriveBackgroundSGR(styles.CardSelectedBg)
	if !strings.HasPrefix(lines[0], panelOpen) {
		t.Errorf("unselected row does not open with PanelBg: %q", lines[0])
	}
	if !strings.Contains(lines[0], strings.TrimSuffix(styles.AccentBarStyle.Render(styles.AccentBar), "\x1b[m")) {
		t.Errorf("unselected row does not use AccentBarStyle: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], selectedOpen) {
		t.Errorf("selected row does not open with CardSelectedBg: %q", lines[1])
	}
	if !strings.Contains(lines[1], strings.TrimSuffix(styles.CardRailStyle.Render(styles.AccentBar), "\x1b[m")) {
		t.Errorf("selected row does not use CardRailStyle: %q", lines[1])
	}
	if !strings.Contains(lines[1], strings.TrimSuffix(styles.CardSelectedStyle.Render("@tui/components/"), "\x1b[m")) {
		t.Errorf("selected path is not rendered with CardSelectedStyle: %q", lines[1])
	}
}

func TestFileCompleteViewWidthClampsUnicodePathANSISafely(t *testing.T) {
	t.Parallel()

	const width = 10
	f := NewFileComplete([]FileItem{{Path: "目录/界界界.go"}})
	type widthViewer interface {
		ViewWidth(int) string
	}
	viewer, ok := any(f).(widthViewer)
	if !ok {
		t.Fatal("FileComplete does not implement ViewWidth(int)")
	}
	view := viewer.ViewWidth(width)
	if got := lipgloss.Width(view); got != width {
		t.Fatalf("ViewWidth() width = %d, want %d; view=%q", got, width, view)
	}
	if strings.ContainsRune(stripANSI(view), '\uFFFD') {
		t.Fatalf("ViewWidth() split a Unicode sequence: %q", view)
	}
}
