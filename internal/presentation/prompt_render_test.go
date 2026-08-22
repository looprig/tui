package presentation

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/looprig/harness/pkg/gate"
	"github.com/looprig/tui/styles"
)

// bandedRows returns the ANSI-stripped, space-trimmed text of every row of a rendered card
// that carries THE selection fill — the evidence of which row is selected now that the band
// replaces the ▸ cursor glyph.
//
// It looks for the fill derived from styles.CardBorderColor, the single blue token
// styles.SelectedRow bands with. Asserting against that token rather than a literal escape
// is what makes these tests catch a card that invents its own highlight: a private shade
// would simply not appear here.
func bandedRows(t *testing.T, rendered string) []string {
	t.Helper()
	open, _ := styles.DeriveBackgroundSGR(styles.CardBorderColor)
	if open == "" {
		t.Fatal("selection fill derives no SGR — bandedRows cannot tell selected rows apart")
	}
	var out []string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, open) {
			out = append(out, strings.TrimSpace(stripANSI(line)))
		}
	}
	return out
}

// assertBandedRow fails t unless exactly one row of rendered is banded and it reads want.
// "Exactly one" is half the assertion: two banded rows would mean two cursors.
func assertBandedRow(t *testing.T, rendered, want string) {
	t.Helper()
	got := bandedRows(t, rendered)
	if len(got) != 1 {
		t.Fatalf("banded rows = %q, want exactly one (%q) in:\n%s", got, want, stripANSI(rendered))
	}
	if want == "" {
		t.Fatal("assertBandedRow called with an empty want — that would assert nothing")
	}
	if !strings.Contains(got[0], want) {
		t.Errorf("banded row = %q, want it to contain %q", got[0], want)
	}
}

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

// TestRenderPermissionBoxCombinedMultiCapability is the headline render test: ONE
// blue-panel prompt shows EVERY unmet requirement and EVERY exact persisted candidate,
// and offers EXACTLY the three gate.ApprovalControls actions (Approve / Approve always
// for this workspace / Deny) — no session scope, no user-global scope, no per-capability
// sub-prompt. The three footer labels are asserted against the gate.ApprovalAction
// values so the control set can never drift.
func TestRenderPermissionBoxCombinedMultiCapability(t *testing.T) {
	t.Parallel()

	// Three unmet requirements, each with its exact persisted rule candidates — all in
	// ONE prompt.
	req := toolRequest("Bash", "run the release pipeline",
		requirement("execute /bin/release", "always allow /bin/release"),
		requirement("write /etc/hosts", "always allow writes under /etc"),
		requirement("connect api.example.com:443", "always allow api.example.com:443"),
	)
	p := promptFromPermission(callID(1), req)
	rendered := renderPermissionBox(p, 100, 1)
	got := stripANSI(rendered)

	assertPanelFramed(t, rendered)

	// Header + summary + every requirement + every candidate, all in the single prompt.
	wantContain := []string{
		"Approve Bash?", "run the release pipeline",
		"execute /bin/release", "always allow /bin/release",
		"write /etc/hosts", "always allow writes under /etc",
		"connect api.example.com:443", "always allow api.example.com:443",
	}
	for _, sub := range wantContain {
		if !strings.Contains(got, sub) {
			t.Errorf("combined prompt missing %q in:\n%s", sub, got)
		}
	}

	// EXACTLY the three approval actions, by their exact gate.ApprovalAction strings.
	for _, action := range []gate.ApprovalAction{gate.ApprovalApprove, gate.ApprovalApproveAlwaysWorkspace, gate.ApprovalDeny} {
		if !strings.Contains(got, string(action)) {
			t.Errorf("footer missing exact approval action %q in:\n%s", action, got)
		}
	}
	// No legacy scope affordances survive.
	for _, gone := range []string{"once", "session", "workspace level", "[s] ", "[w] "} {
		if strings.Contains(got, gone) {
			t.Errorf("combined prompt still shows removed scope affordance %q in:\n%s", gone, got)
		}
	}
	// The keys the router binds.
	for _, key := range []string{"[y]", "[a]", "[n]"} {
		if !strings.Contains(got, key) {
			t.Errorf("footer missing key %q in:\n%s", key, got)
		}
	}
}

