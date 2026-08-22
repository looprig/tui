package styles

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// selectionNearBlack pins, independently of the implementation, the foreground a light
// selection fill is expected to force onto the row.
const selectionNearBlack = "#101010"

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

// completeSGR matches a whole SGR escape sequence. Stripping every match must leave no
// stray ESC behind — a leftover means a truncated/broken sequence would reach the terminal.
var completeSGR = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// assertWellFormedEscapes fails if the output carries a truncated escape sequence.
func assertWellFormedEscapes(t *testing.T, got string) {
	t.Helper()
	if rest := completeSGR.ReplaceAllString(got, ""); strings.ContainsRune(rest, '\x1b') {
		t.Errorf("output carries a broken escape sequence; residue = %q, full = %q", rest, got)
	}
}

// TestSelectedRowBandsToWidth pins the two things that make the band THE selection
// affordance: it spans the full requested width (so the fill reads as a bar, not a tint
// around the text), and it carries NO cursor glyph — the band is the cursor.
func TestSelectedRowBandsToWidth(t *testing.T) {
	t.Parallel()

	got := SelectedRow("hello", "hello", 20)
	if w := lipgloss.Width(got); w != 20 {
		t.Errorf("width = %d, want 20", w)
	}
	for _, glyph := range []string{"▸", "▶", ">"} {
		if strings.Contains(got, glyph) {
			t.Errorf("selected row contains cursor glyph %q", glyph)
		}
	}
}

// TestSelectedRowLightFillRestylesPlain pins the configured default: SelectionBg
// (CardBorderColor #A2D2FF) is LIGHT, so inner styling would wash out on it. The PLAIN
// argument is what reaches the output, re-rendered near-black; the pre-styled argument's
// inner SGR (here the bold-blue accelerator) must NOT survive.
func TestSelectedRowLightFillRestylesPlain(t *testing.T) {
	t.Parallel()

	if SelectionIsDark {
		t.Fatal("SelectionIsDark = true, want false: the configured fill is light")
	}

	const plain = "[1] Approve once"
	styled := CardKeyStyle.Render("[1]") + " Approve once"

	got := SelectedRow(styled, plain, 30)

	acceleratorSGR := openingSGR(t, CardKeyStyle)
	if strings.Contains(got, acceleratorSGR) {
		t.Errorf("light fill kept the accelerator's inner SGR %q; got %q", acceleratorSGR, got)
	}
	nearBlackSGR := openingSGR(t, lipgloss.NewStyle().Foreground(lipgloss.Color(selectionNearBlack)))
	if !strings.Contains(got, nearBlackSGR) {
		t.Errorf("light fill did not re-render the row near-black (%q); got %q", nearBlackSGR, got)
	}
	fillSGR, _ := DeriveBackgroundSGR(SelectionBg)
	if !strings.HasPrefix(got, fillSGR) {
		t.Errorf("row does not open with the selection fill %q; got %q", fillSGR, got)
	}
	if visible := stripANSI(got); !strings.HasPrefix(visible, plain) {
		t.Errorf("visible text = %q, want it to start with the plain row %q", visible, plain)
	}
	assertWellFormedEscapes(t, got)
}

// TestSelectedRowDarkFillKeepsInnerStyling pins the other branch: when the fill is dark
// enough for inner styling to stay legible, the already-STYLED argument is banded as-is and
// its inner SGR survives (FillLineBackgroundWith re-opens the fill after every inner reset).
//
// Not parallel: it mutates the package-level selection vars. Go defers parallel tests until
// every sequential top-level test has finished, so the mutation cannot overlap with them,
// and t.Cleanup restores the defaults either way.
func TestSelectedRowDarkFillKeepsInnerStyling(t *testing.T) {
	prevBg, prevDark := SelectionBg, SelectionIsDark
	t.Cleanup(func() { SelectionBg, SelectionIsDark = prevBg, prevDark })
	SelectionBg = lipgloss.Color("#1F2A44")
	SelectionIsDark = true

	const plain = "[1] Approve once"
	acceleratorSGR := openingSGR(t, CardKeyStyle)
	styled := CardKeyStyle.Render("[1]") + " Approve once"

	got := SelectedRow(styled, plain, 30)

	if !strings.Contains(got, acceleratorSGR) {
		t.Errorf("dark fill lost the accelerator's inner SGR %q; got %q", acceleratorSGR, got)
	}
	fillSGR, _ := DeriveBackgroundSGR(SelectionBg)
	if !strings.HasPrefix(got, fillSGR) {
		t.Errorf("row does not open with the dark selection fill %q; got %q", fillSGR, got)
	}
	if !strings.Contains(got, "\x1b[m"+fillSGR) && !strings.Contains(got, "\x1b[0m"+fillSGR) {
		t.Errorf("dark fill was not re-opened after the accelerator's inner reset; got %q", got)
	}
	if w := lipgloss.Width(got); w != 30 {
		t.Errorf("width = %d, want 30", w)
	}
	assertWellFormedEscapes(t, got)
}

// TestSelectedRowBoundaryWidths pins the degenerate widths: zero and a width narrower than
// the content must not panic and must not emit a broken escape. Neither pads — the band
// still spans the content it has.
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
		{name: "width narrower than content", width: 3, wantWidth: 5},
		{name: "width exactly content", width: 5, wantWidth: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := SelectedRow(plain, plain, tt.width)
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

// TestSelectedRowDegenerateFillReturnsBodyUnchanged pins the fail-safe: when the fill color
// derives no SGR at all, FillLineBackgroundWith returns the body untouched rather than
// emitting a half-formed escape. The row still renders — just without a band.
//
// Not parallel: mutates the package-level selection vars (see the dark-fill test).
func TestSelectedRowDegenerateFillReturnsBodyUnchanged(t *testing.T) {
	prevBg := SelectionBg
	t.Cleanup(func() { SelectionBg = prevBg })
	SelectionBg = lipgloss.NoColor{}

	if open, _ := DeriveBackgroundSGR(SelectionBg); open != "" {
		t.Fatalf("DeriveBackgroundSGR(NoColor) open = %q, want %q (test premise)", open, "")
	}

	const plain = "hello"
	got := SelectedRow(plain, plain, 20)

	nearBlack := lipgloss.NewStyle().Foreground(lipgloss.Color(selectionNearBlack))
	if want := nearBlack.Render(plain); got != want {
		t.Errorf("degenerate fill = %q, want the unfilled near-black body %q", got, want)
	}
	assertWellFormedEscapes(t, got)
}
