package components

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/looprig/tui/styles"
)

type trayRowKind uint8

// trayRowHeading is visual group chrome, such as a model provider. It renders in the
// panel but never takes the cursor or dispatches a selection. Ordinary interactive rows
// keep the trayRowKind zero value, so existing literals need no extra boilerplate.
const trayRowHeading trayRowKind = 1

// trayRowSpacer is a blank rail-carrying row: the breathing room BETWEEN a group's last
// option and the next group's heading. It is a row rather than a delegate pad because a
// tray's spacing is uniform per entry (see trayLayout.PadV) while this gap falls only at a
// group boundary -- and because it must carry the rail, so the panel's left edge runs
// unbroken through the gap. Like a heading it never takes the cursor.
const trayRowSpacer trayRowKind = 2

type completionTrayRow struct {
	primary   string
	secondary string
	// filter is the string whose indices safely map onto primary for match underlining.
	// Empty falls back to primary, preserving every existing tray's behavior.
	filter string
	kind   trayRowKind
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
// The rail is the tray's left edge, not a second cursor: the band is the cursor. On an
// ordinary row it is the same neutral gray throughout, and on the SELECTED row it is handed
// to styles.SelectedRowWithRail, which redraws it in the band's own color. Either way the
// edge reads as one continuous line down the panel and never restates the selection.
//
// The clamp happens BEFORE the band because styles.SelectedRow pads to width but never
// truncates; that half of the contract is the caller's. It clamps short of width by the
// inset, so nothing is ever drawn in the trailing inset columns -- while the FILL still runs
// the full width, so the tray stays one solid block rather than a ragged one. The rail's own
// column is excluded from the body's budget, since the rail is composed beside the body
// rather than truncated with it.
func (r trayRowRender) row(body string, selected bool) string {
	avail := max(0, r.width-r.inset)
	if avail <= 0 {
		return styles.FillLineBackgroundWith("", r.width, r.panelOpen, r.panelReset)
	}
	content := ansi.Truncate(strings.Repeat(" ", 1+r.inset)+body, max(0, avail-ansi.StringWidth(styles.AccentBar)), "")
	if selected {
		return styles.SelectedRowWithRail(styles.AccentBar, content, r.width)
	}
	line := styles.AccentBarStyle.Render(styles.AccentBar) + content
	return styles.FillLineBackgroundWith(line, r.width, r.panelOpen, r.panelReset)
}

// line renders a whole tray row: the primary, padded out to the shared column, then the
// faint secondary beside it.
func (r trayRowRender) line(row completionTrayRow, selected bool) string {
	switch row.kind {
	case trayRowSpacer:
		return r.row("", false)
	case trayRowHeading:
		return r.row(styles.HeadlineStyle.Render(row.primary), false)
	}
	body := row.primary
	if row.secondary != "" {
		body += completionTrayGap(row.primary, r.column) + styles.CardHintStyle.Render(row.secondary)
	}
	return r.row(body, selected)
}

const trayHeaderHeight = 4

// renderTrayHeader makes a tray's top breathing room, title, muted count, and one-row
// separator part of the same full-width panel as its choices. The blank rail rows preserve a
// clear edge while keeping the header distinct from both the rest of the screen and its first
// selectable record.
func renderTrayHeader(width int, title, summary string) string {
	if width <= 0 {
		return ""
	}
	render := newTrayRowRender(nil, width, 0)
	return strings.Join([]string{
		render.row("", false),
		render.row(styles.HeadlineStyle.Render(title), false),
		render.row(styles.StatusStyle.Render(summary), false),
		render.row("", false),
	}, "\n")
}

func renderTrayHint(width int, text string) string {
	if width <= 0 {
		return ""
	}
	return newTrayRowRender(nil, width, 0).row(styles.StatusStyle.Render(text), false)
}

// renderCompletionTray renders every row at width columns and bands the selected one with
// the shared selection treatment.
//
// There is deliberately NO fill parameter: styles.SelectedRow is one-argument and owns the
// selection fill, so no surface can drift to its own shade.
//
// It is the batch renderer trayList is measured against: it draws a whole slice through the
// same trayRowRender the engine's delegate composes a row with, which is what makes
// TestTrayListMatchesTheHandRolledTray a real byte-identity gate rather than two renderers
// agreeing by coincidence.
func renderCompletionTray(rows []completionTrayRow, selected, width int) string {
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
