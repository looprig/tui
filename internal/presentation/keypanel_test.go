package presentation

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/tui/components"
	"github.com/looprig/tui/styles"
)

// shiftSlashKey is the `?` a US ANSI keyboard actually delivers: the UNSHIFTED key is '/',
// the shift modifier is set, and only Text/String() render the question mark. Every test
// that opens the panel uses this form rather than the convenient {Code: '?'} because it is
// the one that distinguishes a String() match from a Code match — a panel matching on Code
// would see '/' and open on the first character of every slash command.
func shiftSlashKey() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: '/', ShiftedCode: '?', Mod: tea.ModShift, Text: "?"}
}

// spaceKey is the space bar. It carries Text " " yet stringifies to "space", so it is the
// press that catches any attempt to recognize printable input by a one-character String().
func spaceKey() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }

// TestKeyPanelTogglesOnlyOnAnEmptyComposer pins the collision that defines the feature: `?`
// is an ordinary printable character, so it may open the legend ONLY where it cannot be
// something the user is typing. Every negative case additionally asserts the `?` still
// reached its real destination — refusing to toggle must never mean swallowing the key.
func TestKeyPanelTogglesOnlyOnAnEmptyComposer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setup leaves the screen in the state the press lands on.
		setup    func(t *testing.T, m Screen) Screen
		key      tea.KeyPressMsg
		wantOpen bool
		// wantValue is the composer text after the press.
		wantValue string
	}{
		{
			name:      "shift+slash on an empty composer opens the panel",
			key:       shiftSlashKey(),
			wantOpen:  true,
			wantValue: "",
		},
		{
			name:      "an unshifted question mark opens it too",
			key:       runeKey('?'),
			wantOpen:  true,
			wantValue: "",
		},
		{
			name: "a question mark typed into a draft stays literal",
			setup: func(_ *testing.T, m Screen) Screen {
				m.interaction.input.SetValue("why")
				return m
			},
			key:       shiftSlashKey(),
			wantOpen:  false,
			wantValue: "why?",
		},
		{
			name: "a lone space is still something the user typed",
			setup: func(_ *testing.T, m Screen) Screen {
				m.interaction.input.SetValue(" ")
				return m
			},
			key:       shiftSlashKey(),
			wantOpen:  false,
			wantValue: " ?",
		},
		{
			name:      "a plain slash never opens it",
			key:       runeKey('/'),
			wantOpen:  false,
			wantValue: "/",
		},
		{
			name: "a runtime tray owns its own keys",
			setup: func(_ *testing.T, m Screen) Screen {
				m.runtimeTray = components.NewValueComplete([]components.ValueItem{{ID: "a", Label: "a"}}, "")
				return m
			},
			key:       shiftSlashKey(),
			wantOpen:  false,
			wantValue: "",
		},
		{
			name: "a pending gate owns its own keys",
			setup: func(t *testing.T, m Screen) Screen {
				t.Helper()
				return feed(t, m, event.PermissionRequested{
					Header:          hdr(callID(1)),
					ToolExecutionID: callID(7),
					Request:         bashPermission("ls"),
				})
			},
			key:       shiftSlashKey(),
			wantOpen:  false,
			wantValue: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
			if tt.setup != nil {
				m = tt.setup(t, m)
			}
			m, _ = updateScreen(t, m, tt.key)
			if m.keyPanelOpen != tt.wantOpen {
				t.Errorf("keyPanelOpen = %v, want %v", m.keyPanelOpen, tt.wantOpen)
			}
			if got := m.interaction.input.Value(); got != tt.wantValue {
				t.Errorf("composer = %q, want %q", got, tt.wantValue)
			}
			if got := m.layout().panelH > 0; got != tt.wantOpen {
				t.Errorf("panelH > 0 = %v, want %v", got, tt.wantOpen)
			}
		})
	}
}

