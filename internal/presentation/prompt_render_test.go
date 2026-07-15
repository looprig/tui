package presentation

import (
	"regexp"
	"strings"
	"testing"

	"github.com/looprig/harness/pkg/tool"
)

// styledCursor matches the ▸ selection cursor immediately preceded by an SGR escape —
// the evidence that the selected choice row is HIGHLIGHTED (CardSelectedStyle wraps the
// row, which begins with "▸ "), not rendered plain like the unselected rows. Bold alone
// emits an SGR even with color off, so this holds across color profiles.
var styledCursor = regexp.MustCompile("\x1b\\[[0-9;]*m▸")

// panelRail is the blue ▌ edge every gate row carries in the panel-styled card. It
// survives stripANSI (only SGR color codes are stripped), so it is the color-agnostic
// evidence the control is the blue panel rather than the prior rounded-border card.
const panelRail = "▌"

// assertPanelFramed fails t unless got is the blue-panel gate: it draws NO rounded border,
// spans at least three rows, opens with a rail-only pad row, and every row carries the ▌ rail
// down the left edge. The trailing "(+N more pending)" note sits OUTSIDE the panel, so it is
// exempt from the rail check.
func assertPanelFramed(t *testing.T, got string) {
	t.Helper()
	for _, g := range []string{"╭", "╮", "╰", "╯"} {
		if strings.Contains(got, g) {
			t.Errorf("panel gate unexpectedly draws rounded-border glyph %q in:\n%s", g, got)
		}
	}
	lines := strings.Split(stripANSI(got), "\n")
	if len(lines) < 3 {
		t.Errorf("panel gate too short to be framed (%d lines):\n%s", len(lines), got)
		return
	}
	if got := strings.TrimSpace(lines[0]); got != panelRail {
		t.Errorf("panel gate first row = %q, want a %q-only pad row", got, panelRail)
	}
	for i, ln := range lines {
		if ln == "" || strings.HasPrefix(ln, "(+") {
			continue // a blank tail line or the pending note, which sits outside the panel
		}
		if !strings.HasPrefix(ln, panelRail) {
			t.Errorf("panel gate row %d does not start with the %q rail: %q", i, panelRail, ln)
		}
	}
}

// TestRenderPermissionBox covers the permission card: a blue-panel box with a
// bold "Approve <ToolName>?" title, the request description as its body, and a footer
// row of key hints that shows ONLY the scope keys the request offers — [y] once iff
// ScopeOnce, [s] session iff ScopeSession, [w] workspace iff ScopeWorkspace — plus
// [n] deny ALWAYS. (+N more pending) appears when the queue is deeper than one. The
// scope-hint plain text is preserved so key-routing tests that read the control still
// match — only the framing changed.
func TestRenderPermissionBox(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		req         tool.PermissionRequest
		pending     int
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "bash offers all scopes plus deny",
			req:         tool.BashRequest{Command: "go build"},
			pending:     1,
			wantContain: []string{"Approve Bash?", "go build", "[y] once", "[s] session", "[w] workspace", "[n] deny"},
			wantAbsent:  []string{"more pending"},
		},
		{
			name:        "fetch shows method and url",
			req:         tool.FetchRequest{Method: "GET", URL: "https://google.com"},
			pending:     1,
			wantContain: []string{"Approve Fetch?", "GET https://google.com", "[y] once", "[s] session", "[w] workspace", "[n] deny"},
			wantAbsent:  []string{"more pending"},
		},
		{
			name:        "unknown offers only once and deny",
			req:         tool.UnknownRequest{Tool: "Mystery", Summary: "does a thing"},
			pending:     1,
			wantContain: []string{"Approve Mystery?", "does a thing", "[y] once", "[n] deny"},
			wantAbsent:  []string{"[s] session", "[w] workspace"},
		},
		{
			name:        "more pending hint when queue deeper than one",
			req:         tool.BashRequest{Command: "rm -rf /"},
			pending:     3,
			wantContain: []string{"Approve Bash?", "(+2 more pending)"},
			wantAbsent:  []string{},
		},
		{
			name:        "single pending shows no hint",
			req:         tool.FileWriteRequest{Path: "/tmp/x"},
			pending:     1,
			wantContain: []string{"Approve WriteFile?", "[y] once"},
			wantAbsent:  []string{"more pending"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := promptFromPermission(callID(1), tt.req)
			rendered := renderPermissionBox(p, 80, tt.pending)
			got := stripANSI(rendered)

			assertPanelFramed(t, rendered)
			for _, sub := range tt.wantContain {
				if !strings.Contains(got, sub) {
					t.Errorf("renderPermissionBox missing %q in:\n%s", sub, got)
				}
			}
			for _, sub := range tt.wantAbsent {
				if sub != "" && strings.Contains(got, sub) {
					t.Errorf("renderPermissionBox unexpectedly contains %q in:\n%s", sub, got)
				}
			}
		})
	}
}

