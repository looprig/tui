package components

import (
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/looprig/tui/styles"
)

type completionTrayRow struct {
	primary   string
	secondary string
}

// completionTrayPrimaryColumn is the display width every primary is padded out to so the
// secondaries line up down the tray instead of trailing each command raggedly.
//
// It is the widest primary among the rows that HAVE a secondary. A row with no description
// paints nothing at that column, so letting a long bare primary push the column right would
// only cost width and buy nothing. It is recomputed per render from the rows actually being
// drawn, so a scrolled window aligns to what is on screen rather than to a row the user
// cannot see.
func completionTrayPrimaryColumn(rows []completionTrayRow) int {
	column := 0
	for _, row := range rows {
		if row.secondary == "" {
			continue
		}
		column = max(column, ansi.StringWidth(row.primary))
	}
	return column
}

// completionTrayGap is the run of spaces between a primary and its secondary: enough to pad
// the primary out to the shared column, plus the two-space gutter that separates the two
// columns. Measuring and rendering both go through it, so the natural width can never
// disagree with what is drawn.
func completionTrayGap(primary string, column int) string {
	return strings.Repeat(" ", max(0, column-ansi.StringWidth(primary))+2)
}

func completionTrayNaturalWidth(rows []completionTrayRow) int {
	column := completionTrayPrimaryColumn(rows)
	width := 0
	for _, row := range rows {
		line := styles.AccentBar + " " + row.primary
		if row.secondary != "" {
			line += completionTrayGap(row.primary, column) + row.secondary
		}
		width = max(width, ansi.StringWidth(line))
	}
	return width
}

func renderCompletionTray(rows []completionTrayRow, selected, width int) string {
	return renderCompletionTrayBackground(rows, selected, width, styles.TraySelectedBg)
}

// trayRowRender holds everything the rows of ONE render share: the width they fill, the
// column their secondaries align to, the inset their content is pushed in by, and the panel
// fill's SGR pair -- derived once per render rather than once per row, per the note on
// styles.FillLineBackgroundWith.
//
// It exists so the batch renderer and the list-backed trayDelegate compose a row through the
// SAME code. Two renderers that merely agree today would drift the first time one of them is
// touched; going through one primitive makes their agreement structural.
type trayRowRender struct {
	width      int
	column     int
	inset      int
	panelOpen  string
	panelReset string
}

func newTrayRowRender(rows []completionTrayRow, width, inset int) trayRowRender {
	open, reset := styles.DeriveBackgroundSGR(styles.PanelBg)
	return trayRowRender{
		width:      width,
		column:     completionTrayPrimaryColumn(rows),
		inset:      max(0, inset),
		panelOpen:  open,
		panelReset: reset,
	}
}

// row composes one PHYSICAL row -- rail, inset, body -- and paints its background.
//
// The rail is the same neutral gray on every row, selected or not. It is the tray's left
// edge, not a second cursor: the band is the cursor, so a highlighted rail would say the
// same thing twice and break the edge's continuity down the tray. It is moot on the selected
// row in any case -- the shared fill is light, so styles.SelectedRow strips inner styling and
// re-renders the row near-black, discarding whatever color were chosen here.
//
// The clamp happens BEFORE the band because styles.SelectedRow pads to width but never
// truncates; that half of the contract is the caller's. It clamps short of width by the
// inset, so nothing is ever drawn in the trailing inset columns -- while the FILL still runs
// the full width, so the tray stays one solid block rather than a ragged one.
func (r trayRowRender) row(body string, selected bool) string {
	line := styles.AccentBarStyle.Render(styles.AccentBar) + strings.Repeat(" ", 1+r.inset) + body
	line = ansi.Truncate(line, max(0, r.width-r.inset), "")
	if selected {
		return styles.SelectedRow(line, r.width)
	}
	return styles.FillLineBackgroundWith(line, r.width, r.panelOpen, r.panelReset)
}

// line renders a whole tray row: the primary, padded out to the shared column, then the
// faint secondary beside it.
func (r trayRowRender) line(row completionTrayRow, selected bool) string {
	body := row.primary
	if row.secondary != "" {
		body += completionTrayGap(row.primary, r.column) + styles.CardHintStyle.Render(row.secondary)
	}
	return r.row(body, selected)
}

// renderCompletionTrayBackground renders every row at width columns and bands the selected
// one with the shared selection treatment.
//
// selectedBg is IGNORED and kept only so the four panels' ViewWindowBackground signatures
// need not change while they migrate one at a time. styles.SelectedRow is deliberately
// one-argument -- it owns the selection fill so no surface can drift to its own shade -- which
// also means the caller's per-frame glow color no longer reaches the row. Retiring the glow
// (and this parameter with it) belongs to the task that migrates the last panel, not here.
func renderCompletionTrayBackground(rows []completionTrayRow, selected, width int, selectedBg color.Color) string {
	if width <= 0 || len(rows) == 0 {
		return ""
	}

	render := newTrayRowRender(rows, width, 0)
	rendered := make([]string, len(rows))
	for i, row := range rows {
		rendered[i] = render.line(row, i == selected)
	}
	return strings.Join(rendered, "\n")
}

func renderCompletionTrayWindowBackgroundAt(rows []completionTrayRow, selected, width, maxRows, start int, selectedBg color.Color) string {
	if maxRows <= 0 || len(rows) == 0 {
		return ""
	}
	if maxRows >= len(rows) {
		return renderCompletionTrayBackground(rows, selected, width, selectedBg)
	}
	start = max(0, min(start, len(rows)-maxRows))
	return renderCompletionTrayBackground(rows[start:start+maxRows], selected-start, width, selectedBg)
}

func completionTrayWindowStart(rowCount, selected, maxRows int) int {
	if rowCount <= 0 || maxRows <= 0 || maxRows >= rowCount {
		return 0
	}
	selected = max(0, min(selected, rowCount-1))
	start := selected - maxRows + 1
	if start < 0 {
		start = 0
	}
	if start+maxRows > rowCount {
		start = rowCount - maxRows
	}
	return start
}
