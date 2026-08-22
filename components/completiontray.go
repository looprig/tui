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

// renderCompletionTrayBackground renders every row at width columns and bands the selected
// one with the shared selection treatment.
//
// selectedBg is IGNORED and kept only so the four panels' ViewWindowBackground signatures
// need not change while they migrate one at a time. styles.SelectedRow is deliberately
// one-argument — it owns the selection fill so no surface can drift to its own shade — which
// also means the caller's per-frame glow color no longer reaches the row. Retiring the glow
// (and this parameter with it) belongs to the task that migrates the last panel, not here.
func renderCompletionTrayBackground(rows []completionTrayRow, selected, width int, selectedBg color.Color) string {
	if width <= 0 || len(rows) == 0 {
		return ""
	}

	panelOpen, panelReset := styles.DeriveBackgroundSGR(styles.PanelBg)
	column := completionTrayPrimaryColumn(rows)
	rendered := make([]string, len(rows))
	for i, row := range rows {
		// The rail is the SAME neutral gray on every row, selected or not. It is the
		// tray's left edge, not a second cursor: the band is the cursor, so a highlighted
		// rail would say the same thing twice and break the edge's continuity down the
		// tray. It is also moot on the selected row — the shared fill is light, so
		// SelectedRow strips inner styling and re-renders the row near-black, discarding
		// whatever color were chosen here. The old blue CardRailStyle rail would have
		// been brand blue on brand blue.
		rail := styles.AccentBarStyle.Render(styles.AccentBar)

		styled := rail + " " + row.primary
		if row.secondary != "" {
			styled += completionTrayGap(row.primary, column) + styles.CardHintStyle.Render(row.secondary)
		}
		// SelectedRow pads but never truncates, so clamp to width first — that is the
		// caller's half of the contract.
		styled = ansi.Truncate(styled, width, "")
		if i == selected {
			rendered[i] = styles.SelectedRow(styled, width)
			continue
		}
		rendered[i] = styles.FillLineBackgroundWith(styled, width, panelOpen, panelReset)
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
