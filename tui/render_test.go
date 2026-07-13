package tui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/looprig/cli/tui/styles"
)

// TestFormatThought covers the committed thinking-header formatter: a zero span (a cold
// restore / backlog with NO timing captured) is the bare lowercase "thought"; any measured
// span under a second is floored to "thought for 1s" (thinking that ran is never shown as
// timeless); under a minute is "thought for Ns" (whole seconds, truncated); a minute or more
// is "thought for Nm Ns".
func TestFormatThought(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "zero is the bare fallback", d: 0, want: "thought"},
		{name: "negative degrades to the fallback", d: -5 * time.Second, want: "thought"},
		{name: "measured-floor sentinel shows one second", d: measuredFloor, want: "thought for 1s"},
		{name: "sub-second floors to one second", d: 400 * time.Millisecond, want: "thought for 1s"},
		{name: "just under a second floors to one second", d: 999 * time.Millisecond, want: "thought for 1s"},
		{name: "exactly one second", d: time.Second, want: "thought for 1s"},
		{name: "ten seconds", d: 10 * time.Second, want: "thought for 10s"},
		{name: "truncates to whole seconds", d: 10*time.Second + 900*time.Millisecond, want: "thought for 10s"},
		{name: "just under a minute", d: 59 * time.Second, want: "thought for 59s"},
		{name: "exactly one minute", d: 60 * time.Second, want: "thought for 1m 0s"},
		{name: "minutes and seconds", d: 90 * time.Second, want: "thought for 1m 30s"},
		{name: "several minutes", d: 5*time.Minute + 5*time.Second, want: "thought for 5m 5s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatThought(tt.d); got != tt.want {
				t.Errorf("formatThought(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}

// ansiSGR matches ANSI SGR (color/style) escape sequences. The markdown renderer
// emits per-word color spans, so substring assertions on narration text must strip
// styling first — they verify rendered TEXT, not the incidental color codes (which
// depend on the runtime color profile and would otherwise split words like
// "reading config" across two escapes).
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// stripANSI removes SGR escape sequences so content assertions match the visible text.
func stripANSI(s string) string { return ansiSGR.ReplaceAllString(s, "") }

func TestRenderMD(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		md       string
		width    int
		wantWord string // substring expected in the output (empty → expect blank)
	}{
		{name: "happy path", md: "hello world", width: 80, wantWord: "hello"},
		{name: "heading", md: "# Title here", width: 80, wantWord: "Title"},
		{name: "narrow width", md: "wrapme please", width: 10, wantWord: "wrapme"},
		{name: "empty", md: "", width: 80, wantWord: ""},
		{name: "zero width", md: "zerowidth", width: 0, wantWord: "zerowidth"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stripANSI(renderMD(tt.md, tt.width))
			if tt.wantWord == "" {
				if strings.TrimSpace(got) != "" {
					t.Errorf("renderMD(%q) = %q, want empty/whitespace", tt.md, got)
				}
				return
			}
			if !strings.Contains(got, tt.wantWord) {
				t.Errorf("renderMD(%q) = %q, want to contain %q", tt.md, got, tt.wantWord)
			}
		})
	}
}

// TestRenderMDAlignsWithDot covers aligning the AI message with its bullet: the
// narration starts on the SAME line as the "●" dot, not the dot alone with the text
// indented on the next line.
func TestRenderMDAlignsWithDot(t *testing.T) {
	t.Parallel()

	got := stripANSI(renderMD("Hello there friend", 60))
	first := got
	if i := strings.IndexByte(got, '\n'); i >= 0 {
		first = got[:i]
	}
	if !strings.HasPrefix(first, styles.Dot) {
		t.Errorf("first line = %q, want it to start with the dot %q", first, styles.Dot)
	}
	if !strings.Contains(first, "Hello there friend") {
		t.Errorf("first line = %q, want the narration on the same line as the dot", first)
	}
}

// makeLines returns a slice of n distinct result lines ("line0".."lineN-1").
func makeLines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line" + itoa(i)
	}
	return out
}

// itoa is a tiny base-10 int→string for test fixtures (avoids importing strconv
// just for the table builder).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	return string(b[p:])
}

// TestToolNodeStatus covers the tool-lifecycle → rail-node tint mapping: OK (and any
// unknown status) is the faint hollow node, error and cancelled are the failed (red)
// node, and running is the pulsing node. stripANSI cannot distinguish the node colors,
// so this maps the status directly to its styles.NodeStatus.
func TestToolNodeStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ToolStatus
		want   styles.NodeStatus
	}{
		{name: "ok is the hollow node", status: ToolOK, want: styles.NodeOK},
		{name: "error is the failed node", status: ToolError, want: styles.NodeFailed},
		{name: "cancelled is the failed node", status: ToolCancelled, want: styles.NodeFailed},
		{name: "running is the pulsing node", status: ToolRunning, want: styles.NodeRunning},
		{name: "unknown falls back to the hollow node", status: ToolStatus(99), want: styles.NodeOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := toolNodeStatus(tt.status); got != tt.want {
				t.Errorf("toolNodeStatus(%d) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

// TestRenderToolCard_RailNode covers the rail-node tool-card render: a resolved card is
// its status-tinted "○"/"◍" node glyph beside the faint header, then its result preview
// as "│ "-railed detail rows — never the old "⎿" card connector. A still-running card on
// the LIVE path renders header-only with the pulsing "◍" node (its body appears once
// committed). Node color is asserted by TestToolNodeStatus, not here (stripANSI is
// color-blind).
func TestRenderToolCard_RailNode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		render func() string
		want   []string // substrings that must appear
		absent []string // substrings that must NOT appear
	}{
		{
			name: "ok card is a hollow node with header and railed detail",
			render: func() string {
				c := ToolCallView{ToolName: "Read", Summary: "config.go", Status: ToolOK, Result: []string{"42 lines"}}
				return stripANSI(renderToolCalls([]ToolCallView{c}, true, 40))
			},
			want:   []string{"○ Read(config.go)", "│ 42 lines"},
			absent: []string{"⎿"},
		},
		{
			name: "error card still renders its header and result text",
			render: func() string {
				c := ToolCallView{ToolName: "Bash", Summary: "boom", Status: ToolError, Result: []string{"exit 1"}}
				return stripANSI(renderToolCalls([]ToolCallView{c}, true, 40))
			},
			want:   []string{"○ Bash(boom)", "│ exit 1"},
			absent: []string{"⎿"},
		},
		{
			name: "running card on the live path is header-only with the pulsing node",
			render: func() string {
				return stripANSI(strings.Join(renderToolNode(
					ToolCallView{ToolName: "Fetch", Status: ToolRunning}, 0, false, 40, true), "\n"))
			},
			want:   []string{"◍ Fetch"},
			absent: []string{"⎿", noOutput, "│ "}, // header-only: no detail rows
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.render()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("render = %q, want to contain %q", got, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("render = %q, want to NOT contain %q", got, a)
				}
			}
		})
	}
}

