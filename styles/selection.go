package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// SelectionBg is THE selection fill, shared by the completion tray, the AskUser choice
// card and the permission card. One value, so a selected row can never read differently
// between two surfaces.
//
// It is CardBorderColor (#A2D2FF) — the SAME token CardRailStyle, CardKeyStyle and
// WorkflowActivityStyle resolve to. styles/card.go says that token exists so there is
// "one semantic blue rather than drifting shades"; TraySelectedBg #3A526B was one of the
// drifting shades and is retired from selection use.
var SelectionBg color.Color = CardBorderColor

// SelectionIsDark reports whether SelectionBg is dark enough for inner styling (the bold
// blue accelerator, faint secondaries) to stay legible on top of it. CardBorderColor is
// LIGHT, so SelectedRow renders the plain text near-black instead. The bold-blue
// accelerator therefore loses its blue on the SELECTED row only — accepted, since it
// would be invisible against an identical fill.
var SelectionIsDark = false

// selectionOnLight is the foreground forced onto a selected row when the fill is light.
var selectionOnLight = lipgloss.NewStyle().Foreground(lipgloss.Color("#101010"))

// SelectedRow bands an already-styled row with SelectionBg across width columns. It takes
// BOTH forms: styled keeps inner styling (used on dark fills, where FillLineBackgroundWith
// re-opens the fill after every inner SGR reset), and plain is the unstyled text (used on
// light fills, restyled near-black for contrast). There is deliberately NO cursor glyph —
// the band is the cursor.
func SelectedRow(styled, plain string, width int) string {
	open, reset := DeriveBackgroundSGR(SelectionBg)
	body := styled
	if !SelectionIsDark {
		body = selectionOnLight.Render(plain)
	}
	return FillLineBackgroundWith(body, width, open, reset)
}
