package styles

import (
	"image/color"
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// selectionNearBlack pins, independently of the implementation, the foreground a light
// selection fill is expected to force onto the row.
const selectionNearBlack = "#101010"

// setSelectionBgForTest swaps the package-level selection fill and returns the restore
// func. It exists so the fill stays unexported: nothing in production writes it, so it
// must not be an exported mutable var just to give tests a seam.
func setSelectionBgForTest(c color.Color) func() {
	prev := selectionBg
	selectionBg = c
	return func() { selectionBg = prev }
}

// openingSGR returns the escape sequence a style emits BEFORE its content, derived by
// rendering a NUL sentinel and splitting on it (the same trick DeriveBackgroundSGR uses),
// so no test hardcodes an escape.
func openingSGR(t *testing.T, s lipgloss.Style) string {
	t.Helper()
	rendered := s.Render("\x00")
	i := strings.IndexByte(rendered, 0)
	if i <= 0 {
		t.Fatalf("style emitted no opening SGR (rendered %q)", rendered)
	}
	return rendered[:i]
}

// nearBlackSGR is the opening escape of the foreground a light fill forces onto a row.
func nearBlackSGR(t *testing.T) string {
	t.Helper()
	return openingSGR(t, lipgloss.NewStyle().Foreground(lipgloss.Color(selectionNearBlack)))
}

// completeSGR matches a whole SGR escape sequence. Stripping every match must leave no
// stray ESC behind — a leftover means a truncated/broken sequence would reach the terminal.
var completeSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// assertNoCursorGlyph fails if the output carries a cursor glyph. The band IS the cursor,
// so this must hold on EVERY branch of SelectedRow — the light branch rebuilds the body and
// would silently swallow a glyph the dark branch keeps.
func assertNoCursorGlyph(t *testing.T, got string) {
	t.Helper()
	for _, glyph := range []string{"▸", "▶", ">"} {
		if strings.Contains(got, glyph) {
			t.Errorf("selected row contains cursor glyph %q; got %q", glyph, got)
		}
	}
}

// assertWellFormedEscapes fails if the output carries a truncated escape sequence.
func assertWellFormedEscapes(t *testing.T, got string) {
	t.Helper()
	if rest := completeSGR.ReplaceAllString(got, ""); strings.ContainsRune(rest, '\x1b') {
		t.Errorf("output carries a broken escape sequence; residue = %q, full = %q", rest, got)
	}
}

// TestSelectionBgIsTheOneSemanticBlue pins the whole point of this file: the selection fill
// IS CardBorderColor, the single semantic blue token. If someone re-points it at a private
// per-surface shade (the tray's retired #3A526B was exactly that), selection drifts between
// surfaces again.
func TestSelectionBgIsTheOneSemanticBlue(t *testing.T) {
	t.Parallel()

	if selectionBg != color.Color(CardBorderColor) {
		t.Errorf("selectionBg = %v, want CardBorderColor %v", selectionBg, CardBorderColor)
	}
}

// TestCurrentChoiceStyleUsesTheSemanticBlue keeps the persistent active-value marker on
// the same blue token as the tray rail and selection family rather than introducing a
// private near-match.
func TestCurrentChoiceStyleUsesTheSemanticBlue(t *testing.T) {
	t.Parallel()

	const text = "current"
	want := lipgloss.NewStyle().Foreground(CardBorderColor).Render(text)
	if got := CurrentChoiceStyle.Render(text); got != want {
		t.Errorf("CurrentChoiceStyle = %q, want semantic blue %q", got, want)
	}
}

// TestSelectionIsDark pins the derivation that replaced the stored SelectionIsDark flag: the
// answer is computed FROM the fill, so it can never contradict the fill it describes.
func TestSelectionIsDark(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		bg   color.Color
		want bool
	}{
		{name: "CardBorderColor #A2D2FF is light", bg: CardBorderColor, want: false},
		{name: "white is light", bg: color.White, want: false},
		{name: "a dark navy is dark", bg: lipgloss.Color("#1F2A44"), want: true},
		{name: "the tray's retired private fill is dark", bg: lipgloss.Color("#3A526B"), want: true},
		{name: "black is dark", bg: color.Black, want: true},
		{name: "degenerate NoColor reports dark (preserves inner styling)", bg: lipgloss.NoColor{}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := selectionIsDark(tt.bg); got != tt.want {
				t.Errorf("selectionIsDark(%v) = %v, want %v", tt.bg, got, tt.want)
			}
		})
	}
}

