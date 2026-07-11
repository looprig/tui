package tui

import (
	"strings"

	"github.com/looprig/core/content"

	"github.com/looprig/cli/tui/styles"
)

// queuedTag is the marker word for a queued-affordance: renderQueued uppercases it into a
// "QUEUED" banner row shown ON TOP of each stacked, not-yet-running user message so the state
// reads UNAMBIGUOUSLY as queued — distinct from both the bold committed user row it will promote
// to and the live assistant tail it sits between. The queue is placed inline in the live tail, so
// it spells out the state rather than relying on faintness + position alone.
const queuedTag = "queued"

// renderQueued renders the focused loop's pending queued-input affordances for the
// viewport: each message is a blue "▌ QUEUED" banner row (styles.QueuedLabelStyle) with the
// message's first line of text on the row(s) beneath it — every row prefixed with the gray
// "▌ " accent bar, the message text wrapped to width and rendered faint (styles.QueuedStyle).
// The QUEUED label sits ON TOP of the message so the pending state reads unambiguously. It is
// a compact per-message preview of a throwaway affordance, never committed to scrollback.
// Empty messages yields "" so renderFocused appends no queued rows.
func renderQueued(messages [][]content.Block, width int) string {
	if len(messages) == 0 {
		return ""
	}
	bar := styles.AccentBarStyle.Render(styles.AccentBarPrompt)
	label := bar + styles.QueuedLabelStyle.Render(strings.ToUpper(queuedTag))
	var out []string
	for _, blocks := range messages {
		out = append(out, label)
		text := firstLine(renderInlineBlocks(blocks))
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
