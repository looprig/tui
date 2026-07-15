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