// TestKeyPanelAutoDismissesOnTheNextKeyAndStillActs pins the peek contract: the panel is not
// a mode. The next press of ANY kind closes it and then does its ordinary job, so no other
// handler ever has to know the panel exists. Each case asserts BOTH halves — closed, and the
// key's own effect still happened — because a dismiss that swallowed the key would satisfy
// the first half alone.
func TestKeyPanelAutoDismissesOnTheNextKeyAndStillActs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  tea.KeyPressMsg
		// acted reports whether the dismissing key ALSO performed its normal job.
		acted func(before, after Screen) bool
	}{
		{
			name:  "a printable character reaches the composer",
			key:   runeKey('a'),
			acted: func(_, after Screen) bool { return after.interaction.input.Value() == "a" },
		},
		{
			name:  "the space bar reaches the composer",
			key:   spaceKey(),
			acted: func(_, after Screen) bool { return after.interaction.input.Value() == " " },
		},
		{
			name: "ctrl+t still flips the global fold",
			key:  ctrlKey('t'),
			acted: func(before, after Screen) bool {
				return after.collapse.GloballyCollapsed() != before.collapse.GloballyCollapsed()
			},
		},
		{
			name: "slash still opens the completion tray",
			key:  runeKey('/'),
			acted: func(_, after Screen) bool {
				return after.interaction.slash != nil && after.interaction.input.Value() == "/"
			},
		},
		{
			name:  "another question mark toggles it shut without typing one",
			key:   shiftSlashKey(),
			acted: func(_, after Screen) bool { return after.interaction.input.Value() == "" },
		},
		{
			name:  "esc closes it and leaves the draft alone",
			key:   tea.KeyPressMsg{Code: tea.KeyEsc},
			acted: func(_, after Screen) bool { return after.interaction.input.Value() == "" },
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
			m, _ = updateScreen(t, m, shiftSlashKey())
			if !m.keyPanelOpen {
				t.Fatal("panel did not open; the dismiss cases below would be vacuous")
			}
			before := m
			m, _ = updateScreen(t, m, tt.key)
			if m.keyPanelOpen {
				t.Errorf("keyPanelOpen = true after %q, want the panel auto-dismissed", tt.key.String())
			}
			if !tt.acted(before, m) {
				t.Errorf("%q was swallowed by the dismiss; it must still do its normal job", tt.key.String())
			}
		})
	}
}

// TestKeyPanelDismissResizesTheViewport pins the half of the dismiss that is invisible in the
// model flags: the panel's rows go back to the transcript, so the viewport must be re-sized
// even on a key (a global chord) whose own handler returns without resizing.
func TestKeyPanelDismissResizesTheViewport(t *testing.T) {
	t.Parallel()

	m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
	closedH := m.viewport.height

	m, _ = updateScreen(t, m, shiftSlashKey())
	openH := m.viewport.height
	if openH >= closedH {
		t.Fatalf("viewport height with the panel open = %d, want fewer than the %d rows it had closed", openH, closedH)
	}

	m, _ = updateScreen(t, m, ctrlKey('n')) // a global chord: focus only, no resize of its own
	if got := m.viewport.height; got != closedH {
		t.Errorf("viewport height after dismiss = %d, want the closed-panel %d", got, closedH)
	}
}

// TestKeyPanelLayoutTakesRowsFromTheTranscript pins the layout arithmetic, which is where a
// panel below the composer goes wrong: it must sit directly under the box, push the gap and
// bar down by exactly its height, and pay for those rows out of contentH — not out of the
// frame, which would run the loop bar off the bottom of the terminal.
func TestKeyPanelLayoutTakesRowsFromTheTranscript(t *testing.T) {
	t.Parallel()

	m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
	closed := m.layout()
	if closed.panelH != 0 {
		t.Fatalf("closed panelH = %d, want 0", closed.panelH)
	}
	if closed.panelTop != closed.gapBotY {
		t.Errorf("closed panelTop = %d, want the gap row %d (a zero-height panel occupies nothing)", closed.panelTop, closed.gapBotY)
	}

	m, _ = updateScreen(t, m, shiftSlashKey())
	lay := m.layout()

	panel := m.keyPanelView(m.height)
	if panel == "" {
		t.Fatal("keyPanelView() = empty with the panel open")
	}
	if got, want := lay.panelH, lipgloss.Height(panel); got != want {
		t.Errorf("panelH = %d, want rendered height %d", got, want)
	}
	if got, want := lay.contentH, closed.contentH-lay.panelH; got != want {
		t.Errorf("contentH = %d, want closed contentH %d - panelH %d = %d", lay.contentH, closed.contentH, lay.panelH, want)
	}
	if lay.panelTop != lay.boxTop+lay.boxH {
		t.Errorf("panelTop = %d, want directly below the box at %d", lay.panelTop, lay.boxTop+lay.boxH)
	}
	if lay.gapBotY != lay.panelTop+lay.panelH {
		t.Errorf("gapBotY = %d, want the panel's bottom %d", lay.gapBotY, lay.panelTop+lay.panelH)
	}
	if lay.barY != m.height-1 {
		t.Errorf("barY = %d, want the last row %d", lay.barY, m.height-1)
	}

	// The panel is a read-only legend. Its rows must be inert, and above all must NOT read as
	// content: a click there would otherwise toggle a fold in the transcript behind it.
	for y := lay.panelTop; y < lay.panelTop+lay.panelH; y++ {
		if got := m.regionAt(y); got != regionGap {
			t.Errorf("regionAt(panel row %d) = %d, want inert regionGap %d", y, got, regionGap)
		}
	}
}