// TestRenderAskUserBoxChoices covers the choice-list AskUser card: a blue-panel box with
// numbered choices [1].., the ▸ cursor on prompt.selected with that row HIGHLIGHTED, an
// [o] other escape hatch, the key legend, and a window that scrolls with selected so a
// high row (including double-digit choices past the 1–9 accelerators) stays visible.
func TestRenderAskUserBoxChoices(t *testing.T) {
	t.Parallel()

	twelve := []string{
		"internal/version.Version()", "git describe", "VERSION file",
		"build-time ldflags", "hardcoded constant", "env var", "config key",
		"latest git tag", "CHANGELOG top", "date-based", "manual prompt", "ask each build",
	}

	tests := []struct {
		name        string
		question    string
		choices     []string
		selected    int
		height      int
		pending     int
		wantContain []string
		wantAbsent  []string
	}{
		{
			name:        "short list shows all numbered with other and cursor",
			question:    "pick one",
			choices:     []string{"alpha", "beta", "gamma"},
			selected:    0,
			height:      10,
			pending:     1,
			wantContain: []string{"1.", "alpha", "2.", "beta", "3.", "gamma", "[o] other", "▸", "↑/↓ select"},
		},
		{
			name:        "cursor marks the selected row",
			question:    "pick one",
			choices:     []string{"alpha", "beta", "gamma"},
			selected:    2,
			height:      10,
			pending:     1,
			wantContain: []string{"▸ 3. gamma"},
			wantAbsent:  []string{"▸ 1.", "▸ 2."},
		},
		{
			name:        "window scrolls so high selection stays visible",
			question:    "Which source?",
			choices:     twelve,
			selected:    9, // the 10th choice, well past a small window
			height:      5,
			pending:     1,
			wantContain: []string{"▸ 10. date-based"},
			wantAbsent:  []string{"1. internal/version.Version()"}, // scrolled out of the window
		},
		{
			name:        "double-digit choice past the 1-9 keys renders and is reachable",
			question:    "Which source?",
			choices:     twelve,
			selected:    11, // the 12th choice — beyond the 1–9 accelerators
			height:      5,
			pending:     1,
			wantContain: []string{"▸ 12. ask each build"},
		},
		{
			name:        "more pending hint with many choices",
			question:    "Which source?",
			choices:     twelve,
			selected:    0,
			height:      8,
			pending:     4,
			wantContain: []string{"(+3 more pending)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := promptFromUserInput(callID(2), tt.question, tt.choices)
			p.selected = tt.selected
			rendered := renderAskUserBox(p, 80, tt.height, tt.pending)
			got := stripANSI(rendered)

			assertPanelFramed(t, rendered)
			if !styledCursor.MatchString(rendered) {
				t.Errorf("choice card selected row not highlighted (no styled ▸) in:\n%s", rendered)
			}
			for _, sub := range tt.wantContain {
				if !strings.Contains(got, sub) {
					t.Errorf("renderAskUserBox missing %q in:\n%s", sub, got)
				}
			}
			for _, sub := range tt.wantAbsent {
				if sub != "" && strings.Contains(got, sub) {
					t.Errorf("renderAskUserBox unexpectedly contains %q in:\n%s", sub, got)
				}
			}
		})
	}
}

// TestRenderAskUserBoxFreeText covers the free-text card: a blue-panel box with the
// "answer" title and the question as its body, NO choice list and NO [o] other hint.
// The actual answer field is the reused composer the surface stacks below this card.
func TestRenderAskUserBoxFreeText(t *testing.T) {
	t.Parallel()

	p := promptFromUserInput(callID(3), "What should the version look like?", nil)
	rendered := renderAskUserBox(p, 80, 6, 1)
	got := stripANSI(rendered)

	assertPanelFramed(t, rendered)
	for _, sub := range []string{"answer", "What should the version look like?"} {
		if !strings.Contains(got, sub) {
			t.Errorf("free-text box missing %q in:\n%s", sub, got)
		}
	}
	for _, sub := range []string{"[o] other", "▸", "1."} {
		if strings.Contains(got, sub) {
			t.Errorf("free-text box unexpectedly contains choice affordance %q in:\n%s", sub, got)
		}
	}
}

// TestChoiceWindow covers the pure window calculator that selects the visible slice
// of choices and the cursor offset within it, scrolling so selected stays inside.
func TestChoiceWindow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		total         int
		selected      int
		cap           int
		wantStart     int
		wantEnd       int
		wantHasCursor bool
	}{
		{name: "all fit", total: 3, selected: 1, cap: 5, wantStart: 0, wantEnd: 3, wantHasCursor: true},
		{name: "cap zero shows none", total: 5, selected: 2, cap: 0, wantStart: 0, wantEnd: 0, wantHasCursor: false},
		{name: "selection at start", total: 12, selected: 0, cap: 3, wantStart: 0, wantEnd: 3, wantHasCursor: true},
		{name: "selection scrolls window", total: 12, selected: 9, cap: 3, wantStart: 8, wantEnd: 11, wantHasCursor: true},
		{name: "selection at end clamps", total: 12, selected: 11, cap: 3, wantStart: 9, wantEnd: 12, wantHasCursor: true},
		{name: "empty list", total: 0, selected: 0, cap: 5, wantStart: 0, wantEnd: 0, wantHasCursor: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start, end := choiceWindow(tt.total, tt.selected, tt.cap)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("choiceWindow(%d,%d,%d) = (%d,%d), want (%d,%d)",
					tt.total, tt.selected, tt.cap, start, end, tt.wantStart, tt.wantEnd)
			}
			hasCursor := tt.selected >= start && tt.selected < end
			if hasCursor != tt.wantHasCursor {
				t.Errorf("cursor visible = %v, want %v (start=%d end=%d sel=%d)",
					hasCursor, tt.wantHasCursor, start, end, tt.selected)
			}
		})
	}
}
