package view

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/looprig/tui/styles"
)

// railGlyph is one rail column: the vertical bar U+2502 plus a trailing space, so a
// column occupies railWidth (2) display cells and stacks flush into a continuous spine.
const railGlyph = "│ "

// railWidth is the display width of one rail column (and of a node glyph), so nested
// content indents by a whole number of aligned columns.
const RailWidth = 2

// railSpine returns n leading styled rail columns ("│ " each). railSpine(0) == "".
// It is the faint vertical spine shared with the thinking rail, so nodes at nesting
// depth n hang below n bars.
func RailSpine(n int) string {
	return strings.Repeat(styles.ThinkingStyle.Render(railGlyph), n)
}

// railNode renders a node row at depth: `depth` leading rail columns, then the node
// glyph (2 cols, e.g. styles.LitDot / styles.ToolNode(...)), then width-wrapped text
// hanging under the glyph. Continuation lines carry the spine plus a railWidth blank so
// wrapped text aligns under the first line's text, never under the glyph. glyph is
// pre-styled by the caller; text is plain. Trailing spaces are trimmed per row.
func RailNode(glyph, text string, depth, width int) []string {
	return RailNodeStyled(glyph, text, lipgloss.NewStyle(), depth, width)
}

// railNodeStyled is railNode with a per-line text style applied to each wrapped text row
// (e.g. faint tool headers) before assembly — the glyph and rail columns are left
// unstyled by this helper (the glyph is pre-styled by the caller; the spine carries its
// own faint style). railNode delegates here with a no-op style. Because the styling is
// zero-width ANSI, an ANSI-stripped assertion sees the SAME text as railNode.
func RailNodeStyled(glyph, text string, textStyle lipgloss.Style, depth, width int) []string {
	spine := RailSpine(depth)
	rows := WrapToWidth(text, max(1, width-RailWidth*(depth+1)))
	// Continuation indent: the spine, then a railWidth blank standing in for the glyph
	// column, so wrapped text lines up under the first line's text.
	contIndent := spine + strings.Repeat(" ", RailWidth)
	out := make([]string, 0, len(rows))
	for i, row := range rows {
		styled := textStyle.Render(row)
		if i == 0 {
			out = append(out, strings.TrimRight(spine+glyph+styled, " "))
			continue
		}
		out = append(out, strings.TrimRight(contIndent+styled, " "))
	}
	return out
}

// railDetail renders detail rows under a node at depth: the detail belongs to the node's
// own column, which is one rail level deeper, so each row is railSpine(depth+1) plus
// faint (styles.ToolResultStyle) width-wrapped text. Trailing spaces are trimmed per row.
func RailDetail(text string, depth, width int) []string {
	spine := RailSpine(depth + 1)
	rows := WrapToWidth(text, max(1, width-RailWidth*(depth+1)))
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimRight(spine+styles.ToolResultStyle.Render(row), " "))
	}
	return out
}

// railConnector renders a bare connector row between two nodes at depth: the node column
// continued as a rail with no glyph — railSpine(depth) plus a trailing rail glyph trimmed
// of its trailing space, so the row is just the faint vertical bars.
func RailConnector(depth int) string {
	return RailSpine(depth) + styles.ThinkingStyle.Render(strings.TrimRight(railGlyph, " "))
}