// TestToolNodeHeaderTextNormalizesAuditableSummaries covers live tool events whose
// AuditSummary already includes the tool name. The node header owns the tool name,
// so the argument display should not duplicate it.
func TestToolNodeHeaderTextNormalizesAuditableSummaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call ToolCallView
		want string
	}{
		{
			name: "bash colon prefix",
			call: ToolCallView{ToolName: "Bash", Summary: "Bash: curl -p https://example.com"},
			want: "Bash(curl -p https://example.com)",
		},
		{
			name: "readfile space prefix",
			call: ToolCallView{ToolName: "ReadFile", Summary: "ReadFile pkg/tui/render.go"},
			want: "ReadFile(pkg/tui/render.go)",
		},
		{
			name: "fetch summary without tool prefix",
			call: ToolCallView{ToolName: "Fetch", Summary: "GET google.com"},
			want: "Fetch(GET google.com)",
		},
		{
			name: "summary equal to tool name is omitted",
			call: ToolCallView{ToolName: "Subagent", Summary: "Subagent"},
			want: "Subagent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := toolNodeHeaderText(tt.call); got != tt.want {
				t.Errorf("toolNodeHeaderText() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRenderToolCalls covers card rendering: glyphs, collapsed vs expanded preview,
// the truncation marker, (no output), error-always-shown, multi-card batches, and
// width wrapping (design §3).
func TestRenderToolCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		calls       []ToolCallView
		expandTools bool
		width       int
		want        []string // substrings that must appear
		absent      []string // substrings that must NOT appear
	}{
		{
			name:  "running card shows the pulsing node glyph and name+summary",
			calls: []ToolCallView{{ToolName: "ReadFile", Summary: "config.yaml", Status: ToolRunning}},
			width: 80,
			// Committed path (liveRunning=false): a running card maps to the pulsing "◍" node.
			want: []string{"ReadFile", "config.yaml", "◍"},
		},
		{
			name:  "ok card shows the hollow node glyph",
			calls: []ToolCallView{{ToolName: "ReadFile", Status: ToolOK}},
			width: 80,
			// Color (faint vs red) is asserted by TestToolNodeStatus; stripANSI is color-blind,
			// so both OK and failed render the same "○" glyph here.
			want: []string{"○"},
		},
		{
			name:  "error card shows the failed node glyph and its result",
			calls: []ToolCallView{{ToolName: "Bash", Status: ToolError, Result: []string{"boom"}}},
			width: 80,
			want:  []string{"○", "boom"},
		},
		{
			name:  "cancelled card shows the failed node glyph",
			calls: []ToolCallView{{ToolName: "Bash", Status: ToolCancelled}},
			width: 80,
			want:  []string{"○"},
		},
		{
			name:        "result over the cap is trimmed with a more-marker (no ctrl+t)",
			calls:       []ToolCallView{{ToolName: "ReadFile", Status: ToolOK, Result: makeLines(10)}},
			expandTools: false,
			width:       80,
			// HARD cap = previewLineCap (3) → lines 0..2 shown, 3..9 hidden, "7 more" marker.
			want:   []string{"line0", "line2", "7 more lines"},
			absent: []string{"line3", "line9", "ctrl+t"},
		},
		{
			name:        "expanded still hard-caps the tool result (ignores expand)",
			calls:       []ToolCallView{{ToolName: "ReadFile", Status: ToolOK, Result: makeLines(10)}},
			expandTools: true,
			width:       80,
			want:        []string{"line0", "line2", "7 more lines"},
			absent:      []string{"line3", "line9"},
		},
		{
			name:        "exactly cap lines shows all with no marker",
			calls:       []ToolCallView{{ToolName: "ReadFile", Status: ToolOK, Result: makeLines(previewLineCap)}},
			expandTools: false,
			width:       80,
			want:        []string{"line0"},
			absent:      []string{"more lines"},
		},
		{
			name:   "empty result shows (no output)",
			calls:  []ToolCallView{{ToolName: "Noop", Status: ToolOK, Result: nil}},
			width:  80,
			want:   []string{noOutput},
			absent: []string{"more lines"},
		},
		{
			name:        "error card shows its result even collapsed",
			calls:       []ToolCallView{{ToolName: "Bash", Status: ToolError, Result: []string{"error: permission denied"}}},
			expandTools: false,
			width:       80,
			want:        []string{"○", "error: permission denied"},
		},
		{
			name: "parallel batch renders all cards",
			calls: []ToolCallView{
				{ToolName: "ReadFile", Summary: "a.go", Status: ToolOK, Result: []string{"alpha"}},
				{ToolName: "Bash", Summary: "ls", Status: ToolOK, Result: []string{"beta"}},
			},
			width: 80,
			want:  []string{"ReadFile", "Bash", "alpha", "beta"},
		},
		{
			name:  "no calls renders empty",
			calls: nil,
			width: 80,
		},
		{
			name:        "long result line is width-wrapped",
			calls:       []ToolCallView{{ToolName: "Bash", Status: ToolOK, Result: []string{"aaaa bbbb cccc dddd eeee ffff gggg"}}},
			expandTools: true,
			width:       20,
			// At width 20 the line cannot fit on one row → at least one wrap newline.
			want: []string{"aaaa", "gggg"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := renderToolCalls(tt.calls, tt.expandTools, tt.width)
			if len(tt.calls) == 0 {
				if got != "" {
					t.Errorf("renderToolCalls(nil) = %q, want empty", got)
				}
				return
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("renderToolCalls() = %q, want to contain %q", got, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("renderToolCalls() = %q, want to NOT contain %q", got, a)
				}
			}
		})
	}
}

// TestRenderToolCallsWidthWrap asserts a long result line actually breaks onto
// multiple display rows when the width is too small to hold it.
func TestRenderToolCallsWidthWrap(t *testing.T) {
	t.Parallel()

	calls := []ToolCallView{{ToolName: "Bash", Status: ToolOK, Result: []string{"aaaa bbbb cccc dddd eeee ffff gggg hhhh"}}}
	narrow := renderToolCalls(calls, true, 16)
	wide := renderToolCalls(calls, true, 200)

	narrowRows := strings.Count(narrow, "\n")
	wideRows := strings.Count(wide, "\n")
	if narrowRows <= wideRows {
		t.Errorf("narrow render rows = %d, wide render rows = %d; want narrow to wrap into more rows", narrowRows, wideRows)
	}
}

// TestRenderAssistantNestsCards covers a committed assistant segment under the
// node-presence rule: narration renders the "●" bullet and never the old "⎿" card
// connector (committed tool calls are their OWN kindTool rail nodes, not inline here);
// a thinking-only segment renders just its "│" rail with no bullet; a fully empty
// segment renders nothing. This exercises the renderAssistant primitive that the
// kindAssistant entry render (entryrender.go) drives.
func TestRenderAssistantNestsCards(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		thinking string
		text     string
		want     []string
		absent   []string
	}{
		{
			name: "narration renders the dot bullet, no card connector",
			text: "let me read the config",
			want: []string{strings.TrimSpace(styles.Dot), "let me read the config"},
			// committed cards are their OWN kindTool entries — never nested here.
			absent: []string{cardConnector},
		},
		{
			// A committed thinking-only segment renders the rail'd header "│ thought"
			// (formatThought(0), no captured duration) with no assistant bullet.
			name:     "thinking only renders the rail with no bullet",
			thinking: "mulling it over",
			want:     []string{"│ thought"},
			absent:   []string{strings.TrimSpace(styles.Dot)},
		},
		{
			name:   "fully empty renders nothing",
			absent: []string{strings.TrimSpace(styles.Dot)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stripANSI(renderAssistant(tt.thinking, tt.text, false, 80, formatThought(0)))
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("renderAssistant() = %q, want to contain %q", got, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("renderAssistant() = %q, want to NOT contain %q", got, a)
				}
			}
		})
	}
}