// TestSelectedRowBandsToWidth pins the two things that make the band THE selection
// affordance: it spans the full requested width (so the fill reads as a bar, not a tint
// around the text), and it carries NO cursor glyph — the band is the cursor.
func TestSelectedRowBandsToWidth(t *testing.T) {
	t.Parallel()

	got := SelectedRow("hello", 20)
	if w := lipgloss.Width(got); w != 20 {
		t.Errorf("width = %d, want 20", w)
	}
	assertNoCursorGlyph(t, got)
}

// TestSelectedRowLightFillStripsInnerStyling pins the configured default: the fill
// (CardBorderColor #A2D2FF) is LIGHT, so inner styling would wash out on it. SelectedRow
// STRIPS the row's own styling and re-renders it near-black — the caller passes one string
// and cannot get the two forms out of sync.
func TestSelectedRowLightFillStripsInnerStyling(t *testing.T) {
	t.Parallel()

	if selectionIsDark(selectionBg) {
		t.Fatal("configured selectionBg is dark, want light: this test pins the light branch")
	}

	// Two DIFFERENT inner styles, so the assertions pin that stripping happened rather
	// than that one particular escape is absent.
	const plain = "[1] Approve once"
	row := CardKeyStyle.Render("[1]") + " " + CardHintStyle.Render("Approve once")

	got := SelectedRow(row, 30)

	for _, style := range []struct {
		name string
		s    lipgloss.Style
	}{{"accelerator", CardKeyStyle}, {"hint", CardHintStyle}} {
		if sgr := openingSGR(t, style.s); strings.Contains(got, sgr) {
			t.Errorf("light fill kept the %s's inner SGR %q; got %q", style.name, sgr, got)
		}
	}
	if !strings.Contains(got, nearBlackSGR(t)) {
		t.Errorf("light fill did not re-render the row near-black; got %q", got)
	}
	fillSGR, _ := DeriveBackgroundSGR(selectionBg)
	if !strings.HasPrefix(got, fillSGR) {
		t.Errorf("row does not open with the selection fill %q; got %q", fillSGR, got)
	}
	if visible := strings.TrimRight(stripANSI(got), " "); visible != plain {
		t.Errorf("visible text = %q, want %q", visible, plain)
	}
	assertNoCursorGlyph(t, got)
	assertWellFormedEscapes(t, got)
}

// TestSelectedRowDarkFillKeepsInnerStyling pins the other branch: when the fill is dark
// enough for inner styling to stay legible, the row is banded AS-IS and its inner SGR
// survives (FillLineBackgroundWith re-opens the fill after every inner reset).
//
// Not parallel: it swaps the package-level fill. Go defers parallel tests until every
// sequential top-level test has finished, so the swap cannot overlap with them, and the
// restore runs via t.Cleanup either way.
func TestSelectedRowDarkFillKeepsInnerStyling(t *testing.T) {
	dark := lipgloss.Color("#1F2A44")
	t.Cleanup(setSelectionBgForTest(dark))

	acceleratorSGR := openingSGR(t, CardKeyStyle)
	row := CardKeyStyle.Render("[1]") + " Approve once"

	got := SelectedRow(row, 30)

	if !strings.Contains(got, acceleratorSGR) {
		t.Errorf("dark fill lost the accelerator's inner SGR %q; got %q", acceleratorSGR, got)
	}
	if strings.Contains(got, nearBlackSGR(t)) {
		t.Errorf("dark fill wrongly forced the near-black light-fill foreground; got %q", got)
	}
	fillSGR, _ := DeriveBackgroundSGR(dark)
	if !strings.HasPrefix(got, fillSGR) {
		t.Errorf("row does not open with the dark selection fill %q; got %q", fillSGR, got)
	}
	if !strings.Contains(got, "\x1b[m"+fillSGR) && !strings.Contains(got, "\x1b[0m"+fillSGR) {
		t.Errorf("dark fill was not re-opened after the accelerator's inner reset; got %q", got)
	}
	if w := lipgloss.Width(got); w != 30 {
		t.Errorf("width = %d, want 30", w)
	}
	assertNoCursorGlyph(t, got)
	assertWellFormedEscapes(t, got)
}

// TestSelectedRowBoundaryWidths pins the degenerate widths: zero, negative and a width
// narrower than the content must not panic and must not emit a broken escape. None of them
// truncate — SelectedRow pads but never cuts, so a row wider than the request comes back
// whole (truncating to the surface width is the caller's job).
func TestSelectedRowBoundaryWidths(t *testing.T) {
	t.Parallel()

	const plain = "hello" // 5 display columns

	tests := []struct {
		name      string
		width     int
		wantWidth int
	}{
		{name: "zero width", width: 0, wantWidth: 5},
		{name: "negative width", width: -4, wantWidth: 5},
		{name: "width narrower than content does not truncate", width: 3, wantWidth: 5},
		{name: "width exactly content", width: 5, wantWidth: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SelectedRow(plain, tt.width)
			if w := lipgloss.Width(got); w != tt.wantWidth {
				t.Errorf("width = %d, want %d; got %q", w, tt.wantWidth, got)
			}
			if visible := stripANSI(got); visible != plain {
				t.Errorf("visible text = %q, want %q", visible, plain)
			}
			assertWellFormedEscapes(t, got)
		})
	}
}

