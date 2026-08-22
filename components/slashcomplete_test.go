package components

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/looprig/tui/styles"
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
			name:      "prefix /cl matches only clear",
			prefix:    "/cl",
			wantCount: 1,
			wantNames: []string{"/clear"},
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
			// Related words are gone: the filter reads command NAMES only, so a synonym
			// that appears in no name no longer opens the tray.
			name:    "synonym of a command matches nothing",
			prefix:  "/new",
			wantNil: true,
		},
		{
			name:      "matching ignores case",
			prefix:    "/COM",
			wantCount: 1,
			wantNames: []string{"/compact"},
		},
		{
			name:      "single letter matches every name containing it",
			prefix:    "/c",
			wantCount: 2,
			wantNames: []string{"/clear", "/compact"},
		},
		{
			name:      "a run inside a name still matches",
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
			names := slashNames(t, got)
			if len(names) != tt.wantCount {
				t.Fatalf("NewSlashComplete(%q) item count = %d, want %d", tt.prefix, len(names), tt.wantCount)
			}
			for i, name := range tt.wantNames {
				if names[i] != name {
					t.Errorf("NewSlashComplete(%q) item[%d].Name = %q, want %q", tt.prefix, i, names[i], name)
				}
			}
		})
	}
}

func TestNewSlashCompleteWithCommandsUsesInjectedCatalog(t *testing.T) {
	commands := []SlashCmd{{Name: "/mode", Desc: "change mode"}}
	tray := NewSlashCompleteWithCommands("/mo", commands)
	if tray == nil || tray.Selected().Name != "/mode" {
		t.Fatalf("selected = %#v, want injected /mode", tray)
	}
	commands[0].Name = "/mutated"
	if tray.Selected().Name != "/mode" {
		t.Fatal("live tray retained caller command slice")
	}
	if NewSlashCompleteWithCommands("/clear", commands) != nil {
		t.Fatal("injected catalog unexpectedly fell back to static commands")
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

	// The selected row is the shared band and nothing else: no tray-local fill, no
	// highlighted rail, no bold label. Those were three ways for this surface to drift.
	if !strings.HasPrefix(lines[0], selectedBandOpen(t)) {
		t.Errorf("selected row does not open with the shared selection band: %q", lines[0])
	}
	if strings.Contains(lines[0], strings.TrimSuffix(styles.CardRailStyle.Render(styles.AccentBar), "\x1b[m")) {
		t.Errorf("selected row still paints its own rail color: %q", lines[0])
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
	selectedOpen := selectedBandOpen(t)
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(stripANSI(line), "/exit") && !strings.HasPrefix(line, selectedOpen) {
			t.Errorf("visible /exit row is not selected: %q", line)
		}
	}
	if got := s.ViewWindow(40, 0); got != "" {
		t.Errorf("ViewWindow(maxRows=0) = %q, want empty", got)
	}
}

func TestSlashCompleteViewWidthClampsANSISafely(t *testing.T) {
	t.Parallel()

	const width = 11
	s := NewSlashCompleteWithCommands("", []SlashCmd{{Name: "/界界界界", Desc: "a long description"}})
	if s == nil {
		t.Fatal(`NewSlashCompleteWithCommands("", one command) = nil, want the whole catalog`)
	}
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

// slashNames is every matched command in render order, read through the exported API alone.
// The filtered items belong to the list engine now, so the walk uses the wrapping cursor:
// Down from the last row returns to the first, which is where the walk stops -- and where a
// freshly built panel started, so the walk leaves the cursor as it found it.
func slashNames(t *testing.T, s *SlashComplete) []string {
	t.Helper()

	var names []string
	for {
		names = append(names, s.Selected().Name)
		s.Down()
		if s.Cursor() == 0 {
			return names
		}
		if len(names) > 1000 {
			t.Fatal("cursor never wrapped back to the first row")
		}
	}
}

// The old rank ladder was prefix/substring only, so a subsequence query found nothing.
// /sandbox is a fixture rather than a real command: the catalog is production data, and a
// command nobody can run has no business in it just to make a test read well.
func TestFuzzyFindsSubsequence(t *testing.T) {
	t.Parallel()

	commands := []SlashCmd{
		{Name: "/clear", Desc: "start a new conversation"},
		{Name: "/sandbox", Desc: "change the sandbox policy"},
	}
	s := NewSlashCompleteWithCommands("sbx", commands)
	if s == nil {
		t.Fatal(`fuzzy filter found no match for "sbx", want /sandbox`)
	}
	if got := s.Selected().Name; got != "/sandbox" {
		t.Errorf("Selected() = %q, want /sandbox", got)
	}
}

// TestSlashCompleteSelectWindowRowSlidesAndPins covers the half of the migration the list
// engine cannot do for itself. list.Model paginates: with a two-row window and the cursor on
// the fifth of five commands it considers rows 4-5 to be a page of their own, so its own
// SelectWindowRow would map the window's top row to the cursor's own item and report no
// change. The tray SLIDES, so that top row is the FOURTH command.
//
// The second half is the pin: having pointed at a row, the window must hold still. A window
// re-derived from the new cursor would scroll up by one and slide a different command under
// the motionless pointer.
func TestSlashCompleteSelectWindowRowSlidesAndPins(t *testing.T) {
	t.Parallel()

	commands := []SlashCmd{
		{Name: "/one", Desc: "first"},
		{Name: "/two", Desc: "second"},
		{Name: "/three", Desc: "third"},
		{Name: "/four", Desc: "fourth"},
		{Name: "/five", Desc: "fifth"},
	}
	s := NewSlashCompleteWithCommands("", commands)
	if s == nil {
		t.Fatal("NewSlashCompleteWithCommands with no query = nil, want the whole catalog")
	}
	s.Up() // wrap to the last command, scrolling the window to the bottom

	if !s.SelectWindowRow(0, 2) {
		t.Fatal("SelectWindowRow(top row of the scrolled window) reported no change, want a move")
	}
	if got := s.Selected().Name; got != "/four" {
		t.Errorf("selected = %q, want %q: the window slides, it does not page", got, "/four")
	}

	view := stripANSI(s.ViewWindow(40, 2))
	if !strings.Contains(view, "/five") {
		t.Errorf("window after pointing at its top row = %q, want it pinned with /five still below", view)
	}
	if strings.Contains(view, "/three") {
		t.Errorf("window after pointing at its top row = %q, want it pinned, not scrolled up onto /three", view)
	}
}

// TestSlashCompleteViewIsAsWideAsItsMatches pins where the natural width is measured. The
// tray is only ever as wide as the rows it draws, so a long command the filter removed must
// not leave its width behind as a band of empty fill.
func TestSlashCompleteViewIsAsWideAsItsMatches(t *testing.T) {
	t.Parallel()

	match := completionTrayRow{primary: "/clear", secondary: "start a new conversation"}
	commands := []SlashCmd{
		{Name: match.primary, Desc: match.secondary},
		{Name: "/zzzzzzzzzzzzzzzzzzzz", Desc: "a much wider row that the filter drops"},
	}
	s := NewSlashCompleteWithCommands("clear", commands)
	if s == nil {
		t.Fatal(`NewSlashCompleteWithCommands("clear", ...) = nil, want /clear`)
	}

	want := completionTrayNaturalWidth([]completionTrayRow{match})
	if got := lipgloss.Width(s.View()); got != want {
		t.Errorf("View() width = %d, want %d: the width of the matched rows alone", got, want)
	}
}
