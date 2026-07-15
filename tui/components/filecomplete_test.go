package components

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestFileCompleteViewWindowKeepsSelectionVisible(t *testing.T) {
	t.Parallel()

	items := make([]FileItem, 8)
	for i := range items {
		items[i] = FileItem{Path: fmt.Sprintf("file-%d.go", i)}
	}
	f := NewFileComplete(items)
	if f == nil {
		t.Fatal("NewFileComplete() = nil, want non-nil")
	}
	for range 6 {
		f.Down()
	}

	view := f.ViewWindow(40, 3)
	if got := lipgloss.Height(view); got != 3 {
		t.Fatalf("ViewWindow() height = %d, want 3; view=%q", got, view)
	}
	if !strings.Contains(ansi.Strip(view), "@file-6.go") {
		t.Errorf("ViewWindow() = %q, want selected @file-6.go visible", view)
	}
	selectedOpen, _ := styles.DeriveBackgroundSGR(styles.CardSelectedBg)
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), "@file-6.go") && !strings.HasPrefix(line, selectedOpen) {
			t.Errorf("visible @file-6.go row is not selected: %q", line)
		}
	}
	if got := f.ViewWindow(40, 0); got != "" {
		t.Errorf("ViewWindow(maxRows=0) = %q, want empty", got)
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

func TestFileCompleteViewWidthSanitizesControlCharactersWithoutChangingSelection(t *testing.T) {
	t.Parallel()

	const width = 64
	tests := []struct {
		name        string
		path        string
		wantDisplay string
	}{
		{name: "newline", path: "line\nbreak", wantDisplay: "line�break"},
		{name: "tab", path: "tab\tfile", wantDisplay: "tab�file"},
		{name: "carriage return", path: "return\rfile", wantDisplay: "return�file"},
		{name: "escape CSI", path: "color\x1b[31mred\x1b[0m", wantDisplay: "color�[31mred�[0m"},
		{name: "C1 CSI", path: "c1\u009b31mred", wantDisplay: "c1�31mred"},
		{name: "OSC", path: "title\x1b]0;pwn\aend", wantDisplay: "title�]0;pwn�end"},
	}
	items := make([]FileItem, len(tests))
	for i, tt := range tests {
		items[i] = FileItem{Path: tt.path}
	}
	f := NewFileComplete(items)
	if f == nil {
		t.Fatal("NewFileComplete() = nil, want non-nil")
	}

	view := f.ViewWidth(width)
	lines := strings.Split(view, "\n")
	if len(lines) != len(tests) {
		t.Fatalf("ViewWidth() rendered %d rows for %d candidates; view=%q", len(lines), len(tests), view)
	}
	for i, tt := range tests {
		line := lines[i]
		if got := lipgloss.Width(line); got != width {
			t.Errorf("%s row width = %d, want %d; row=%q", tt.name, got, width, line)
		}
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "@"+tt.wantDisplay) {
			t.Errorf("%s row = %q, want sanitized path %q", tt.name, plain, "@"+tt.wantDisplay)
		}
		for _, r := range plain {
			if unicode.IsControl(r) {
				t.Errorf("%s row contains control character %U: %q", tt.name, r, plain)
			}
		}
	}
	for _, injected := range []string{"\x1b[31m", "\x1b[0m", "\u009b31m", "\x1b]0;pwn\a"} {
		if strings.Contains(view, injected) {
			t.Errorf("ViewWidth() retained injected terminal sequence %q", injected)
		}
	}

	for i, tt := range tests {
		if got := f.Selected().Path; got != tt.path {
			t.Errorf("candidate %d Selected().Path = %q, want original exact path %q", i, got, tt.path)
		}
		f.Down()
	}
}