// TestRenderLiveAssistantCards covers the in-progress (LIVE) segment rendering its
// text then its tool cards (a running card shows the running glyph header-only), and a
// card-only segment with empty text rendering the working-word bullet plus its cards.
// This is the path screen.go's renderLiveTail drives.
func TestRenderLiveAssistantCards(t *testing.T) {
	t.Parallel()

	a := animState{}

	t.Run("text plus running card", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{{ToolName: "Bash", Summary: "ls", Status: ToolRunning}}
		got := stripANSI(renderLiveAssistant("", "checking now", calls, nil, false, 80, a))
		for _, w := range []string{"checking now", "Bash", "ls"} {
			if !strings.Contains(got, w) {
				t.Errorf("renderLiveAssistant() = %q, want to contain %q", got, w)
			}
		}
	})

	t.Run("cards with empty text render the running node directly", func(t *testing.T) {
		t.Parallel()

		// Node presence: empty text → no "●" bullet umbrella; the running call shows its
		// pulsing "◍" node directly.
		calls := []ToolCallView{{ToolName: "Bash", Status: ToolRunning}}
		got := stripANSI(renderLiveAssistant("", "", calls, nil, true, 80, a))
		for _, w := range []string{"◍", "Bash"} {
			if !strings.Contains(got, w) {
				t.Errorf("renderLiveAssistant() = %q, want to contain %q", got, w)
			}
		}
		if strings.Contains(got, strings.TrimSpace(styles.Dot)) {
			t.Errorf("renderLiveAssistant() = %q, must not show the %q bullet umbrella for a tools-only step", got, styles.Dot)
		}
	})

	t.Run("many calls keep only the most recent, with an earlier-calls marker", func(t *testing.T) {
		t.Parallel()

		calls := make([]ToolCallView, 0, liveCallCap+3)
		for i := 0; i < liveCallCap+3; i++ {
			calls = append(calls, ToolCallView{ToolName: "Bash", Summary: "cmd" + strconv.Itoa(i), Status: ToolOK, Result: []string{"out"}})
		}
		// The narration ("the answer") is the top content the assistant bullet anchors.
		got := stripANSI(renderLiveAssistant("", "the answer", calls, nil, false, 80, a))

		hidden := len(calls) - liveCallCap
		if !strings.Contains(got, "… "+strconv.Itoa(hidden)+" earlier calls") {
			t.Errorf("missing '… %d earlier calls' marker in %q", hidden, got)
		}
		if !strings.Contains(got, "the answer") {
			t.Errorf("top narration dropped; the bullet must stay anchored: %q", got)
		}
		if strings.Contains(got, "cmd0") { // oldest cards are elided
			t.Errorf("oldest card cmd0 should be elided in %q", got)
		}
		if last := "cmd" + strconv.Itoa(len(calls)-1); !strings.Contains(got, last) { // newest shows
			t.Errorf("most recent card %q missing in %q", last, got)
		}
	})
}

// TestRenderLiveAssistant_Rail covers the LIVE tail brought to rail parity with the
// committed path (Task 8): the AI-message "●"/"◦" bullet under the node-presence rule, the
// running tool call as a pulsing "◍" rail node, resolved calls as "○" nodes with "│ "-railed
// detail rows, the over-cap elision as a rail detail row — and NEVER the old "⎿" connector
// or a working-word umbrella.
func TestRenderLiveAssistant_Rail(t *testing.T) {
	t.Parallel()

	hasBullet := func(s string) bool { return strings.Contains(s, "●") || strings.Contains(s, "◦") }

	t.Run("text plus running and resolved calls use the rail", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{
			{ToolName: "Bash", Summary: "ls", Status: ToolRunning},
			{ToolName: "Read", Summary: "main.go", Status: ToolOK, Result: []string{"package main"}},
		}
		got := stripANSI(renderLiveAssistant("", "checking now", calls, nil, true, 80, animState{}))
		if !hasBullet(got) {
			t.Errorf("live step %q missing the blinking assistant bullet (● / ◦)", got)
		}
		if !strings.Contains(got, "◍") {
			t.Errorf("live step %q missing the pulsing running node ◍", got)
		}
		if !strings.Contains(got, "○") {
			t.Errorf("live step %q missing the resolved hollow node ○", got)
		}
		if !strings.Contains(got, "│ ") {
			t.Errorf("live step %q missing the │ rail (resolved node detail rows)", got)
		}
		if strings.Contains(got, "⎿") {
			t.Errorf("live step %q must not carry the old ⎿ card connector", got)
		}
	})

	t.Run("empty text with a running call shows no working-word umbrella", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{{ToolName: "Bash", Summary: "ls", Status: ToolRunning}}
		got := stripANSI(renderLiveAssistant("", "", calls, nil, true, 80, animState{frame: 3}))
		for _, w := range workingWords {
			if strings.Contains(got, w) {
				t.Errorf("live tools-only step %q must not show a working-word (%q)", got, w)
			}
		}
		if hasBullet(got) {
			t.Errorf("live tools-only step %q must not show the ●/◦ bullet umbrella (no text → no bullet node)", got)
		}
		if !strings.Contains(got, "◍") {
			t.Errorf("live tools-only step %q missing the running node ◍", got)
		}
	})

	t.Run("more than the cap shows a rail earlier-calls row", func(t *testing.T) {
		t.Parallel()

		calls := make([]ToolCallView, 0, liveCallCap+2)
		for i := 0; i < liveCallCap+2; i++ {
			calls = append(calls, ToolCallView{ToolName: "Bash", Summary: "cmd" + strconv.Itoa(i), Status: ToolOK, Result: []string{"out"}})
		}
		got := stripANSI(renderLiveAssistant("", "", calls, nil, true, 80, animState{}))
		hidden := len(calls) - liveCallCap
		if !strings.Contains(got, "… ") || !strings.Contains(got, "earlier calls") {
			t.Errorf("live step %q missing the '… %d earlier calls' rail row", got, hidden)
		}
		if strings.Contains(got, "⎿") {
			t.Errorf("live step %q must not carry the old ⎿ connector on the earlier-calls row", got)
		}
	})

	t.Run("resolved call shows its node with detail rows", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{{ToolName: "Bash", Summary: "ls", Status: ToolOK, Result: []string{"file-a", "file-b"}}}
		got := stripANSI(renderLiveAssistant("", "", calls, nil, true, 80, animState{}))
		if !strings.Contains(got, "○") {
			t.Errorf("resolved live node %q missing the hollow ○ node glyph", got)
		}
		for _, w := range []string{"file-a", "file-b"} {
			if !strings.Contains(got, w) {
				t.Errorf("resolved live node %q missing detail row %q (not header-only)", got, w)
			}
		}
	})
}