// TestRenderPermissionBoxPendingAndPure covers the (+N more pending) note and a pure
// tool with no requirements (header + three actions only).
func TestRenderPermissionBoxPendingAndPure(t *testing.T) {
	t.Parallel()

	deep := renderPermissionBox(promptFromPermission(callID(1), bashPermission("rm -rf /")), 80, 3)
	if got := stripANSI(deep); !strings.Contains(got, "Approve Bash?") || !strings.Contains(got, "(+2 more pending)") {
		t.Errorf("deep-queue prompt = %q, want header + (+2 more pending)", got)
	}

	pure := renderPermissionBox(promptFromPermission(callID(2), toolRequest("Mystery", "does a thing")), 80, 1)
	got := stripANSI(pure)
	assertPanelFramed(t, pure)
	if !strings.Contains(got, "Approve Mystery?") || !strings.Contains(got, "does a thing") {
		t.Errorf("pure-tool prompt = %q, want header + summary", got)
	}
	if strings.Contains(got, "more pending") {
		t.Errorf("single-pending prompt unexpectedly shows a pending note in:\n%s", got)
	}
	if !strings.Contains(got, string(gate.ApprovalApprove)) || !strings.Contains(got, string(gate.ApprovalDeny)) {
		t.Errorf("pure-tool prompt missing the approval actions in:\n%s", got)
	}
}

