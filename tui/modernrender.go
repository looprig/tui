package tui

import "github.com/looprig/cli/tui/styles"

// paintUserBackground is the MODERN-ONLY post-process that paints the gray panel behind a
// user entry's rendered lines: it replaces each line's styled form with a full-width
// gray-filled version (styles.FillLineBackground) and leaves plain/entry/sub untouched, so
// selection, copy, and the click-to-collapse provenance are unaffected — the fill is
// display-only and never reaches the clipboard. The scrollback Screen renders the SAME
// entry through renderEntry with no fill (see styles.BoxStyle); this transform runs only in
// renderFocused.
func paintUserBackground(lines []renderedLine, width int) []renderedLine {
	out := make([]renderedLine, len(lines))
	for i, ln := range lines {
		out[i] = renderedLine{
			styled: styles.FillLineBackground(ln.styled, width),
			plain:  ln.plain,
			entry:  ln.entry,
			sub:    ln.sub,
		}
	}
	return out
}
