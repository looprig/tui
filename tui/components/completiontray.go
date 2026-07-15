package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/looprig/cli/tui/styles"
)

type completionTrayRow struct {
	primary   string
	secondary string
}

func completionTrayNaturalWidth(rows []completionTrayRow) int {
	width := 0
	for _, row := range rows {
		line := styles.AccentBar + " " + row.primary
		if row.secondary != "" {
			line += "  " + row.secondary
		}
		width = max(width, ansi.StringWidth(line))
	}
	return width
}

func renderCompletionTray(rows []completionTrayRow, selected, width int) string {
	if width <= 0 || len(rows) == 0 {
		return ""
	}

	panelOpen, panelReset := styles.DeriveBackgroundSGR(styles.PanelBg)
	selectedOpen, selectedReset := styles.DeriveBackgroundSGR(styles.CardSelectedBg)
	rendered := make([]string, len(rows))
	for i, row := range rows {
		rail := styles.AccentBarStyle.Render(styles.AccentBar)
		primary := row.primary
		open, reset := panelOpen, panelReset
		if i == selected {
			rail = styles.CardRailStyle.Render(styles.AccentBar)
			primary = styles.CardSelectedStyle.Render(primary)
			open, reset = selectedOpen, selectedReset
		}

		line := rail + " " + primary
		if row.secondary != "" {
			line += "  " + styles.CardHintStyle.Render(row.secondary)
		}
		line = ansi.Truncate(line, width, "")
		rendered[i] = styles.FillLineBackgroundWith(line, width, open, reset)
	}
	return strings.Join(rendered, "\n")
}

// renderCompletionTrayWindow renders at most maxRows while keeping selected visible. The
// window follows the cursor once it moves past the initially visible rows and shifts back at
// the end so every non-empty window uses its full row budget.
func renderCompletionTrayWindow(rows []completionTrayRow, selected, width, maxRows int) string {
	if maxRows <= 0 || len(rows) == 0 {
		return ""
	}
	if maxRows >= len(rows) {
		return renderCompletionTray(rows, selected, width)
	}

	selected = max(0, min(selected, len(rows)-1))
	start := selected - maxRows + 1
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(rows) {
		start = len(rows) - maxRows
	}
	return renderCompletionTray(rows[start:start+maxRows], selected-start, width)
}
