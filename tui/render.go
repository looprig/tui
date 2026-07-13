package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/looprig/cli/tui/styles"
	"github.com/looprig/core/content"
)

// previewLineCap is the HARD cap on result-preview lines a tool card shows: a result with
// more lines is trimmed to the first previewLineCap lines plus a "… N more lines" marker.
// The cap applies ALWAYS — regardless of the ctrl+t expand fold (which now governs only the
// thinking block). A huge tool result (e.g. a long diff) otherwise fills the live tail,
// scrolling the assistant bullet off the top AND, on commit, stranding a screen-height
// scrollback gap (the inline renderer's insertAbove math is sized off the tall live tail).
const previewLineCap = 3

// liveCallCap bounds how many of a step's tool cards the LIVE tail shows: only the most
// recent liveCallCap, prefixed with a "… N earlier calls" marker. A step that fires many
// tools would otherwise grow the live tail (the bubbletea managed region) to fill the whole
// screen — scrolling the assistant bullet off the top and forcing a full-screen repaint each
// frame. The elided cards still commit IN FULL to scrollback at the step's StepDone.
const liveCallCap = 3

// noOutput is the placeholder shown for a completed tool call with no result lines.
const noOutput = "(no output)"

// hintSeparator joins the fields of the subagent done line ("<verb> · N steps"). It is
// " · " (a U+00B7 middle dot framed by single spaces), kept in one place so the hint
// stays consistent.
const hintSeparator = " · "

// cardConnector is the tree connector that prefixes each tool-call card line. It is
// dotWidth (2) columns — the "⎿" glyph plus a space — so a card's body aligns under it.
const cardConnector = "⎿ "

// cardIndent / resultIndent are the leading indents for a card line and for its
// result-preview lines (design §3: cards indent 2, result lines 4).
const (
	cardIndent   = "  "
	resultIndent = "    "
)

// Status glyphs for a tool card (design §3). A tick-driven spinner is a future
// enhancement; v1 uses the static running glyph.
const (
	glyphRunning   = "⋯"
	glyphOK        = "✓"
	glyphError     = "✗"
	glyphCancelled = "⊘"
)

// dotWidth is the display width of the assistant bullet prefix ("● "), which also
// matches glamour's "dark" document left margin. Narration wraps to this much less
// than the content width so continuation lines — indented to align under the first
// line — still fit.
const dotWidth = 2

// renderMD renders markdown to ANSI behind the static committed bullet (styles.LitDot,
// the DotColor-foregrounded "●"). It is the committed/scrollback path: a frozen
// assistant "●" never animates, so it always uses the lit dot. The live tail uses
// renderMDDot with a blink-phased bullet.
func renderMD(md string, width int) string {
	return renderMDDot(md, width, styles.LitDot)
}

