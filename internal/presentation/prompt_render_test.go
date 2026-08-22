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
	if want == "" {
		t.Fatal("assertBandedRow called with an empty want — that would assert nothing")
	}
	got := bandedRows(t, rendered)
	if len(got) != 1 {
		t.Fatalf("banded rows = %q, want exactly one (%q) in:\n%s", got, want, stripANSI(rendered))
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

// TestChoiceRowFitsWidth pins the two things a choice row owes the card: a SELECTED row
// spans the body width exactly, so the band reaches both edges, and NO row exceeds it, so a
// long choice cannot push the panel rail out of alignment. The double-digit accelerator is
// the case a single fixed prefix width got wrong.
//
// It measures with lipgloss.Width, a DISPLAY-width function, and so must feed labels whose
// display width differs from their rune count — otherwise it only ever asserts against
// ASCII and reads as a guarantee the row arithmetic does not actually make. Choice labels
// are model- and tool-supplied text, so both cases below arrive through normal use: CJK is
// two columns per rune (a rune-counted budget overflowed the card by the difference), and a
// decomposed accent is two runes per column (it under-filled by the same slack).
//
// An UNSELECTED row is asserted as a bound, not an equality, and the difference is real
// rather than a hedge: a 2-cell rune cannot be split across the limit, so a label ending one
// column short of the budget leaves that column unused (" [10] 日日日日日日日日…" is 23 of 24).
// The selected row still measures exactly width because styles.SelectedRow pads it.
func TestChoiceRowFitsWidth(t *testing.T) {
	t.Parallel()

	const width = 24
	labels := map[string]string{
		"ascii":     strings.Repeat("verylongchoice", 4),
		"cjk wide":  strings.Repeat("\u65e5", 40),
		"combining": strings.Repeat("e\u0301", 40),
	}
	for name, long := range labels {
		for _, index := range []int{0, 9, 99} {
			for _, selected := range []bool{false, true} {
				row := stripANSI(choiceRow(index, long, selected, width))
				got := lipgloss.Width(row)
				if got > width {
					t.Errorf("%s index %d selected=%v: row width = %d, want at most %d (%q)", name, index, selected, got, width, row)
				}
				if selected && got != width {
					t.Errorf("%s index %d: selected row width = %d, want exactly %d so the band spans the card (%q)", name, index, got, width, row)
				}
			}
		}
	}
}

// TestKeyRowNeverOverflowsWidth pins the clamp on the shared row shape at the widths where
// the row's own chrome does not fit.
//
// keyRow emits " [k] " unconditionally and used to bound only the LABEL, so a card too
// narrow to hold the accelerator overflowed by the difference no matter how short the label
// was: "[100]" in 6 columns rendered 7 cells. styles.SelectedRow pads but never truncates,
// and neither choiceRow nor permissionActionRows clamped, so that extra cell escaped the
// card and pushed the panel rail out of alignment for every row beneath it.
func TestKeyRowNeverOverflowsWidth(t *testing.T) {
	t.Parallel()

	keys := []string{"[y]", "[1]", "[10]", "[100]"}
	labels := []string{"", "x", "a much longer label than fits", strings.Repeat("日", 20)}

	for _, key := range keys {
		for _, label := range labels {
			for width := 0; width <= 12; width++ {
				row := stripANSI(keyRow(key, label, width))
				if got := lipgloss.Width(row); got > width {
					t.Errorf("keyRow(%q, %q, %d) = %q, width %d — overflows by %d",
						key, label, width, row, got, got-width)
				}
			}
		}
	}
}

// TestFormRowFitsWidth pins that a form field row spans EXACTLY the card body width,
// cursor prefix included, however long the label and the value are. The row carries a
// 2-cell cursor/indent prefix (formCursorWidth) that the label budget must pay for;
// charging the label the full width instead put the row two columns over on every long
// field, focused or not.
//
// A grown field is measured the same way: lipgloss.Width is the WIDEST line, so a row
// that wrapped onto extra rows and let one of them run long fails here too.
func TestFormRowFitsWidth(t *testing.T) {
	t.Parallel()

	labels := map[string]string{
		"overlong ascii": strings.Repeat("longfieldlabel", 6),
		"short ascii":    "Note",
		// A CJK label is the case a rune count gets wrong: the lead-in is 2 cells per
		// rune, so the continuation rows are indented by CELLS or they do not line up
		// under the first row and the field stops spanning the card.
		"cjk": "日本語のラベル",
	}
	for name, label := range labels {
		for _, width := range []int{12, 20, 30, 48} {
			for _, focused := range []bool{false, true} {
				f := formField{Label: label, Kind: gate.FieldText, editor: newFormEditor(strings.Repeat("v", 40))}
				row := stripANSI(formRow(&f, focused, width))
				if got := lipgloss.Width(row); got != width {
					t.Errorf("%s/%d/focused=%v: form row width = %d, want %d (%q)", name, width, focused, got, width, row)
				}
			}
		}
	}
}

// TestFormRowKeepsTheWholeValue pins that a growing text field never LOSES the answer to
// the row's column budget. Whatever the label costs, the value wraps into the columns
// left over and every one of its characters reaches the card.
//
// This is the half of the row contract the width clamp cannot see. A clamped row is
// exactly the card width whether the budget was right or wrong, so a field that wrapped
// at the wrong column — charged for a cursor prefix it does not pay, indented by runes
// instead of cells, handed a lead-in that was never clipped — looks perfect to a width
// check while quietly cutting the tail off every row. A field that grows exists so an
// answer is never clipped; that is the thing to pin.
func TestFormRowKeepsTheWholeValue(t *testing.T) {
	t.Parallel()

	const value = "abcdefghijklmnopqrstuvwxyz0123456789"
	labels := map[string]string{
		"short ascii":    "Note",
		"overlong ascii": strings.Repeat("longfieldlabel", 6),
		"cjk":            "日本語のラベル",
	}
	for name, label := range labels {
		for _, width := range []int{12, 20, 30, 48} {
			f := formField{Label: label, Kind: gate.FieldText, editor: newFormEditor(value)}
			// The value wraps into fixed-width chunks padded with spaces, so dropping the
			// whitespace and the row breaks puts it back together contiguously.
			row := stripANSI(formRow(&f, true, width))
			joined := strings.NewReplacer(" ", "", "\n", "").Replace(row)
			if !strings.Contains(joined, value) {
				t.Errorf("%s/%d: the value did not survive the row: %q", name, width, row)
			}
		}
	}
}

// TestApprovalHintsCoverEveryControl pins approvalHints against gate.ApprovalControls,
// one row per control and in the same order.
//
// approvalHints cannot be DERIVED from the control set, because a control's key is a TUI
// decision the shared set does not carry. This test is what the derivation would have
// bought: a control added, removed or reordered upstream fails here rather than leaving the
// card silently short a row, or pairing an action with another action's key.
func TestApprovalHintsCoverEveryControl(t *testing.T) {
	t.Parallel()

	controls := gate.ApprovalControls()
	if len(controls) != len(approvalHints) {
		t.Fatalf("approvalHints has %d rows, want one per gate.ApprovalControls control (%d)", len(approvalHints), len(controls))
	}
	for i, c := range controls {
		if string(approvalHints[i].action) != c.Action {
			t.Errorf("approvalHints[%d].action = %q, want control %d's action %q", i, approvalHints[i].action, i, c.Action)
		}
	}
}

// TestDenyHintIndexWithoutADenyRow pins the answer denyHintIndex must never give.
//
// The cursor a fresh permission prompt starts on is whatever this returns. A hint list with
// no gate.ApprovalDeny row has no correct index, and -1 is how that is said: it names no
// row, nothing is banded, and approvalAt(-1) denies. Returning 0 on a miss would instead
// park the cursor on the FIRST row and a blind enter would approve — the fail-secure
// default silently inverted.
func TestDenyHintIndexWithoutADenyRow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		hints []approvalHint
		want  int
	}{
		{name: "no deny row", hints: []approvalHint{{"y", gate.ApprovalApprove}, {"a", gate.ApprovalApproveAlwaysWorkspace}}, want: -1},
		{name: "empty list", hints: nil, want: -1},
		{name: "deny is not first", hints: []approvalHint{{"y", gate.ApprovalApprove}, {"n", gate.ApprovalDeny}}, want: 1},
		{name: "the shipped list", hints: approvalHints, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := denyHintIndex(tt.hints); got != tt.want {
				t.Errorf("denyHintIndex = %d, want %d", got, tt.want)
			}
			if got := denyHintIndex(tt.hints); got == 0 && tt.want != 0 {
				t.Error("denyHintIndex returned 0 on a miss — a blind enter would approve")
			}
		})
	}
}