// TestSelectedRowDegenerateFillReturnsRowUnchanged pins the fail-safe. When the fill derives
// no SGR at all there is no band to paint, so the row must come back EXACTLY as it went in,
// still carrying its own styling. It must NOT be re-rendered near-black: a #101010
// foreground with no background behind it is black-on-black on a dark terminal, which is
// strictly worse than doing nothing.
//
// Not parallel: swaps the package-level fill (see the dark-fill test).
func TestSelectedRowDegenerateFillReturnsRowUnchanged(t *testing.T) {
	t.Cleanup(setSelectionBgForTest(lipgloss.NoColor{}))

	if open, _ := DeriveBackgroundSGR(selectionBg); open != "" {
		t.Fatalf("DeriveBackgroundSGR(NoColor) open = %q, want %q (test premise)", open, "")
	}

	row := CardKeyStyle.Render("[1]") + " Approve once"
	got := SelectedRow(row, 30)

	if got != row {
		t.Errorf("degenerate fill = %q, want the row unchanged %q", got, row)
	}
	if sgr := nearBlackSGR(t); strings.Contains(got, sgr) {
		t.Errorf("degenerate fill forced the near-black foreground %q with no band behind it "+
			"(black-on-black); got %q", sgr, got)
	}
	assertNoCursorGlyph(t, got)
	assertWellFormedEscapes(t, got)
}

// TestSelectedRowNilFillReturnsRowUnchanged pins that a nil fill takes the same fail-safe
// path as a degenerate one, rather than panicking in the darkness derivation.
//
// Not parallel: swaps the package-level fill (see the dark-fill test).
func TestSelectedRowNilFillReturnsRowUnchanged(t *testing.T) {
	t.Cleanup(setSelectionBgForTest(nil))

	row := CardKeyStyle.Render("[1]") + " Approve once"
	if got := SelectedRow(row, 30); got != row {
		t.Errorf("nil fill = %q, want the row unchanged %q", got, row)
	}
}

// TestSelectedRowWithRailKeepsTheRailInTheFillColor pins the fix for a rail that read as a
// second cursor: on the light fill the row's own styling is stripped to near-black, and a
// rail carried inside that row went black with it — a dark mark down the left of the band.
// The rail is drawn in the FILL's color instead, so it disappears into the band while the
// panel's edge stays continuous on the rows above and below.
func TestSelectedRowWithRailKeepsTheRailInTheFillColor(t *testing.T) {
	t.Parallel()

	if selectionIsDark(selectionBg) {
		t.Fatal("configured selectionBg is dark, want light: this test pins the light branch")
	}

	const rail = "▌"
	got := SelectedRowWithRail(AccentBarStyle.Render(rail), " opus-5", 20)

	fillFG := openingSGR(t, lipgloss.NewStyle().Foreground(selectionBg))
	if !strings.Contains(got, fillFG+rail) {
		t.Errorf("rail is not drawn in the fill's color %q; got %q", fillFG, got)
	}
	if sgr := nearBlackSGR(t); strings.Contains(got, sgr+rail) {
		t.Errorf("rail was re-rendered near-black like the body; got %q", got)
	}
	if sgr := openingSGR(t, AccentBarStyle); strings.Contains(got, sgr) {
		t.Errorf("rail kept its own unselected color %q on the band; got %q", sgr, got)
	}
	if w := lipgloss.Width(got); w != 20 {
		t.Errorf("width = %d, want 20 (the rail counts toward the band)", w)
	}
	if visible := strings.TrimRight(stripANSI(got), " "); visible != rail+" opus-5" {
		t.Errorf("visible text = %q, want %q", visible, rail+" opus-5")
	}
	assertNoCursorGlyph(t, got)
	assertWellFormedEscapes(t, got)
}

// TestSelectedRowIsTheRaillessCase pins that SelectedRow is SelectedRowWithRail's empty-rail
// form rather than a second implementation that could drift from it.
func TestSelectedRowIsTheRaillessCase(t *testing.T) {
	t.Parallel()

	row := CardKeyStyle.Render("[1]") + " Approve once"
	if got, want := SelectedRow(row, 30), SelectedRowWithRail("", row, 30); got != want {
		t.Errorf("SelectedRow = %q, want the empty-rail form %q", got, want)
	}
}