// renderMDDot renders markdown to ANSI and prefixes it with dot so the narration
// begins on the SAME line as the bullet. glamour's "dark" style indents every line by
// a 2-column document margin and brackets the block with blank lines; those are
// stripped so the text aligns with the dot — first line "<dot>text", continuation
// lines indented to clear the bullet. On a glamour construction or render error it
// falls back to the raw text behind the dot, so the UI always gets readable output.
// dot MUST be dotWidth (2) columns wide so continuation-line alignment holds; callers
// pass either the static styles.Dot (committed) or a blink-phased live bullet.
func renderMDDot(md string, width int, dot string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}

	r, err := styles.NewMarkdownRenderer(max(0, width-dotWidth))
	if err != nil {
		return dot + md
	}
	out, err := r.Render(md)
	if err != nil {
		return dot + md
	}

	lines := dedentDocument(out)
	indent := strings.Repeat(" ", dotWidth)
	for i := range lines {
		if i == 0 {
			lines[i] = dot + lines[i]
		} else {
			lines[i] = indent + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// dedentDocument strips glamour's document framing from rendered output: the
// dotWidth-column left margin on every line and the surrounding blank lines. It
// returns at least one line.
func dedentDocument(s string) []string {
	margin := strings.Repeat(" ", dotWidth)
	raw := strings.Split(s, "\n")
	out := make([]string, 0, len(raw))
	for _, ln := range raw {
		out = append(out, strings.TrimPrefix(strings.TrimRight(ln, " "), margin))
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// toolGlyph maps a tool-call status to its single-rune display glyph (design §3).
// An unrecognised status falls back to the running glyph (fail-visible, not panic).
func toolGlyph(s ToolStatus) string {
	switch s {
	case ToolOK:
		return glyphOK
	case ToolError:
		return glyphError
	case ToolCancelled:
		return glyphCancelled
	default: // ToolRunning and any unknown value
		return glyphRunning
	}
}

// renderToolCalls renders a segment's tool-call children as indented cards, each a
// header line ("⎿ ToolName(Summary)  <glyph>") followed by its result preview. The
// preview is HARD-capped to previewLineCap lines plus a "… N more lines" marker
// regardless of expandTools (the ctrl+t fold governs only the thinking block now), so a
// huge result can never fill the live tail. An empty result renders "(no output)". An
// error card's result is capped the same way, never hidden. Lines are width-wrapped so a
// long card never blows the viewport. Returns "" when there are no calls.
func renderToolCalls(calls []ToolCallView, expandTools bool, width int) string {
	// Committed/scrollback path: full cards, static glyphs, never header-only (a
	// stray running card committed at a terminal still shows its body).
	return renderToolCallsGlyph(calls, expandTools, width, toolGlyph, false)
}

// renderToolCallsGlyph is the shared card renderer: it maps each call's status to a
// glyph via glyph, the indirection that lets the LIVE path animate a running card's
// glyph (spinnerGlyph) while the committed path keeps the static toolGlyph. When
// liveRunning is true (the LIVE tail only), a still-RUNNING card renders header-only
// — see renderToolCard. Returns "" when there are no calls.
func renderToolCallsGlyph(calls []ToolCallView, expandTools bool, width int, glyph func(ToolStatus) string, liveRunning bool) string {
	if len(calls) == 0 {
		return ""
	}
	parts := make([]string, 0, len(calls))
	for i := range calls {
		parts = append(parts, renderToolCard(calls[i], expandTools, width, glyph, liveRunning))
	}
	return strings.Join(parts, "\n")
}

// toolNodeStatus maps a tool's lifecycle status to its rail-node tint: error and
// cancelled are the failed (red) node, running is the pulsing node, and OK (or any
// unrecognised status) is the faint hollow node (fail-visible, never a panic).
func toolNodeStatus(s ToolStatus) styles.NodeStatus {
	switch s {
	case ToolError, ToolCancelled:
		return styles.NodeFailed
	case ToolRunning:
		return styles.NodeRunning
	default: // ToolOK and any unknown value
		return styles.NodeOK
	}
}

// renderToolNode renders one tool call as a rail node at depth: a status-tinted
// "○" (ok/failed) / "◍" (running) node glyph beside its faint header, then its result
// preview as "│ "-railed detail rows (the SAME hard-cap fold as before). A still-RUNNING
// card on the LIVE path (liveRunning) renders header-only — its body appears once the
// card commits to scrollback (the live→committed handoff fix, design Option B: a running
// card has no result yet, so dropping its "(no output)" body keeps the live tail one line
// tall and the running→completed transition reads as a clean continuation, not a
// multi-line shrink). It applies ONLY to ToolRunning cards on the live path; resolved
// cards (live or committed) and the committed path always render their full body. depth
// nests the node under a parent spine — Task 6 reuses this at depth 1 for subagent
// children.
func renderToolNode(c ToolCallView, depth int, expandTools bool, width int, liveRunning bool) []string {
	header := railNodeStyled(styles.ToolNode(toolNodeStatus(c.Status)), toolNodeHeaderText(c), styles.ToolCallStyle, depth, width)
	if liveRunning && c.Status == ToolRunning {
		return header // compact running indicator; body appears once, on commit
	}
	out := header
	for _, rl := range previewLines(c.Result, expandTools) {
		out = append(out, railDetail(rl, depth, width)...)
	}
	return out
}

// renderToolCard renders one tool call as a rail node (design: unified rail). It delegates
// to renderToolNode at depth 0. The glyph resolver is no longer consulted — the rail node
// glyph conveys status via toolNodeStatus — but the parameter is retained so the LIVE
// caller (renderToolCallsGlyph) keeps compiling; a later task removes the indirection.
func renderToolCard(c ToolCallView, expandTools bool, width int, _ func(ToolStatus) string, liveRunning bool) string {
	return strings.Join(renderToolNode(c, 0, expandTools, width, liveRunning), "\n")
}

// toolNodeHeaderText assembles a tool node's header text "<verb >ToolName(detail)" — the
// permission-decision verb ("Approved "/"Denied ") when the call prompted (empty for an
// ungated/pre-approved call) and the args detail in parens. The parens are omitted when
// there is no detail. It carries NO status glyph: the rail node glyph conveys status.
func toolNodeHeaderText(c ToolCallView) string {
	head := c.ToolName
	if detail := toolCallDetail(c); detail != "" {
		head = c.ToolName + "(" + detail + ")"
	}
	if v := decisionVerb(c.Decision); v != "" {
		head = v + " " + head
	}
	return head
}

// toolCallDetail returns the argument/target text to render inside ToolName(...).
// A permission prompt body is preferred because it is what the user approved.
// Otherwise the redacted audit summary is normalized to avoid duplicating the
// tool name inside the header.
func toolCallDetail(c ToolCallView) string {
	if c.Permission != "" {
		return c.Permission
	}
	return stripToolNamePrefix(c.ToolName, c.Summary)
}

// stripToolNamePrefix trims summaries such as "Bash: make test" and
// "ReadFile main.go" down to their argument text. The card header already renders
// the tool name, so repeating it obscures the useful detail.
func stripToolNamePrefix(toolName, summary string) string {
	s := strings.TrimSpace(summary)
	if s == "" {
		return ""
	}
	if s == toolName {
		return ""
	}
	if rest, ok := strings.CutPrefix(s, toolName+":"); ok {
		return strings.TrimSpace(rest)
	}
	if rest, ok := strings.CutPrefix(s, toolName+" "); ok {
		return strings.TrimSpace(rest)
	}
	return s
}

// decisionVerb maps a permission decision to its card-header verb. A call that never
// prompted (gateNone) — ungated or pre-approved by an existing grant — gets no verb;
// a still-pending gate likewise shows none (it resolves before the card commits).
func decisionVerb(d gateDecision) string {
	switch d {
	case gateApproved:
		return "Approved"
	case gateDenied:
		return "Denied"
	default:
		return ""
	}
}

// previewLines selects the result lines to display for a card. An empty result
// yields the single "(no output)" placeholder. When collapsed and the result has
// more than previewLineCap lines, it returns the first previewLineCap lines plus a
// "… N more lines · ctrl+t" marker (N = the remainder). When expanded, every line
// shows (the runner already capped the preview — no extra TUI cap).
func previewLines(result []string, _ bool) []string {
	// The expand flag is intentionally IGNORED for tool results: the preview is HARD-capped
	// to previewLineCap lines regardless of ctrl+t, so a huge result can never fill the live
	// tail (hiding the assistant bullet) or strand a commit-time scrollback gap. ctrl+t still
	// folds the thinking block. (The bool param is retained for call-site compatibility.)
	if len(result) == 0 {
		return []string{noOutput}
	}
	if len(result) <= previewLineCap {
		return result
	}
	remaining := len(result) - previewLineCap
	shown := make([]string, 0, previewLineCap+1)
	shown = append(shown, result[:previewLineCap]...)
	shown = append(shown, "… "+strconv.Itoa(remaining)+" more lines")
	return shown
}

// indentWrap word-wraps s to the column budget left after the indent, then prefixes
// every wrapped row with indent. A non-positive width skips wrapping (the indent is
// still applied). Trailing wrap padding is trimmed so output stays clean for tests
// and copy/paste.
func indentWrap(s, indent string, width int) string {
	avail := width - len(indent)
	if avail <= 0 {
		return indent + s
	}
	wrapped := lipgloss.NewStyle().Width(avail).Render(s)
	rows := strings.Split(wrapped, "\n")
	for i := range rows {
		rows[i] = styles.ToolResultStyle.Render(indent + strings.TrimRight(rows[i], " "))
	}
	return strings.Join(rows, "\n")
}

// renderAssistant renders a committed assistant segment under the node-presence rule:
// the thinking rail first (when the segment has reasoning), then the neon "●" AI-message
// node — via renderMD, which prefixes styles.LitDot — ONLY when the narration text is
// non-empty. Empty text renders no "●" node and no umbrella: a thinking-only segment is
// just its rail, and a pure-tool step commits no assistant entry at all (its tool calls
// stand alone as their own kindTool rail nodes). Committed tool cards are their OWN
// kindTool entries, so this never renders cards inline. expand drives the thinking
// block's compact/full fold. thinkHeader is the reasoning block's header label — the
// committed caller (renderEntry) passes formatThought(duration) so the rail reads
// "│ thought for Nsec" / "│ thought"; the live-spill caller passes styles.ThinkingHeader.
func renderAssistant(thinking, text string, expand bool, width int, thinkHeader string) string {
	var b strings.Builder
	if t := renderThinking(thinking, expand, width, thinkHeader); t != "" {
		b.WriteString(t)
	}
	if text != "" {
		body := renderMD(text, width)
		if b.Len() > 0 {
			b.WriteString("\n") // AI message follows its thinking block directly, no blank line
		}
		b.WriteString(body)
	}
	return b.String()
}

// subagentTerminalVerb maps a child loop's terminal status to its done-line verb
// (design §4): subDone→"done", subFailed→"failed", subInterrupted→"interrupted". An
// outstanding child (subRunning, the zero value) reads "running" — a defensive label
// for a card committed before its terminal (a card normally commits only after the
// child has handed back, so this is the rare in-flight case).
func subagentTerminalVerb(s subStatus) string {
	switch s {
	case subDone:
		return "done"
	case subFailed:
		return "failed"
	case subInterrupted:
		return "interrupted"
	default:
		return "running"
	}
}

// renderSubagentCard renders a committed Subagent card (design §5/§4): a "●"-level
// header "Subagent(<agent>)  \"<task>\"" beside the lit dot, the subagent's nested tool
// calls as ordinary "⎿" cards ONE indent level under the header (never "⎿ ⎿"), then a
// final "⎿ <verb> · N steps — \"<summary>\"" line whose verb comes from SubStatus and
// whose summary is the card's own (suppressed-elsewhere) Result — so the hand-back text
// shows ONLY here, never doubled as a normal result body. A subInterrupted card omits
// the summary; subFailed shows its error text. When Nested > 0 a trailing
// "⎿ +M nested subagent steps" line collapses the depth-2 activity (design §6). expand
// drives the child cards' result-preview fold.
func renderSubagentCard(c ToolCallView, expand bool, width int) string {
	// The header wraps to the viewport width (a long task string must NOT overrun the right
	// edge): the first row carries the lit "● " bullet, continuation rows a barWidth-wide
	// indent so the wrapped task aligns under the header text rather than under the bullet.
	head := wrapHeadline(subagentHeaderText(c), width)
	// Cap accounts for: the (possibly multi-row) header, the children's ONE joined element
	// from renderToolCalls (1), the done line (1), and the optional nested line (1).
	lines := make([]string, 0, len(head)+3)
	lines = append(lines, head...)

	// Children render as the existing "⎿" tool cards, one indent level under the header.
	if body := renderToolCalls(c.Children, expand, width); body != "" {
		lines = append(lines, body)
	}

	// The done/failed/interrupted child carries the step count and (for done/failed) the
	// summary — the ONLY place the hand-back text appears (no doubling).
	lines = append(lines, subagentDoneLine(c, width))

	if c.Nested > 0 {
		nested := cardIndent + styles.ToolCallStyle.Render(
			cardConnector+"+"+strconv.Itoa(c.Nested)+" nested subagent steps")
		lines = append(lines, nested)
	}
	return strings.Join(lines, "\n")
}

// wrapHeadline renders a "● <text>" bullet headline width-wrapped to the viewport: the
// first row carries the lit "● " bullet, every continuation row a barWidth-wide blank
// indent so the wrapped text hangs under the header rather than under the bullet. Each row
// is bold (HeadlineStyle) — the text is wrapped on its PLAIN form (no ANSI in the wrap
// input) before styling. A non-empty text always yields at least one row.
func wrapHeadline(text string, width int) []string {
	bullet := strings.TrimRight(styles.LitDot, " ") + " " // colored "● "
	hangingIndent := strings.Repeat(" ", barWidth)
	rows := wrapToWidth(text, max(1, width-barWidth))
	out := make([]string, 0, len(rows))
	for i, row := range rows {
		prefix := bullet
		if i > 0 {
			prefix = hangingIndent
		}
		out = append(out, prefix+styles.HeadlineStyle.Render(row))
	}
	return out
}

// subagentHeaderText assembles a Subagent card's header body: the standard tool-card
// form "Subagent(<agent>)" (tool name + the agent as its argument) plus the task in
// quotes when present. The task quotes are omitted for an empty task.
func subagentHeaderText(c ToolCallView) string {
	head := c.ToolName + "(" + c.Agent + ")"
	if c.Task != "" {
		head += `  "` + c.Task + `"`
	}
	return head
}

// subagentDoneLine builds the Subagent card's terminal "⎿" child: "<verb> · N steps"
// from SubStatus + Steps, plus the truncated summary (the card's own Result, the
// suppressed hand-back text) appended as `— "<summary>"` for done/failed. An
// interrupted child omits the summary (design §4). It is width-wrapped like a card body.
// Result is expected single-line, pre-truncated at the reduce layer; this only joins it.
func subagentDoneLine(c ToolCallView, width int) string {
	line := subagentTerminalVerb(c.SubStatus) + hintSeparator + plural(c.Steps, "step")
	if summary := strings.Join(c.Result, " "); summary != "" && c.SubStatus != subInterrupted {
		line += " — " + `"` + summary + `"`
	}
	return cardIndent + styles.ToolCallStyle.Render(cardConnector+line)
}

// plural renders a count with grammatical agreement on unit: "1 <unit>" (singular) for
// n == 1, "N <unit>s" (plural) otherwise. Used by the Subagent done line ("step") and
// the collapsed thinking summary ("line").
func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(n) + " " + unit + "s"
}

// nonSubagentCalls returns the calls that are NOT raw Subagent tool cards (ToolName ==
// subagentToolName). The LIVE tail suppresses the orchestrator's own raw Subagent running
// card (a generic "Subagent(Subagent)" row) because that activity is shown by the nested
// pending Subagent card instead (pendingSubagentCards → renderSubagentCard); rendering
// both would double it. It returns nil when every call is a Subagent call (so the
// working-word headline path is also suppressed for a subagent-only step). The result is a
// fresh slice; the input is not mutated.
func nonSubagentCalls(calls []ToolCallView) []ToolCallView {
	var out []ToolCallView
	for _, c := range calls {
		if c.ToolName == subagentToolName {
			continue
		}
		out = append(out, c)
	}
	return out
}

// renderLiveAssistant renders the in-progress (live) assistant segment with the
// animation state threaded in: the leading bullet blinks (liveDot) and a still-running
// tool card's glyph cycles through the spinner (spinnerGlyph), while resolved cards
// keep their static ✓/✗. It mirrors renderAssistant's ordering (thinking → narration
// → cards) but is the LIVE path ONLY — the committed renderAssistant stays static and
// is never given an anim. Empty parts are omitted.
//
// calls is the NON-Subagent live tool list (the caller filters the raw Subagent call out
// via nonSubagentCalls); subagentCards are the in-flight nested Subagent cards
// (pendingSubagentCards), each rendered as its own "●"-level card (renderSubagentCard)
// below the ordinary calls, separated by a blank line. The working-word headline shows
// ONLY when there is no narration AND at least one ORDINARY call (len(calls) > 0) — a step
// that only spawned subagents does NOT show "◦ Whirring", its activity is the nested card.
func renderLiveAssistant(thinking, text string, calls, subagentCards []ToolCallView, expand bool, width int, a animState) string {
	var b strings.Builder

	// The LIVE tail's reasoning header is the present-tense "thinking" (styles.ThinkingHeader):
	// the step has not committed, so no duration is known yet. It flips to "thought for Nsec"
	// once StepDone commits the step (renderEntry passes formatThought there).
	if t := renderThinking(thinking, expand, width, styles.ThinkingHeader); t != "" {
		b.WriteString(t)
	}

	body := renderMDDot(text, width, liveDot(a.blink))
	if body == "" && len(calls) > 0 {
		// Live empty-text tool step: a rotating working-word beside the blinking dot —
		// the provisional, pre-StepDone counterpart of the committed promoted-tool /
		// "Multiple actions" headline. The word may rotate while the step runs. It is
		// keyed on the NON-Subagent calls, so a subagent-only step shows no working-word.
		body = strings.TrimRight(liveDot(a.blink), " ") + " " + styles.HeadlineStyle.Render(workingWord(a.frame))
	}
	if body != "" {
		if b.Len() > 0 {
			b.WriteString("\n") // no blank line: the message follows its thinking block directly
		}
		b.WriteString(body)
	}

	if len(calls) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		// Cap the live tail to the most recent liveCallCap tool cards (keeping the running
		// one), prefixed with a "… N earlier calls" marker, so a many-tool step can't grow
		// the managed region to fill the screen and drop the assistant bullet. The full set
		// commits to scrollback at StepDone.
		shown := calls
		if hidden := len(calls) - liveCallCap; hidden > 0 {
			shown = calls[hidden:]
			b.WriteString(cardIndent + styles.ToolCallStyle.Render(cardConnector+"… "+strconv.Itoa(hidden)+" earlier calls") + "\n")
		}
		// liveRunning=true: a still-running card renders header-only in the live tail
		// so the live→committed handoff is a one-line→full-card continuation, not a
		// multi-line live shrink (see renderToolCard).
		b.WriteString(renderToolCallsGlyph(shown, expand, width, liveToolGlyph(a.frame), true))
	}

	// Each pending subagent card is its OWN "●"-level card (like the committed form),
	// separated by a blank line from whatever precedes it.
	for i := range subagentCards {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(renderSubagentCard(subagentCards[i], expand, width))
	}
	return b.String()
}

// liveToolGlyph returns a status→glyph resolver for the LIVE path: a running call
// shows the animated spinner cell for frame; every other (resolved) status falls
// through to the static toolGlyph. It closes over frame so renderToolCallsGlyph can
// stay frame-agnostic.
func liveToolGlyph(frame uint) func(ToolStatus) string {
	return func(s ToolStatus) string {
		if s == ToolRunning {
			return spinnerGlyph(frame)
		}
		return toolGlyph(s)
	}
}

// barWidth is the display columns a left-bar prefix ("▌ " / "│ ") consumes.
const barWidth = 2

// renderUser renders a committed user message as MARKDOWN behind the gray "▌ " rail:
// the text goes through the same glamour renderer as assistant narration (so a fenced
// file attachment shows as a code block, and lists/headings/inline-code/links render),
// then every output line is prefixed with the bar so the message reads as the user's
// with its left-accent identity. This is display-only — the literal text is what was
// sent to the model. On a glamour construction/render error it falls back to the raw,
// width-wrapped text behind the bar (bad markdown shows as-is).
func renderUser(text string, width int) string {
	return renderMDRail(text, width, styles.AccentBarStyle.Render(styles.AccentBarPrompt))
}

// renderMDRail renders md to ANSI (glamour) and prefixes EVERY line with bar — a left
// rail down the whole block. It dedents glamour's document margin first so the content
// sits flush behind the bar (bar is barWidth == dotWidth columns), and falls back to
// the raw, width-wrapped text behind the bar on any glamour error.
func renderMDRail(md string, width int, bar string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	raw := func() string {
		var out []string
		for _, para := range strings.Split(md, "\n") {
			for _, line := range wrapToWidth(para, width-barWidth) {
				out = append(out, bar+line)
			}
		}
		return strings.Join(out, "\n")
	}
	r, err := styles.NewMarkdownRenderer(max(0, width-barWidth))
	if err != nil {
		return raw()
	}
	out, err := r.Render(md)
	if err != nil {
		return raw()
	}
	lines := dedentDocument(out)
	for i := range lines {
		lines[i] = bar + lines[i]
	}
	return strings.Join(lines, "\n")
}

// firstLine returns the first "\n"-delimited line of s (s itself when single-line).
// The queued affordance previews only the first line of a multi-line submission —
// it is a compact pending hint, not the full committed row.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// thinkingRail is the left-rail margin ("│ ") that prefixes EVERY line of the
// expanded thinking block — header included — so the block renders as one unbroken
// vertical rail attaching the reasoning to the assistant turn it precedes.
const thinkingRail = "│ "

// renderThinking renders the model's reasoning under the unified ctrl+t expand flag,
// behind a caller-supplied header label so the SAME renderer serves both the live tail
// and committed scrollback: the LIVE tail passes the present-tense "thinking"
// (styles.ThinkingHeader); a COMMITTED entry passes formatThought(duration) — "thought
// for Ns" once its span is known, or the bare "thought" fallback when it is not
// (a cold restore replays no timing). Both modes render on the "│ " rail and BOTH carry a
// blank "│" rail gap row directly under the header (it stays put whether collapsed or
// expanded): COLLAPSED is the "│ <header>" line then the "│" gap (no reasoning body);
// EXPANDED is that same header + gap, then the "│ "-prefixed, width-wrapped reasoning, then a
// TRAILING "│" gap that sets the body off from the AI message below — an unbroken vertical
// rail down the left margin, gapped top and bottom. Empty/whitespace-only reasoning renders
// nothing in either mode.
func renderThinking(s string, expand bool, width int, header string) string {
	s = strings.TrimSpace(s) // drop the model's leading/trailing blank reasoning lines
	if s == "" {
		return ""
	}
	// The header, then a blank "│" rail gap row that pads it off whatever follows. The gap is
	// present in BOTH forms — it stays put whether the block is collapsed or expanded — so
	// "thought for Ns" always reads with one line of breathing space beneath it (before the
	// AI message when collapsed, before the reasoning body when expanded). The "│ " rail stays
	// unbroken (like an empty reasoning line), never a bare gap.
	out := []string{
		styles.ThinkingStyle.Render(thinkingRail + header),
		styles.ThinkingStyle.Render(thinkingRail),
	}
	if !expand {
		// COLLAPSED: header + the rail gap, no reasoning body. No arrow, no glyph, no
		// "· N lines · ctrl+t" summary.
		return strings.Join(out, "\n")
	}
	for _, raw := range strings.Split(s, "\n") {
		for _, line := range wrapToWidth(raw, width-barWidth) {
			out = append(out, styles.ThinkingStyle.Render(thinkingRail+line))
		}
	}
	// A trailing rail gap closes the expanded block: it sets the reasoning body off from the
	// AI message that follows, keeping the "│" rail (a bare blank line would break it).
	out = append(out, styles.ThinkingStyle.Render(thinkingRail))
	return strings.Join(out, "\n")
}

// formatThought renders a committed thinking block's header from its measured span (the
// modern shell stamps the model clock onto each Ephemeral thinking chunk — see
// Screen.handleEventModern): a zero duration (a cold restore / backlog carries NO timing)
// yields the bare lowercase "thought"; any measured span under a second is floored to
// "thought for 1s" (thinking that ran is never shown as timeless — thinkDuration reports the
// positive measuredFloor sentinel for a same-tick span); under a minute "thought for Ns"
// (whole seconds, truncated); a minute or more "thought for Nm Ns". It is the committed
// counterpart of the live tail's present-tense "thinking" header — the same rail, flipped
// from present to past once the step commits.
func formatThought(d time.Duration) string {
	if d <= 0 {
		return "thought"
	}
	if d < time.Second {
		return "thought for 1s"
	}
	secs := int(d / time.Second)
	if secs < 60 {
		return "thought for " + strconv.Itoa(secs) + "s"
	}
	return "thought for " + strconv.Itoa(secs/60) + "m " + strconv.Itoa(secs%60) + "s"
}

// wrapToWidth word-wraps s to width columns and returns the resulting rows with
// trailing wrap padding trimmed. A non-positive width skips wrapping (single row).
func wrapToWidth(s string, width int) []string {
	if width <= 0 {
		return []string{s}
	}
	wrapped := lipgloss.NewStyle().Width(width).Render(s)
	rows := strings.Split(wrapped, "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

// renderInlineBlocks renders each block to plain text and joins with newlines.
// Used for user rows where blocks are shown verbatim (no markdown).
func renderInlineBlocks(blocks []content.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, blk := range blocks {
		parts = append(parts, renderBlock(blk))
	}
	return strings.Join(parts, "\n")
}

// assistantText concatenates the narration of an assistant segment for markdown
// rendering: every block except ThinkingBlock (rendered separately as the dim
// thinking block by renderThinking, so it must not be markdown-rendered here too).
func assistantText(blocks []content.Block) string {
	parts := make([]string, 0, len(blocks))
	for _, blk := range blocks {
		if _, ok := blk.(*content.ThinkingBlock); ok {
			continue
		}
		parts = append(parts, renderBlock(blk))
	}
	return strings.Join(parts, "\n")
}

// thinkingText concatenates the reasoning of every ThinkingBlock in blocks, the
// source for an assistant row's dim thinking block.
func thinkingText(blocks []content.Block) string {
	var b strings.Builder
	for _, blk := range blocks {
		if tb, ok := blk.(*content.ThinkingBlock); ok {
			b.WriteString(tb.Thinking)
		}
	}
	return b.String()
}

// firstText returns the text of the first TextBlock, or "" if there is none.
// Used by single-block roles (the leveled notice).
func firstText(blocks []content.Block) string {
	for _, blk := range blocks {
		if tb, ok := blk.(*content.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}

// renderBlock renders one block to its display string via a type switch over the
// sealed Block interface. Unknown types fall through to a safe placeholder.
func renderBlock(blk content.Block) string {
	switch b := blk.(type) {
	case *content.TextBlock:
		return b.Text
	case *content.ThinkingBlock:
		return b.Thinking
	case *content.ImageBlock:
		return fmt.Sprintf("[image: %s, %d bytes]", string(b.MediaType), len(b.Source.Data))
	default:
		return "[unsupported block]"
	}
}