// TestRenderLiveAssistant_RailConnectors covers the live tail's inter-node "│" connector
// rows (Task 8 follow-up): a bare "│" connector joins the "●" AI-message body to the first
// tool node and separates consecutive tool nodes, so the live rail is continuous like the
// committed path — WITHOUT doubling the rail after a thinking block (whose own trailing "│ "
// already connects into the first node) or before a tools-only step's first node.
func TestRenderLiveAssistant_RailConnectors(t *testing.T) {
	t.Parallel()

	// A bare connector row is exactly "│" (no trailing space); a thinking trailing rail
	// line is "│ " and a detail row is "│ <text>", so this identifies connectors only.
	strippedLines := func(s string) []string {
		lines := strings.Split(s, "\n")
		for i := range lines {
			lines[i] = stripANSI(lines[i])
		}
		return lines
	}
	countConnectors := func(lines []string) int {
		n := 0
		for _, l := range lines {
			if l == "│" {
				n++
			}
		}
		return n
	}
	firstNodeIdx := func(lines []string) int {
		for i, l := range lines {
			if strings.Contains(l, "◍") || strings.Contains(l, "○") {
				return i
			}
		}
		return -1
	}

	t.Run("text plus two tools has connectors between body and nodes", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{
			{ToolName: "Read", Summary: "config.go", Status: ToolOK, Result: []string{"42 lines"}},
			{ToolName: "Bash", Summary: "go test", Status: ToolOK, Result: []string{"ok"}},
		}
		lines := strippedLines(renderLiveAssistant("", "the answer", calls, nil, true, 80, animState{}))

		// Exactly two bare connectors: ● body → node1, and node1 → node2.
		if got := countConnectors(lines); got != 2 {
			t.Errorf("text + 2 tools: got %d '│' connector rows, want 2\nlines: %#v", got, lines)
		}
		// The connector sits immediately after the "●" body line.
		for i, l := range lines {
			if strings.Contains(l, "●") {
				if i+1 >= len(lines) || lines[i+1] != "│" {
					t.Errorf("no '│' connector directly after the ● body line\nlines: %#v", lines)
				}
				break
			}
		}
	})

	t.Run("thinking plus tools (no text) does not double the rail before first node", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{
			{ToolName: "Read", Summary: "config.go", Status: ToolOK, Result: []string{"42 lines"}},
			{ToolName: "Bash", Summary: "go test", Status: ToolOK, Result: []string{"ok"}},
		}
		lines := strippedLines(renderLiveAssistant("weighing options", "", calls, nil, true, 80, animState{}))

		idx := firstNodeIdx(lines)
		if idx < 1 {
			t.Fatalf("first tool node not found (or has no preceding line)\nlines: %#v", lines)
		}
		// The line before the first node is the thinking block's trailing "│ " rail line —
		// NOT an added bare "│" connector (that would double the rail).
		if lines[idx-1] == "│" {
			t.Errorf("rail doubled: a bare '│' connector precedes the first node after thinking\nlines: %#v", lines)
		}
		// Only ONE bare connector overall: between the two tool nodes, none before the first.
		if got := countConnectors(lines); got != 1 {
			t.Errorf("thinking + 2 tools: got %d '│' connector rows, want 1 (only between nodes)\nlines: %#v", got, lines)
		}
	})

	t.Run("tools only (no thinking, no text) has no leading connector", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{
			{ToolName: "Read", Summary: "config.go", Status: ToolOK, Result: []string{"42 lines"}},
			{ToolName: "Bash", Summary: "go test", Status: ToolOK, Result: []string{"ok"}},
		}
		lines := strippedLines(renderLiveAssistant("", "", calls, nil, true, 80, animState{}))

		if idx := firstNodeIdx(lines); idx != 0 {
			t.Errorf("tools-only step: first node at line %d, want 0 (no leading connector)\nlines: %#v", idx, lines)
		}
		if got := countConnectors(lines); got != 1 {
			t.Errorf("tools-only step: got %d '│' connector rows, want 1 (only between the two nodes)\nlines: %#v", got, lines)
		}
	})
}

func TestRenderLiveAssistantExpandedThinkingShowsFullBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		lineCnt  int
		first    string
		middle   string
		last     string
		expanded bool
	}{
		{
			name:     "expanded long thinking keeps earliest and latest lines",
			lineCnt:  12,
			first:    "reason-00",
			middle:   "reason-04",
			last:     "reason-11",
			expanded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines := make([]string, tt.lineCnt)
			for i := range lines {
				lines[i] = "reason-" + strconv.Itoa(i/10) + strconv.Itoa(i%10)
			}
			got := stripANSI(renderLiveAssistant(strings.Join(lines, "\n"), "", nil, nil, tt.expanded, 80, animState{}))

			for _, want := range []string{tt.first, tt.last} {
				if !strings.Contains(got, want) {
					t.Errorf("renderLiveAssistant() = %q, want to contain %q", got, want)
				}
			}
			if strings.HasPrefix(strings.TrimSpace(got), "│ …") {
				t.Errorf("renderLiveAssistant() = %q, want no live thinking tail truncation marker", got)
			}
			if tt.middle != "" && !strings.Contains(got, tt.middle) {
				t.Errorf("renderLiveAssistant() = %q, want middle thinking line %q to remain visible", got, tt.middle)
			}
		})
	}
}

// TestRenderLiveAssistantSubagentCard (live-tail card path): a pending Subagent card in
// the live tail renders the SAME nested "● Subagent(<agent>)" card as the committed form
// (header + ⎿ children + "running · N steps"), and — because the only activity in the
// step is the spawned subagent (no ordinary calls) — does NOT show a rotating
// working-word headline.
func TestRenderLiveAssistantSubagentCard(t *testing.T) {
	t.Parallel()

	subagentCards := []ToolCallView{{
		ToolName: "Subagent", Agent: "explorer", Task: "map repo",
		Children:  []ToolCallView{{ToolName: "Glob", Status: ToolOK}},
		Steps:     1,
		SubStatus: subRunning,
	}}
	got := stripANSI(renderLiveAssistant("", "", nil, subagentCards, false, 80, animState{}))

	for _, w := range []string{"Subagent(explorer)", "map repo", "Glob", "running" + hintSeparator + "1 step"} {
		if !strings.Contains(got, w) {
			t.Errorf("live subagent card = %q, want to contain %q", got, w)
		}
	}
	// No working-word: the step only spawned a subagent, so there is no ordinary
	// card-only headline.
	for _, w := range workingWords {
		if strings.Contains(got, w) {
			t.Errorf("live subagent card = %q, must NOT show a working-word (%q)", got, w)
		}
	}
}

