package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/looprig/cli/tui/styles"
)

func TestNewSlashComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prefix    string
		wantNil   bool
		wantCount int
		wantNames []string
	}{
		{
			name:      "prefix /cl ranks clear before related close match",
			prefix:    "/cl",
			wantCount: 2,
			wantNames: []string{"/clear", "/exit"},
		},
		{
			name:      "slash shows visible commands in stable order",
			prefix:    "/",
			wantCount: 3,
			wantNames: []string{"/clear", "/compact", "/exit"},
		},
		{
			name:      "prefix /co matches compact",
			prefix:    "/co",
			wantCount: 1,
			wantNames: []string{"/compact"},
		},
		{
			name:      "related word new matches clear",
			prefix:    "/new",
			wantCount: 1,
			wantNames: []string{"/clear"},
		},
		{
			name:      "matching ignores case",
			prefix:    "/COM",
			wantCount: 1,
			wantNames: []string{"/compact"},
		},
		{
			name:      "name prefixes rank before related word matches",
			prefix:    "/c",
			wantCount: 3,
			wantNames: []string{"/clear", "/compact", "/exit"},
		},
		{
			name:      "command-name contains ranks last",
			prefix:    "/ear",
			wantCount: 1,
			wantNames: []string{"/clear"},
		},
		{
			name:    "prefix /zzz matches nothing",
			prefix:  "/zzz",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := NewSlashComplete(tt.prefix)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("NewSlashComplete(%q) = %+v, want nil", tt.prefix, got)
				}
				return
			}

			if got == nil {
				t.Fatalf("NewSlashComplete(%q) = nil, want %d items", tt.prefix, tt.wantCount)
			}
			if len(got.items) != tt.wantCount {
				t.Fatalf("NewSlashComplete(%q) item count = %d, want %d", tt.prefix, len(got.items), tt.wantCount)
			}
			for i, name := range tt.wantNames {
				if got.items[i].Name != name {
					t.Errorf("NewSlashComplete(%q) item[%d].Name = %q, want %q", tt.prefix, i, got.items[i].Name, name)
				}
			}
		})
	}
}

func TestSlashMatchRankNormalizesRelatedWords(t *testing.T) {
	original := slashRelatedWords["/clear"]
	slashRelatedWords["/clear"] = []string{"NEW"}
	t.Cleanup(func() { slashRelatedWords["/clear"] = original })

	got := NewSlashComplete("/new")
	if got == nil || got.Selected().Name != "/clear" {
		t.Fatalf("NewSlashComplete(\"/new\") = %#v, want /clear from case-insensitive related word", got)
	}
}

func TestSlashCompleteSelected(t *testing.T) {
	t.Parallel()

	s := NewSlashComplete("/")
	if s == nil {
		t.Fatal("NewSlashComplete(\"/\") = nil, want non-nil")
	}

	if got := s.Selected(); got.Name != "/clear" {
		t.Errorf("Selected() = %q, want first match %q", got.Name, "/clear")
	}
}

