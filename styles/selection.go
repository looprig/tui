package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
)

// selectionBg is THE selection fill, to be shared by the completion tray, the AskUser
// choice card and the permission card as each migrates onto SelectedRow. One value, so a
// selected row can never read differently between two surfaces.
//
// It is CardBorderColor (#A2D2FF) — the SAME token CardRailStyle, CardKeyStyle and
// WorkflowActivityStyle resolve to. styles/card.go says that token exists so there is one
// semantic blue token rather than drifting shades; TraySelectedBg #3A526B was one of the
// drifting shades and is retired from selection use.
//
// Unexported deliberately: nothing outside this file needs to write it, and an exported
// mutable var would be a seam for exactly the drift SelectedRow exists to prevent. Add a
// read accessor if a consumer ever genuinely needs the value.
var selectionBg color.Color = CardBorderColor

// selectionOnLight is the foreground forced onto a selected row when the fill is light.
var selectionOnLight = lipgloss.NewStyle().Foreground(lipgloss.Color("#101010"))

// selectionIsDark reports whether c is dark enough for inner styling (the bold blue
// accelerator, faint secondaries) to stay legible on top of it. It is DERIVED from the
// color rather than stored alongside it, so the answer can never contradict the fill it is
// asked about. Rec. 601 luma against the midpoint of the 16-bit component range; the
// numerator maxes out at 65_535_000, well inside uint32. A degenerate color (RGBA all
// zero, e.g. lipgloss.NoColor) reports dark, which is also the safe answer: inner styling
// is preserved rather than flattened.
func selectionIsDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return (299*r+587*g+114*b)/1000 < 0x8000
}

// SelectedRow bands row with the shared selection fill across width columns. There is
// deliberately NO cursor glyph — the band is the cursor.
//
// It takes the row in ONE form. On a dark fill the row keeps its own styling
// (FillLineBackgroundWith re-opens the fill after every inner SGR reset); on a light fill
// the styling would wash out, so the row is stripped and re-rendered near-black. The
// bold-blue accelerator therefore loses its blue on the SELECTED row only — accepted, since
// against an identical fill it would be invisible anyway. Callers pass one string and so
// cannot get a styled and a plain form out of sync.
//
// It PADS to width but never TRUNCATES: a row wider than width comes back whole. Cutting a
// row to the surface width is the caller's job (the tray ansi.Truncates first), because only
// the caller knows what a sensible tail looks like.
//
// If the fill derives no SGR (a degenerate or nil color) there is no band to paint and the
// row is returned untouched — no fill, but still legible. Restyling it near-black without a
// band behind it would render it black-on-black.
func SelectedRow(row string, width int) string {
	open, reset := DeriveBackgroundSGR(selectionBg)
	if open == "" {
		return row
	}
	body := row
	if !selectionIsDark(selectionBg) {
		body = selectionOnLight.Render(xansi.Strip(row))
	}
	return FillLineBackgroundWith(body, width, open, reset)
}