// TestRenderLiveAssistantSuppressesRawSubagentCall (live-tail suppression): while a
// subagent streams, the orchestrator's own live tool list carries a raw running
// "Subagent(Subagent)" tool card. That raw call must be SUPPRESSED in the live tail (it
// is replaced by the nested pending card), so the tail shows the nested card and not the
// doubled raw Subagent tool row.
func TestRenderLiveAssistantSuppressesRawSubagentCall(t *testing.T) {
	t.Parallel()

	// nonSubagentCalls is what renderLiveTail passes for `calls`: the raw Subagent call is
	// filtered out before reaching renderLiveAssistant.
	rawCalls := []ToolCallView{{ToolName: "Subagent", Summary: "Subagent", Status: ToolRunning}}
	filtered := nonSubagentCalls(rawCalls)
	if len(filtered) != 0 {
		t.Fatalf("nonSubagentCalls dropped %d of %d raw Subagent calls, want all filtered", len(rawCalls)-len(filtered), len(rawCalls))
	}

	pending := []ToolCallView{{
		ToolName: "Subagent", Agent: "explorer", Task: "map repo",
		Children:  []ToolCallView{{ToolName: "Glob", Status: ToolOK}},
		Steps:     1,
		SubStatus: subRunning,
	}}
	got := stripANSI(renderLiveAssistant("", "", filtered, pending, false, 80, animState{}))

	if strings.Contains(got, "Subagent(Subagent)") {
		t.Errorf("live tail shows the raw Subagent(Subagent) tool card; want it suppressed: %q", got)
	}
	if !strings.Contains(got, "Subagent(explorer)") {
		t.Errorf("live tail missing the nested Subagent(explorer) card: %q", got)
	}
}

// TestRenderLiveRunningCardIsHeaderOnly locks the live→committed handoff fix
// (Option B): a still-RUNNING tool card in the LIVE tail renders as a SINGLE compact
// header line (spinner + tool name + summary) with NO result body — not the
// "(no output)" placeholder a committed/resolved card carries. This minimises the
// live-tail height that must be removed when the card commits to scrollback, so the
// running→completed handoff composes cleanly (the committed full card replaces a
// one-line live indicator, not a multi-line live card). Resolved cards co-resident in
// the live tail (a batch sibling that finished but hasn't committed yet) keep their
// full body, and the committed scrollback path is unchanged (full card always).
func TestRenderLiveRunningCardIsHeaderOnly(t *testing.T) {
	t.Parallel()

	a := animState{}

	t.Run("running card is one header line, no body", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{{ToolName: "Fetch", Summary: "GET weather.com", Status: ToolRunning}}
		got := stripANSI(renderLiveAssistant("", "", calls, nil, true, 80, a))
		// Node presence: empty text → no "●" bullet line; the running node is the sole line.
		lines := strings.Split(got, "\n")
		if len(lines) != 1 {
			t.Fatalf("live running node: got %d lines %q, want 1 (header-only node, no bullet)", len(lines), lines)
		}
		card := lines[0]
		for _, w := range []string{"◍", "Fetch", "GET weather.com"} {
			if !strings.Contains(card, w) {
				t.Errorf("live running node = %q, want to contain %q", card, w)
			}
		}
		if strings.Contains(got, noOutput) {
			t.Errorf("live running node must NOT show the %q body placeholder; got %q", noOutput, got)
		}
	})

	t.Run("resolved card in live tail keeps its body", func(t *testing.T) {
		t.Parallel()

		// A finished batch sibling that has not yet committed must still show its
		// result in the live tail (it is NOT a running card).
		calls := []ToolCallView{{
			ToolName: "Bash", Summary: "ls", Status: ToolOK, Result: []string{"file-a", "file-b"},
		}}
		got := stripANSI(renderLiveAssistant("", "", calls, nil, true, 80, a))
		for _, w := range []string{"Bash", "ls", "file-a", "file-b"} {
			if !strings.Contains(got, w) {
				t.Errorf("resolved live card = %q, want to contain %q", got, w)
			}
		}
	})

	t.Run("committed running card (defensive) keeps full body", func(t *testing.T) {
		t.Parallel()

		// The committed/scrollback path renders the full card regardless of status;
		// a (stray) running card committed at a terminal still shows its body.
		calls := []ToolCallView{{ToolName: "Fetch", Summary: "GET x", Status: ToolRunning}}
		got := stripANSI(renderToolCalls(calls, true, 80))
		if !strings.Contains(got, noOutput) {
			t.Errorf("committed running card should still show %q body; got %q", noOutput, got)
		}
	})
}

// TestRenderThinking covers the dim reasoning block under the unified ctrl+t flag, behind
// its caller-supplied header. COLLAPSED: a SINGLE rail'd line "│ <header>" — no reasoning
// body, no "· N lines · ctrl+t" summary. EXPANDED: that same "│ <header>" line followed by
// the "│ "-railed reasoning — an unbroken left rail. The header is the LIVE tail's
// present-tense "thinking" or a COMMITTED entry's "thought for Ns" / "thought". Empty or
// whitespace-only input renders nothing in either mode.
func TestRenderThinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           string
		header       string
		expand       bool
		wantContains []string
		wantAbsent   []string
		wantEmpty    bool
	}{
		{name: "empty renders nothing collapsed", in: "", header: "thought", expand: false, wantEmpty: true},
		{name: "empty renders nothing expanded", in: "", header: "thought", expand: true, wantEmpty: true},
		{name: "whitespace renders nothing collapsed", in: "   \n  ", header: "thought", expand: false, wantEmpty: true},
		{name: "whitespace renders nothing expanded", in: "   \n  ", header: "thought", expand: true, wantEmpty: true},
		{
			// COLLAPSED committed: the rail'd header "│ thought for 10s" + the blank "│" rail
			// gap beneath it — but NO reasoning body, no "· N lines · ctrl+t" summary, no bare
			// (rail-less) header.
			name:         "collapsed committed is the rail'd header plus its gap",
			in:           "line one\nline two",
			header:       "thought for 10s",
			expand:       false,
			wantContains: []string{"│ thought for 10s"},
			wantAbsent:   []string{"│ line one", "│ line two", "ctrl+t", "lines"},
		},
		{
			// COLLAPSED live: the present-tense header on the rail — "│ thinking".
			name:         "collapsed live is a single rail'd thinking line",
			in:           "line one\nline two",
			header:       "thinking",
			expand:       false,
			wantContains: []string{"│ thinking"},
			wantAbsent:   []string{"│ line one", "· ", "ctrl+t"},
		},
		{
			// EXPANDED: the header carries the rail ("│ thought for 10s", not bare) and
			// every body line carries the rail too — an unbroken left rail, with a blank
			// rail row padding the header off the body.
			name:         "expanded rails every line including the header",
			in:           "line one\nline two",
			header:       "thought for 10s",
			expand:       true,
			wantContains: []string{"│ thought for 10s", "│ line one", "│ line two"},
			wantAbsent:   []string{"\nthought", "more lines"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := stripANSI(renderThinking(tt.in, tt.expand, 80, tt.header))
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("renderThinking(%q, %v) = %q, want empty", tt.in, tt.expand, got)
				}
				return
			}
			for _, w := range tt.wantContains {
				if !strings.Contains(got, w) {
					t.Errorf("renderThinking(%q, %v) = %q, want to contain %q", tt.in, tt.expand, got, w)
				}
			}
			for _, a := range tt.wantAbsent {
				if strings.Contains(got, a) {
					t.Errorf("renderThinking(%q, %v) = %q, want to NOT contain %q", tt.in, tt.expand, got, a)
				}
			}
		})
	}
}

