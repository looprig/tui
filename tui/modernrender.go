package tui

import (
	"strings"

	"github.com/looprig/core/content"

	"github.com/looprig/cli/tui/styles"
)

// modernQueuedTag is the dim marker word prefixed to every modern queued-affordance
// row so a stacked, not-yet-running user message reads UNAMBIGUOUSLY as queued —
// distinct from both the bold committed user row it will promote to and the live
// assistant tail it sits between. The scrollback affordance (renderQueued) relies on
// faintness + position alone; the modern viewport places the queue inline in the live
// tail, so it spells out the state.
const modernQueuedTag = "queued"

// renderQueuedModern renders the focused loop's pending queued-input affordances as dim,
// EXPLICITLY-tagged rows for the modern viewport: each message's first line of text,
// prefixed with the gray "▌ " accent bar and a faint "queued " tag, wrapped to width and
// rendered in styles.QueuedStyle. It is the modern sibling of the shared renderQueued (a
// compact one-line-per-message preview of a throwaway affordance, never committed
// scrollback) — MODERN-ONLY, so the scrollback surface keeps renderQueued byte-identical.
// Empty messages yields "" so renderFocused appends no queued rows.
func renderQueuedModern(messages [][]content.Block, width int) string {
	if len(messages) == 0 {
		return ""
	}
	bar := styles.AccentBarStyle.Render(styles.AccentBarPrompt)
	var out []string
	for _, blocks := range messages {
		text := modernQueuedTag + "  " + firstLine(renderInlineBlocks(blocks))
		for _, line := range wrapToWidth(text, width-barWidth) {
			out = append(out, bar+styles.QueuedStyle.Render(line))
		}
	}
	return strings.Join(out, "\n")
}

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