// TestKeyPanelDrawsAtTheRowsLayoutReserved pins layout() and View() against each other. Both
// render the panel independently — layout() to measure it, composeBody to draw it — and every
// row below it lands wrong the moment those two disagree, which is exactly the bug shape this
// file's arithmetic has to avoid. Asserting the DRAWN frame (not just the numbers) is what
// makes a measure/draw divergence fail.
func TestKeyPanelDrawsAtTheRowsLayoutReserved(t *testing.T) {
	t.Parallel()

	m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
	m, _ = updateScreen(t, m, shiftSlashKey())

	lay := m.layout()
	rows := strings.Split(m.composeBody(lay), "\n")
	if got := len(rows); got != m.height {
		t.Fatalf("frame height = %d, want the terminal's %d", got, m.height)
	}
	if lay.panelH == 0 {
		t.Fatal("panelH = 0 with the panel open")
	}
	drawn := ansi.Strip(strings.Join(rows[lay.panelTop:lay.panelTop+lay.panelH], "\n"))
	for _, want := range []string{"ctrl+c", "quit", "esc", "interrupt", "shift+enter", "newline"} {
		if !strings.Contains(drawn, want) {
			t.Errorf("reserved panel rows = %q, missing %q", drawn, want)
		}
	}
	// The row immediately below the reserved block is the inert gap, then the bar. If the
	// panel drew taller than it measured, one of the legend's own rows would be sitting here.
	if got := strings.TrimSpace(ansi.Strip(rows[lay.gapBotY])); got != "" {
		t.Errorf("row below the panel = %q, want the inert blank gap", got)
	}
	for i, row := range rows {
		if got := lipgloss.Width(row); got > m.width {
			t.Errorf("frame row %d width = %d, want <= %d", i, got, m.width)
		}
	}
}

// TestKeyPanelHeightIsAFixedPointAtItsOwnHeight pins the property that keeps layout() and
// View() agreeing: the legend depends only on the WIDTH, so re-rendering it at the height it
// just measured returns the identical string. layout() renders with the leftover-row budget
// and composeBody renders with the panelH that measured, so anything less than this identity
// puts every row below the panel at the wrong terminal row.
func TestKeyPanelHeightIsAFixedPointAtItsOwnHeight(t *testing.T) {
	t.Parallel()

	for _, w := range []int{120, 80, 60, 40, 24, 12, 6, 4, 2} {
		for _, budget := range []int{99, 4, 3, 2, 1, 0} {
			m := Screen{keyPanelOpen: true, width: w}
			once := m.keyPanelView(budget)
			twice := m.keyPanelView(m.keyPanelHeight(budget))
			if once != twice {
				t.Errorf("w=%d budget=%d: keyPanelView(Height(view)) = %q, want the identical %q", w, budget, twice, once)
			}
			if once != "" && lipgloss.Height(once) > budget {
				t.Errorf("w=%d budget=%d: panel height %d exceeds its budget", w, budget, lipgloss.Height(once))
			}
		}
	}
}

// TestKeyPanelDropsColumnsAsTheWidthNarrows pins the reason help.FullHelpView is used at all
// rather than a hand-rolled legend string: it sheds whole trailing columns to fit, so a
// narrow terminal loses the least important keys instead of overflowing. Quit and interrupt
// lead the groups precisely so they are the last to go.
func TestKeyPanelDropsColumnsAsTheWidthNarrows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		width   int
		want    []string
		notWant []string
	}{
		{
			name:  "a wide frame shows every group",
			width: 120,
			want:  []string{"ctrl+c", "quit", "ctrl+n", "next loop", "pgup/pgdn", "page"},
		},
		{
			name:    "a medium frame sheds the scroll group",
			width:   60,
			want:    []string{"ctrl+c", "quit", "ctrl+n", "next loop"},
			notWant: []string{"pgup/pgdn"},
		},
		{
			name:    "a narrow frame keeps only the essentials",
			width:   40,
			want:    []string{"ctrl+c", "quit", "esc", "interrupt", "shift+enter"},
			notWant: []string{"ctrl+n", "pgup/pgdn"},
		},
		{
			name:    "a frame too narrow for one column shows nothing but the ellipsis",
			width:   20,
			notWant: []string{"ctrl+c", "esc", "ctrl+n", "pgup/pgdn"},
		},
		{
			// help.FullHelpView's column-shedding INVERTS below the width of its own "…":
			// with no room for the ellipsis it emits every column at full, unclamped width.
			// The panel must draw nothing rather than a legend wider than the terminal.
			name:    "a frame too narrow for even the ellipsis draws nothing",
			width:   4,
			notWant: []string{"ctrl+c", "quit", "esc", "ctrl+n", "pgup/pgdn"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := Screen{keyPanelOpen: true, width: tt.width}
			got := ansi.Strip(m.keyPanelView(99))
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("panel at width %d = %q, missing %q", tt.width, got, want)
				}
			}
			for _, notWant := range tt.notWant {
				if strings.Contains(got, notWant) {
					t.Errorf("panel at width %d = %q, want %q dropped", tt.width, got, notWant)
				}
			}
			for _, row := range strings.Split(got, "\n") {
				if w := lipgloss.Width(row); w > tt.width {
					t.Errorf("panel row %q is %d wide, want <= the frame's %d", row, w, tt.width)
				}
			}
		})
	}
}