// TestRenderThinkingHeaderGapAlways pins that the blank "│" rail gap under the header is present
// in BOTH the collapsed and expanded forms — the gap stays put when the block is expanded, it is
// not an expand-only affordance. In each form the second rendered row is the bare rail gap
// (rail, no content), directly beneath the "│ <header>" line.
func TestRenderThinkingHeaderGapAlways(t *testing.T) {
	t.Parallel()

	const rail = "│ "
	for _, expand := range []bool{false, true} {
		expand := expand
		name := "collapsed"
		if expand {
			name = "expanded"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(stripANSI(renderThinking("weighing\noptions", expand, 80, "thought for 3s")), "\n")
			if len(lines) < 2 {
				t.Fatalf("%s thinking = %q, want at least the header + gap rows", name, lines)
			}
			if got, want := lines[0], rail+"thought for 3s"; got != want {
				t.Errorf("%s header row = %q, want %q", name, got, want)
			}
			// The row under the header is the bare rail gap: the rail with no content.
			if strings.TrimRight(lines[1], " ") != strings.TrimRight(rail, " ") {
				t.Errorf("%s gap row = %q, want the blank rail %q under the header", name, lines[1], rail)
			}
		})
	}
}

// TestRenderThinkingExpandedRailOnEveryLine asserts the expanded thinking block is
// an UNBROKEN left rail: every rendered line — the header, the blank pad row, each body line,
// AND the trailing gap — begins with the "│ " rail in the same column, so the block reads as a
// sub-block attached to the assistant turn. No line (not even the header) is left bare. The
// header is padded off the body by a blank rail row, and a trailing blank rail row sets the
// body off from the AI message below — the block is gapped top and bottom.
func TestRenderThinkingExpandedRailOnEveryLine(t *testing.T) {
	t.Parallel()

	const rail = "│ "
	got := stripANSI(renderThinking("line one\nline two\nline three", true, 80, styles.ThinkingHeader))
	lines := strings.Split(got, "\n")
	if len(lines) < 6 { // header + pad gap + three body lines + trailing gap
		t.Fatalf("expanded thinking = %q, want at least 6 lines (header + pad + body + trailing gap)", got)
	}
	if got, want := lines[0], rail+styles.ThinkingHeader; got != want {
		t.Errorf("header line = %q, want %q (rail on the header, not bare)", got, want)
	}
	// The row right below the header is the blank rail pad (rail, no content) — one line of
	// breathing space between the header and the reasoning body.
	if got, want := strings.TrimRight(lines[1], " "), strings.TrimRight(rail, " "); got != want {
		t.Errorf("pad line = %q, want the blank rail %q below the header", lines[1], rail)
	}
	if got := lines[2]; !strings.HasPrefix(got, rail) || strings.TrimSpace(got) == strings.TrimSpace(rail) {
		t.Errorf("first body line = %q, want the reasoning body (rail + content) after the pad", got)
	}
	// The LAST row is the trailing rail gap (rail, no content) — the gap between the reasoning
	// body and the AI message that follows.
	if last := lines[len(lines)-1]; strings.TrimRight(last, " ") != strings.TrimRight(rail, " ") {
		t.Errorf("trailing line = %q, want the blank rail %q below the body", last, rail)
	}
	for i, ln := range lines {
		if !strings.HasPrefix(ln, rail) {
			t.Errorf("line %d = %q, want it to start with the rail %q (unbroken rail)", i, ln, rail)
		}
	}
}

// TestRenderAssistantUnifiedExpand covers Task 12: ONE flag drives BOTH the
// thinking block and the tool-result folding. Collapsed (expand=false): thinking
// renders as a single rail'd header line "│ thought" (no "│ " body) AND the long tool
// result is folded (first K lines + "more lines" marker). Expanded (expand=true): the
// full "│ "-railed thinking body renders AND the tool result shows every line. The
// SAME flag flips both — there is no separate thinking key.
func TestExpandFoldsThinkingNotToolOutput(t *testing.T) {
	t.Parallel()

	const thinking = "reason one\nreason two\nreason three"
	calls := []ToolCallView{{ToolName: "ReadFile", Status: ToolOK, Result: makeLines(10)}}

	// The thinking fold (renderAssistant) honors the ctrl+t expand flag. The tool-result
	// preview (renderToolCalls) is HARD-capped to previewLineCap lines regardless of expand,
	// so a huge result can never fill the live tail or strand a commit-time gap.
	thinkCollapsed := stripANSI(renderAssistant(thinking, "the answer", false, 80, formatThought(0)))
	thinkExpanded := stripANSI(renderAssistant(thinking, "the answer", true, 80, formatThought(0)))
	toolCollapsed := stripANSI(renderToolCalls(calls, false, 80))
	toolExpanded := stripANSI(renderToolCalls(calls, true, 80))

	// Thinking DOES flip: collapsed is the single rail'd header "│ thought" (no body);
	// expanded is that header PLUS the full "│ "-railed body.
	if !strings.Contains(thinkCollapsed, "│ thought") {
		t.Errorf("collapsed thinking missing the rail'd header in %q", thinkCollapsed)
	}
	if strings.Contains(thinkCollapsed, "│ reason one") {
		t.Errorf("collapsed thinking must NOT show the body in %q", thinkCollapsed)
	}
	for _, w := range []string{"│ thought", "│ reason one", "│ reason three"} {
		if !strings.Contains(thinkExpanded, w) {
			t.Errorf("expanded thinking missing %q in %q", w, thinkExpanded)
		}
	}

	// Tool output does NOT flip: BOTH expand states hard-cap to previewLineCap (3) lines
	// with a "… N more lines" marker; later lines never show, and there is no ctrl+t hint.
	for _, label := range []struct {
		name string
		out  string
	}{{"collapsed", toolCollapsed}, {"expanded", toolExpanded}} {
		for _, w := range []string{"line0", "line2", "more lines"} {
			if !strings.Contains(label.out, w) {
				t.Errorf("%s tool missing %q in %q", label.name, w, label.out)
			}
		}
		for _, a := range []string{"line3", "line9", "ctrl+t"} {
			if strings.Contains(label.out, a) {
				t.Errorf("%s tool must NOT contain %q in %q", label.name, a, label.out)
			}
		}
	}
}