// TestPermissionRowsAreSelectable pins the permission card's new shape: one row per
// approvalHints entry, each with a bracketed accelerator, the row at p.approval BANDED,
// and no cursor glyph anywhere — the same treatment the AskUser choice card uses, so the
// two cards read alike. The labels are asserted against the exact gate.ApprovalAction
// values so the control set still cannot drift.
func TestPermissionRowsAreSelectable(t *testing.T) {
	t.Parallel()

	p := prompt{ToolName: "Bash", approval: 1}
	rendered := renderPermissionBox(p, 60, 1)
	got := stripANSI(rendered)

	for _, want := range []string{
		"[y] " + string(gate.ApprovalApprove),
		"[a] " + string(gate.ApprovalApproveAlwaysWorkspace),
		"[n] " + string(gate.ApprovalDeny),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("permission card missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "▸") {
		t.Errorf("permission card uses a cursor glyph:\n%s", got)
	}
	assertBandedRow(t, rendered, "[a] "+string(gate.ApprovalApproveAlwaysWorkspace))
}

// TestPermissionBandFollowsTheCursor pins that the band tracks p.approval — exactly one
// row banded, and it is the one the cursor names. assertBandedRow's "exactly one" half is
// the load-bearing part: two banded rows would mean two cursors.
func TestPermissionBandFollowsTheCursor(t *testing.T) {
	t.Parallel()

	for i, h := range approvalHints {
		p := prompt{ToolName: "Bash", approval: i}
		assertBandedRow(t, renderPermissionBox(p, 60, 1), "["+h.key+"] "+string(h.action))
	}
}

// TestPermissionCursorFailsSecure covers the two fail-secure properties of the action
// cursor. A freshly built permission prompt starts on Deny, so the action a stray enter
// lands on blocks the tool call rather than running it. And a cursor that names no row at
// all resolves to Deny rather than to whatever sits at index 0.
func TestPermissionCursorFailsSecure(t *testing.T) {
	t.Parallel()

	p := promptFromPermission(callID(1), bashPermission("rm -rf /"))
	if got := approvalAt(p.approval); got != gate.ApprovalDeny {
		t.Errorf("fresh permission prompt selects %q, want %q", got, gate.ApprovalDeny)
	}
	assertBandedRow(t, renderPermissionBox(p, 60, 1), "[n] "+string(gate.ApprovalDeny))

	for _, i := range []int{-1, len(approvalHints), 1 << 20} {
		if got := approvalAt(i); got != gate.ApprovalDeny {
			t.Errorf("approvalAt(%d) = %q, want %q", i, got, gate.ApprovalDeny)
		}
	}
}