// TestKeyPanelIsIndentedUnderTheComposerText pins the panel's left inset against the
// composer's own frame, so the legend's first key column starts under the composer's text
// rather than under its ▌ accent edge.
func TestKeyPanelIsIndentedUnderTheComposerText(t *testing.T) {
	t.Parallel()

	// The inset is not a taste choice, it is BoxStyle's own horizontal frame (the ▌ accent
	// edge plus one space of padding). Pinning it here is what stops the two drifting apart.
	if got := styles.BoxStyle.GetHorizontalFrameSize(); keyPanelIndent != got {
		t.Errorf("keyPanelIndent = %d, want the composer box's horizontal frame %d", keyPanelIndent, got)
	}

	m := Screen{keyPanelOpen: true, width: 80}
	for i, row := range strings.Split(ansi.Strip(m.keyPanelView(99)), "\n") {
		if !strings.HasPrefix(row, strings.Repeat(" ", keyPanelIndent)) {
			t.Errorf("panel row %d = %q, want a %d-column indent", i, row, keyPanelIndent)
		}
		if strings.HasPrefix(row, strings.Repeat(" ", keyPanelIndent+1)) {
			t.Errorf("panel row %d = %q, want exactly a %d-column indent", i, row, keyPanelIndent)
		}
	}
}

// TestKeyPanelLegendMatchesTheKeysTheShellDispatches pins the legend against reality: every
// binding it advertises must be a key handleKey (or the composer, tray or viewport beneath
// it) actually dispatches on, and every Keys entry must be a real KeyPressMsg.String()
// rendering — the same string key.Matches and handleKey's switch compare against. A legend
// that drifts from the switch is worse than no legend.
func TestKeyPanelLegendMatchesTheKeysTheShellDispatches(t *testing.T) {
	t.Parallel()

	// dispatched is every key string the shell binds, gathered by reading handleKey's global
	// switch, the tray switches, viewportModel.handleKey and components.NewInputBox.
	dispatched := map[string]bool{
		"ctrl+c": true, "ctrl+t": true, "ctrl+n": true, "ctrl+p": true, "esc": true,
		"enter": true, "shift+enter": true, "ctrl+j": true, "tab": true,
		"up": true, "down": true, "pgup": true, "pgdown": true, "home": true, "end": true,
		"?": true,
	}

	seen := map[string]bool{}
	for _, group := range globalKeys.FullHelp() {
		for _, binding := range group {
			h := binding.Help()
			if h.Key == "" || h.Desc == "" {
				t.Errorf("binding %v has an empty help label", binding.Keys())
			}
			for _, k := range binding.Keys() {
				if !dispatched[k] {
					t.Errorf("legend advertises %q, which the shell does not dispatch", k)
				}
				seen[k] = true
			}
		}
	}
	for k := range dispatched {
		if !seen[k] {
			t.Errorf("the shell dispatches %q but the legend never mentions it", k)
		}
	}
}

// TestKeyPanelStaysOpenAcrossNonKeyMessages pins that the dismiss is driven by KEYS alone. A
// cursor-blink tick, a status tick and a streamed event all arrive constantly; if any of them
// closed the panel it would flicker shut on its own before the user could read it.
func TestKeyPanelStaysOpenAcrossNonKeyMessages(t *testing.T) {
	t.Parallel()

	loopID := callID(1)
	m := newScreenSized(t, &fakeAgent{activeLoopID: loopID}, 80, 24)
	m, _ = updateScreen(t, m, shiftSlashKey())
	if !m.keyPanelOpen {
		t.Fatal("panel did not open")
	}

	m = feed(t, m, event.TurnStarted{Header: hdr(loopID), Message: userMsg("q")})
	if !m.keyPanelOpen {
		t.Error("a streamed event closed the panel; only a key press may dismiss it")
	}
	m, _ = updateScreen(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if !m.keyPanelOpen {
		t.Error("a resize closed the panel; only a key press may dismiss it")
	}
	if m.layout().panelH == 0 {
		t.Error("panelH = 0 after a resize; the panel must re-measure at the new width")
	}
}