// TestRenderAssistantThinkingBlock covers an assistant segment carrying reasoning:
// when expanded the reasoning renders as the full thinking block (never as
// "[unsupported block]") and the narration still renders. It exercises renderAssistant
// the way the kindAssistant entry render feeds it (thinkingText + assistantText).
func TestRenderAssistantThinkingBlock(t *testing.T) {
	t.Parallel()

	got := stripANSI(renderAssistant("my reasoning", "the final answer", true, 80, formatThought(0))) // expanded

	for _, w := range []string{"│ thought", "│ my reasoning", "the final answer"} {
		if !strings.Contains(got, w) {
			t.Errorf("renderAssistant() = %q, want to contain %q", got, w)
		}
	}
	if strings.Contains(got, "[unsupported block]") {
		t.Errorf("renderAssistant() = %q, must not render reasoning as [unsupported block]", got)
	}
}

// TestRenderAssistant_NodePresence pins the node-presence rule: the thinking rail
// renders iff the segment reasoned; the neon "●" AI-message node renders iff the
// narration text is non-empty; empty text yields no "●" node and NEVER a "Multiple
// actions" umbrella. It covers content-only, thinking-only, thinking+empty-text, and
// the all-three case.
func TestRenderAssistant_NodePresence(t *testing.T) {
	t.Parallel()

	dot := strings.TrimSpace(styles.Dot) // the "●" AI-message node glyph

	tests := []struct {
		name     string
		thinking string
		text     string
		want     []string
		absent   []string
	}{
		{
			name:   "content only renders the ● node, no thinking rail",
			text:   "hi",
			want:   []string{dot, "hi"},
			absent: []string{"│ thought", "Multiple actions"},
		},
		{
			name:     "thinking only renders the rail, no ● node",
			thinking: "mulling",
			want:     []string{"│"},
			absent:   []string{dot, "Multiple actions"},
		},
		{
			name:     "thinking plus empty text renders the rail only, no ● node",
			thinking: "mulling",
			text:     "",
			want:     []string{"│ thought"},
			absent:   []string{dot, "Multiple actions"},
		},
		{
			name:     "thinking and text renders both the rail and the ● node",
			thinking: "mulling",
			text:     "the answer",
			want:     []string{"│ thought", dot, "the answer"},
			absent:   []string{"Multiple actions"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := stripANSI(renderAssistant(tt.thinking, tt.text, false, 80, formatThought(0)))
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("renderAssistant() = %q, want to contain %q", got, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("renderAssistant() = %q, want to NOT contain %q", got, a)
				}
			}
		})
	}
}

// TestRenderEntrySubagentCard covers the committed Subagent card render (Task 6 nested
// rail): a kindTool entry whose ToolCallView has Agent set renders as an "○" header node
// "Subagent(<agent>)  \"<task>\"" opening a nested rail, its children as depth-1 "│ ○"
// nodes, and a closing "│ ○ done · N steps — \"<summary>\"" node. The card's own Result
// (the done summary) must appear ONLY in that closing node, never also as a separate
// result body (no doubling). The "+M nested subagent steps" line shows only when
// Nested > 0.
func TestRenderEntrySubagentCard(t *testing.T) {
	t.Parallel()

	e := entry{
		Kind: kindTool,
		Calls: []ToolCallView{{
			ToolName:  "Subagent",
			Agent:     "explorer",
			Task:      "map repo",
			Steps:     6,
			SubStatus: subDone,
			Result:    []string{"found 12 packages"},
			Children: []ToolCallView{
				{ToolName: "Grep", Status: ToolOK, Result: []string{"hit"}},
				{ToolName: "Read", Status: ToolOK, Result: []string{"contents"}},
			},
		}},
	}
	got := stripANSI(strings.Join(renderEntry(e, false, 100), "\n"))

	// Header: the "○" node, the standard tool-card "Subagent(explorer)" form, and the
	// task in quotes.
	for _, w := range []string{"○ Subagent(explorer)", `"map repo"`} {
		if !strings.Contains(got, w) {
			t.Errorf("subagent card = %q, want %q", got, w)
		}
	}
	// Two child nodes under the header, each at depth 1 (a "│ ○" node).
	for _, w := range []string{"│ ○ Grep", "│ ○ Read"} {
		if !strings.Contains(got, w) {
			t.Errorf("subagent card = %q, want the depth-1 child node %q", got, w)
		}
	}
	// The closing done node: verb + step count + summary at depth 1.
	if !strings.Contains(got, `│ ○ done · 6 steps — "found 12 packages"`) {
		t.Errorf("subagent card = %q, want the closing done node", got)
	}
	// No doubling: "found 12 packages" appears exactly once (only in the done node),
	// not also as a separate result-preview body.
	if n := strings.Count(got, "found 12 packages"); n != 1 {
		t.Errorf("subagent card = %q, summary appears %d times, want exactly 1 (no doubling)", got, n)
	}
	// Every point on the rail is a circle — no "⎿" children anywhere.
	if strings.Contains(got, "⎿") {
		t.Errorf("subagent card = %q, must NOT contain any ⎿ (every point is a node)", got)
	}
	// Nested == 0 → no nested-steps line.
	if strings.Contains(got, "nested subagent steps") {
		t.Errorf("subagent card = %q, must NOT show the nested line when Nested==0", got)
	}
}

// TestRenderSubagentCardHeaderWraps pins that a long subagent header WRAPS to the viewport width
// instead of overflowing the right edge (the reported bug: `Subagent(operator)  "Perform a
// focused security audit of sandbox confine…"` ran off screen): a narrow width wraps the header
// into more rows than a wide one, the first row carries the "○" header node, and the full task
// text survives (wrapped, not clipped).
func TestRenderSubagentCardHeaderWraps(t *testing.T) {
	t.Parallel()

	task := "Perform a focused security audit of the sandbox confinement and report every escape path"
	c := ToolCallView{ToolName: "Subagent", Agent: "operator", Task: task, Steps: 3, SubStatus: subDone, Result: []string{"ok"}}

	narrow := stripANSI(renderSubagentCard(c, false, 40))
	wide := stripANSI(renderSubagentCard(c, false, 200))

	// It wraps: the narrow header spans more rows than the wide one (which fits on one line).
	if narrowRows, wideRows := strings.Count(narrow, "\n"), strings.Count(wide, "\n"); narrowRows <= wideRows {
		t.Errorf("narrow rows = %d, wide rows = %d; want the narrow header to wrap into more rows\nnarrow:\n%s", narrowRows, wideRows, narrow)
	}
	// The first row carries the "○" header node (the header, not a child/done line).
	if first := strings.SplitN(narrow, "\n", 2)[0]; !strings.HasPrefix(first, "○ ") {
		t.Errorf("first header row = %q, want it to start with the ○ header node", first)
	}
	// Nothing is clipped: every task word survives the wrap.
	flat := strings.Join(strings.Fields(narrow), " ")
	for _, word := range strings.Fields(task) {
		if !strings.Contains(flat, word) {
			t.Errorf("wrapped header dropped %q; got:\n%s", word, narrow)
		}
	}
}