func TestSlashCompleteCursorWrap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		moves    []func(*SlashComplete)
		wantName string
	}{
		{
			name:     "no move stays on first",
			moves:    nil,
			wantName: "/clear",
		},
		{
			name:     "down moves to second",
			moves:    []func(*SlashComplete){(*SlashComplete).Down},
			wantName: "/compact",
		},
		{
			name:     "down twice moves to third",
			moves:    []func(*SlashComplete){(*SlashComplete).Down, (*SlashComplete).Down},
			wantName: "/exit",
		},
		{
			name:     "down three times wraps to first",
			moves:    []func(*SlashComplete){(*SlashComplete).Down, (*SlashComplete).Down, (*SlashComplete).Down},
			wantName: "/clear",
		},
		{
			name:     "up wraps to last",
			moves:    []func(*SlashComplete){(*SlashComplete).Up},
			wantName: "/exit",
		},
		{
			name:     "up twice from first moves to second",
			moves:    []func(*SlashComplete){(*SlashComplete).Up, (*SlashComplete).Up},
			wantName: "/compact",
		},
		{
			name:     "up three times from first wraps to first",
			moves:    []func(*SlashComplete){(*SlashComplete).Up, (*SlashComplete).Up, (*SlashComplete).Up},
			wantName: "/clear",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := NewSlashComplete("/")
			if s == nil {
				t.Fatal("NewSlashComplete(\"/\") = nil, want non-nil")
			}
			for _, move := range tt.moves {
				move(s)
			}
			if got := s.Selected(); got.Name != tt.wantName {
				t.Errorf("Selected().Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestSlashCompleteView(t *testing.T) {
	t.Parallel()

	s := NewSlashComplete("/")
	if s == nil {
		t.Fatal("NewSlashComplete(\"/\") = nil, want non-nil")
	}

	view := s.View()
	if view == "" {
		t.Fatal("View() = empty, want non-empty")
	}
	for _, want := range []string{"/clear", "/exit"} {
		if !strings.Contains(view, want) {
			t.Errorf("View() = %q, want substring %q", view, want)
		}
	}
}

func TestSlashCompleteViewWidthRendersContinuousTrayRail(t *testing.T) {
	t.Parallel()

	const width = 48
	s := NewSlashComplete("/")
	if s == nil {
		t.Fatal("NewSlashComplete(\"/\") = nil, want non-nil")
	}

	type widthViewer interface {
		ViewWidth(int) string
	}
	viewer, ok := any(s).(widthViewer)
	if !ok {
		t.Fatal("SlashComplete does not implement ViewWidth(int)")
	}
	lines := strings.Split(viewer.ViewWidth(width), "\n")
	if len(lines) != len(SlashCommands) {
		t.Fatalf("ViewWidth() row count = %d, want %d", len(lines), len(SlashCommands))
	}

	panelOpen, _ := styles.DeriveBackgroundSGR(styles.PanelBg)
	selectedOpen, _ := styles.DeriveBackgroundSGR(styles.TraySelectedBg)
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

	if !strings.HasPrefix(lines[0], selectedOpen) {
		t.Errorf("selected row does not open with TraySelectedBg: %q", lines[0])
	}
	if !strings.Contains(lines[0], strings.TrimSuffix(styles.CardRailStyle.Render(styles.AccentBar), "\x1b[m")) {
		t.Errorf("selected row does not use CardRailStyle: %q", lines[0])
	}
	selectedLabel := styles.CardSelectedStyle.Background(styles.TraySelectedBg).Render("/clear")
	if !strings.Contains(lines[0], strings.TrimSuffix(selectedLabel, "\x1b[m")) {
		t.Errorf("selected primary label is not rendered with CardSelectedStyle: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], panelOpen) {
		t.Errorf("unselected row does not open with PanelBg: %q", lines[1])
	}
	if !strings.Contains(lines[1], strings.TrimSuffix(styles.AccentBarStyle.Render(styles.AccentBar), "\x1b[m")) {
		t.Errorf("unselected row does not use AccentBarStyle: %q", lines[1])
	}
	if !strings.Contains(lines[1], strings.TrimSuffix(styles.CardHintStyle.Render("compact the current conversation"), "\x1b[m")) {
		t.Errorf("unselected description is not faint: %q", lines[1])
	}
}

func TestSlashCompleteViewWindowKeepsSelectionVisible(t *testing.T) {
	t.Parallel()

	s := NewSlashComplete("/")
	if s == nil {
		t.Fatal("NewSlashComplete(\"/\") = nil, want non-nil")
	}
	s.Down()
	s.Down() // /exit is beyond the initial two-row window.

	view := s.ViewWindow(40, 2)
	if got := lipgloss.Height(view); got != 2 {
		t.Fatalf("ViewWindow() height = %d, want 2; view=%q", got, view)
	}
	if !strings.Contains(stripANSI(view), "/exit") {
		t.Errorf("ViewWindow() = %q, want selected /exit visible", view)
	}
	selectedOpen, _ := styles.DeriveBackgroundSGR(styles.TraySelectedBg)
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(stripANSI(line), "/exit") && !strings.HasPrefix(line, selectedOpen) {
			t.Errorf("visible /exit row is not selected: %q", line)
		}
	}
	if got := s.ViewWindow(40, 0); got != "" {
		t.Errorf("ViewWindow(maxRows=0) = %q, want empty", got)
	}
}

func TestSlashCompleteViewWindowBackgroundUsesProvidedSelectionFill(t *testing.T) {
	t.Parallel()

	s := NewSlashComplete("/")
	bg := lipgloss.Color("#112233")
	view := s.ViewWindowBackground(40, 3, bg)
	line := strings.Split(view, "\n")[0]
	open, _ := styles.DeriveBackgroundSGR(bg)
	if !strings.HasPrefix(line, open) {
		t.Errorf("selected row does not open with provided background: %q", line)
	}
	wantLabel := lipgloss.NewStyle().Bold(true).Background(bg).Render("/clear")
	if !strings.Contains(line, strings.TrimSuffix(wantLabel, "\x1b[m")) {
		t.Errorf("selected label does not use provided background: %q", line)
	}
}

func TestSlashCompleteViewWidthClampsANSISafely(t *testing.T) {
	t.Parallel()

	const width = 11
	s := &SlashComplete{items: []SlashCmd{{Name: "/界界界界", Desc: "a long description"}}}
	type widthViewer interface {
		ViewWidth(int) string
	}
	viewer, ok := any(s).(widthViewer)
	if !ok {
		t.Fatal("SlashComplete does not implement ViewWidth(int)")
	}
	view := viewer.ViewWidth(width)
	if got := lipgloss.Width(view); got != width {
		t.Fatalf("ViewWidth() width = %d, want %d; view=%q", got, width, view)
	}
	if strings.ContainsRune(stripANSI(view), '\uFFFD') {
		t.Fatalf("ViewWidth() split a Unicode sequence: %q", view)
	}
}