// TestRenderAskUserBoxChoices covers the choice-list AskUser card: a blue-panel box with
// bracketed accelerators [1].., prompt.selected's row BANDED (no cursor glyph — the band is
// the cursor), an [o] other escape hatch, the key legend, and a window that scrolls with
// selected so a high row (including double-digit choices past the 1–9 accelerators) stays
// visible.
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
		wantBanded  string // the text expected on the one banded (selected) row
	}{
		{
			name:        "short list shows all with bracketed keys, other and a band",
			question:    "pick one",
			choices:     []string{"alpha", "beta", "gamma"},
			selected:    0,
			height:      10,
			pending:     1,
			wantContain: []string{"[1] alpha", "[2] beta", "[3] gamma", "[o] other", "↑/↓ select"},
			wantAbsent:  []string{"▸"},
			wantBanded:  "[1] alpha",
		},
		{
			name:        "the band marks the selected row",
			question:    "pick one",
			choices:     []string{"alpha", "beta", "gamma"},
			selected:    2,
			height:      10,
			pending:     1,
			wantContain: []string{"[3] gamma"},
			wantAbsent:  []string{"▸"},
			wantBanded:  "[3] gamma",
		},
		{
			name:        "window scrolls so high selection stays visible",
			question:    "Which source?",
			choices:     twelve,
			selected:    9, // the 10th choice, well past a small window
			height:      5,
			pending:     1,
			wantContain: []string{"[10] date-based"},
			wantAbsent:  []string{"internal/version.Version()"}, // scrolled out of the window
			wantBanded:  "[10] date-based",
		},
		{
			name:        "double-digit choice past the 1-9 keys renders and is reachable",
			question:    "Which source?",
			choices:     twelve,
			selected:    11, // the 12th choice — beyond the 1–9 accelerators
			height:      5,
			pending:     1,
			wantContain: []string{"[12] ask each build"},
			wantBanded:  "[12] ask each build",
		},
		{
			name:        "more pending hint with many choices",
			question:    "Which source?",
			choices:     twelve,
			selected:    0,
			height:      8,
			pending:     4,
			wantContain: []string{"(+3 more pending)"},
			wantBanded:  "[1] internal/version.Version()",
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
			assertBandedRow(t, rendered, tt.wantBanded)
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
	for _, sub := range []string{"[o] other", "▸", "[1]"} {
		if strings.Contains(got, sub) {
			t.Errorf("free-text box unexpectedly contains choice affordance %q in:\n%s", sub, got)
		}
	}
	if got := bandedRows(t, rendered); len(got) != 0 {
		t.Errorf("free-text box unexpectedly bands a row: %q", got)
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

// styledAccelerator matches a bracketed accelerator immediately preceded by an SGR
// escape — the evidence CardKeyStyle actually wrapped it. CardKeyStyle is bold as well as
// blue, and bold emits an SGR even with color off, so this holds across color profiles.
var styledAccelerator = regexp.MustCompile(`\x1b\[[0-9;]*m\[\d+\]`)

// TestChoiceRowHasNoCursorGlyph pins the two halves of the AskUser row's new shape: the
// accelerator is BRACKETED, and there is no ▸ cursor glyph on the selected row — the
// selection band is the cursor.
func TestChoiceRowHasNoCursorGlyph(t *testing.T) {
	t.Parallel()

	got := choiceRow(1, "develop", true, 40)
	if strings.Contains(stripANSI(got), "▸") {
		t.Errorf("selected choice still uses a cursor glyph: %q", stripANSI(got))
	}
	if !strings.Contains(stripANSI(got), "[2]") {
		t.Errorf("accelerator is not bracketed: %q", stripANSI(got))
	}
}

// TestChoiceRowAcceleratorStyling pins WHERE the bold-blue accelerator survives. An
// unselected row keeps it. The selected row deliberately loses it: styles.SelectedRow's
// fill is light, so it strips the row and re-renders it near-black — a bold-blue key on an
// identical blue fill would be invisible. Only the selected row pays that price.
func TestChoiceRowAcceleratorStyling(t *testing.T) {
	t.Parallel()

	if got := choiceRow(1, "develop", false, 40); !styledAccelerator.MatchString(got) {
		t.Errorf("unselected choice lost its bold-blue accelerator: %q", got)
	}
	if got := choiceRow(1, "develop", true, 40); styledAccelerator.MatchString(got) {
		t.Errorf("selected choice kept inner accelerator styling against the band: %q", got)
	}
}

// TestChoiceRowSelectionDoesNotShiftText pins that moving the selection cannot move a row
// sideways: selected and unselected rows carry the identical " [N] " prefix, so the text
// begins in the same column either way and the list does not jitter under ↑/↓.
func TestChoiceRowSelectionDoesNotShiftText(t *testing.T) {
	t.Parallel()

	for _, index := range []int{0, 9} { // single- and double-digit accelerators
		selected := stripANSI(choiceRow(index, "develop", true, 40))
		plain := stripANSI(choiceRow(index, "develop", false, 40))
		if got, want := strings.Index(selected, "develop"), strings.Index(plain, "develop"); got != want {
			t.Errorf("index %d: selected text starts at column %d, unselected at %d\nselected=%q\nplain   =%q",
				index, got, want, selected, plain)
		}
		if !strings.HasPrefix(selected, strings.TrimRight(plain, " ")) {
			t.Errorf("index %d: selected row %q does not open with the unselected row %q", index, selected, plain)
		}
	}
}

// TestChoiceRowFitsWidth pins that a row is padded/clipped to exactly the card body width
// whether or not it is selected, so the band spans the card and a long choice cannot push
// the panel rail out of alignment. The double-digit accelerator is the case a single fixed
// prefix width got wrong.
func TestChoiceRowFitsWidth(t *testing.T) {
	t.Parallel()

	const width = 24
	long := strings.Repeat("verylongchoice", 4)
	for _, index := range []int{0, 9, 99} {
		for _, selected := range []bool{false, true} {
			row := stripANSI(choiceRow(index, long, selected, width))
			if got := lipgloss.Width(row); got != width {
				t.Errorf("index %d selected=%v: row width = %d, want %d (%q)", index, selected, got, width, row)
			}
		}
	}
}