// TestRenderEntrySubagentCardTerminals covers the done-line verb per SubStatus and the
// nested-steps line: failed shows the error text, interrupted omits the summary, and a
// positive Nested adds the "+M nested subagent steps" line.
func TestRenderEntrySubagentCardTerminals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status subStatus
		result []string
		nested int
		want   []string
		absent []string
	}{
		{
			name:   "done shows the summary",
			status: subDone,
			result: []string{"all good"},
			want:   []string{"done", "all good"},
		},
		{
			name:   "failed shows the error text",
			status: subFailed,
			result: []string{"boom: it broke"},
			want:   []string{"failed", "boom: it broke"},
			absent: []string{"done"},
		},
		{
			name:   "interrupted omits the summary",
			status: subInterrupted,
			result: []string{"ignored summary"},
			want:   []string{"interrupted"},
			absent: []string{"ignored summary"},
		},
		{
			name:   "nested counter shows when positive",
			status: subDone,
			result: []string{"ok"},
			nested: 3,
			want:   []string{"+3 nested subagent steps"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := entry{Kind: kindTool, Calls: []ToolCallView{{
				ToolName:  "Subagent",
				Agent:     "explorer",
				Task:      "task",
				Steps:     2,
				SubStatus: tt.status,
				Result:    tt.result,
				Nested:    tt.nested,
			}}}
			got := stripANSI(strings.Join(renderEntry(e, false, 100), "\n"))
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("subagent card (%s) = %q, want %q", tt.name, got, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Errorf("subagent card (%s) = %q, must NOT contain %q", tt.name, got, a)
				}
			}
		})
	}
}

// TestSubagentNodeStatus covers the child-loop terminal status → rail-node tint mapping:
// subDone is the hollow OK node, subFailed and subInterrupted are the failed (red) node,
// and subRunning is the pulsing node. stripANSI cannot distinguish the node colors, so
// this maps the status directly to its styles.NodeStatus.
func TestSubagentNodeStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status subStatus
		want   styles.NodeStatus
	}{
		{name: "done is the hollow node", status: subDone, want: styles.NodeOK},
		{name: "failed is the failed node", status: subFailed, want: styles.NodeFailed},
		{name: "interrupted is the failed node", status: subInterrupted, want: styles.NodeFailed},
		{name: "running is the pulsing node", status: subRunning, want: styles.NodeRunning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := subagentNodeStatus(tt.status); got != tt.want {
				t.Errorf("subagentNodeStatus(%d) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

// TestRenderSubagentCard_NestedRail covers the Task-6 nested-rail subagent render: the
// subagent is an "○" header node at depth 0 opening a NESTED secondary rail whose children
// are depth-1 "│ ○" nodes (with "│ │" detail rows), closed by a "│ ○ verb · N steps —
// \"summary\"" done node at depth 1. Every point on the rail is a circle — no "⎿" children
// and no plain done line. The interrupted variant omits the summary; a positive Nested adds
// a "│ │ +N nested subagent steps" collapsed marker.
func TestRenderSubagentCard_NestedRail(t *testing.T) {
	t.Parallel()

	base := ToolCallView{
		ToolName:  "Subagent",
		Agent:     "explore",
		Task:      "find the retry logic",
		SubStatus: subDone,
		Steps:     2,
		Result:    []string{"found in backoff.go"},
		Children: []ToolCallView{
			{ToolName: "Read", Summary: "backoff.go", Status: ToolOK, Result: []string{"12 lines"}},
			{ToolName: "Grep", Summary: "retry", Status: ToolOK, Result: []string{"3 matches"}},
		},
	}

	t.Run("done nested rail", func(t *testing.T) {
		t.Parallel()
		out := stripANSI(renderSubagentCard(base, true, 80))
		for _, w := range []string{
			"○ Subagent(explore)",
			"find the retry logic",
			"│ ○ Read(backoff.go)",
			"│ │ 12 lines",
			"│ ○ Grep(retry)",
			"│ │ 3 matches",
			`│ ○ done · 2 steps — "found in backoff.go"`,
		} {
			if !strings.Contains(out, w) {
				t.Errorf("nested rail = \n%s\nwant it to contain %q", out, w)
			}
		}
		if strings.Contains(out, "⎿") {
			t.Errorf("nested rail = \n%s\nmust NOT contain any ⎿ (every point is a circle)", out)
		}
		// The closing line begins with a node (its depth-1 spine then "○"), not plain text.
		lines := strings.Split(out, "\n")
		last := lines[len(lines)-1]
		if !strings.HasPrefix(last, "│ ○ done") {
			t.Errorf("closing line = %q, want it to begin with the depth-1 done node", last)
		}
	})

	t.Run("failed closing text", func(t *testing.T) {
		t.Parallel()
		c := base
		c.SubStatus = subFailed
		c.Children = []ToolCallView{{ToolName: "Read", Summary: "backoff.go", Status: ToolError, Result: []string{"no such file"}}}
		out := stripANSI(renderSubagentCard(c, true, 80))
		if !strings.Contains(out, "failed · 2 steps") {
			t.Errorf("failed nested rail = \n%s\nwant the closing 'failed · 2 steps' text", out)
		}
		// The red tint (subFailed → styles.NodeFailed) is asserted by TestSubagentNodeStatus;
		// stripANSI is color-blind so it cannot be seen here.
	})

	t.Run("interrupted omits the summary", func(t *testing.T) {
		t.Parallel()
		c := base
		c.SubStatus = subInterrupted
		out := stripANSI(renderSubagentCard(c, true, 80))
		if !strings.Contains(out, "interrupted · 2 steps") {
			t.Errorf("interrupted nested rail = \n%s\nwant 'interrupted · 2 steps'", out)
		}
		if strings.Contains(out, "found in backoff.go") || strings.Contains(out, " — ") {
			t.Errorf("interrupted nested rail = \n%s\nmust NOT append the summary", out)
		}
	})

	t.Run("nested counter collapsed marker", func(t *testing.T) {
		t.Parallel()
		c := base
		c.Nested = 3
		out := stripANSI(renderSubagentCard(c, true, 80))
		if !strings.Contains(out, "│ │ +3 nested subagent steps") {
			t.Errorf("nested rail = \n%s\nwant the '│ │ +3 nested subagent steps' marker", out)
		}
	})
}

// TestRenderLiveAssistantNoWorkingWord pins the Task 8 removal of the live working-word
// umbrella: a card-only (empty-text) live step now shows its tool nodes DIRECTLY with no
// rotating "Working"/"Whirring" headline, matching the committed node-presence rule (no
// text → no "●" bullet, the running node's pulsing "◍" stands on its own).
func TestRenderLiveAssistantNoWorkingWord(t *testing.T) {
	t.Parallel()

	calls := []ToolCallView{{ToolName: "Bash", Status: ToolRunning}}
	for _, frame := range []uint{0, 1, 5} {
		got := stripANSI(renderLiveAssistant("", "", calls, nil, true, 80, animState{frame: frame}))
		for _, w := range workingWords {
			if strings.Contains(got, w) {
				t.Errorf("frame %d: live step %q must not show a working-word (%q)", frame, got, w)
			}
		}
		if strings.Contains(got, "●") || strings.Contains(got, "◦") {
			t.Errorf("frame %d: live tools-only step %q must not show a ●/◦ bullet umbrella", frame, got)
		}
		if !strings.Contains(got, "◍") {
			t.Errorf("frame %d: live step %q missing the pulsing running node ◍", frame, got)
		}
	}
}
