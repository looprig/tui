package presentation

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/looprig/tui/styles"
)

// keyMap is the frame's KEY LEGEND — the single declaration of every keystroke the shell
// itself binds, grouped into the columns the panel renders. It is the documentation half of
// the key contract: handleKey still DISPATCHES on msg.String() (Key.Code is modifier-blind,
// so ctrl+a, alt+a and A all carry Code == 'a' — see interaction.go's accelerator note), and
// key.Matches compares k.String() too, so a binding here and the switch there agree by
// construction.
//
// It is deliberately NOT the /help slash command. That command commits the SLASH-COMMAND
// catalogue into the transcript, where it scrolls away with every other message. This is
// transient chrome listing KEYS, drawn below the composer and never committed.
//
// Every Keys value must be a real tea.KeyPressMsg.String() rendering; the Help strings are
// the human labels. The two differ where one action has several bindings (newline is
// shift+enter OR ctrl+j — see components.NewInputBox for why both) or where a pair reads
// better joined ("pgup/pgdn").
type keyMap struct {
	// quit, interrupt, foldAll, nextLoop and prevLoop are Screen.handleKey's global chords.
	quit      key.Binding
	interrupt key.Binding
	foldAll   key.Binding
	nextLoop  key.Binding
	prevLoop  key.Binding
	// send and newline are the composer's; complete and move additionally drive an open
	// completion tray (slash, @path, /mode and /resume all reuse the same tray keys).
	send     key.Binding
	newline  key.Binding
	complete key.Binding
	move     key.Binding
	// page and ends are viewportModel.handleKey's scroll keys — deliberately NOT the arrow
	// keys, which belong to the composer/completion layer.
	page key.Binding
	ends key.Binding
	// panel is the `?` that opens this legend.
	panel key.Binding
}

// globalKeys is the shell's one key legend. It is a package-level value rather than
// per-Screen state because the bindings are FIXED — nothing enables, disables or rebinds
// them at runtime, so there is nothing per-frame to own.
var globalKeys = keyMap{
	quit:      key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "quit")),
	interrupt: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "interrupt")),
	foldAll:   key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "fold all")),
	nextLoop:  key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "next loop")),
	prevLoop:  key.NewBinding(key.WithKeys("ctrl+p"), key.WithHelp("ctrl+p", "prev loop")),
	send:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "send")),
	newline:   key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"), key.WithHelp("shift+enter", "newline")),
	complete:  key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "complete")),
	move:      key.NewBinding(key.WithKeys("up", "down"), key.WithHelp("↑/↓", "completions")),
	page:      key.NewBinding(key.WithKeys("pgup", "pgdown"), key.WithHelp("pgup/pgdn", "page")),
	ends:      key.NewBinding(key.WithKeys("home", "end"), key.WithHelp("home/end", "top, bottom")),
	panel:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "keys")),
}

// FullHelp returns the legend's columns, MOST IMPORTANT FIRST. The order is load-bearing:
// help.Model.FullHelpView drops whole trailing columns as the width narrows, so the leftmost
// group is the one that survives the narrowest frame. Quit and interrupt lead for that
// reason — a user who cannot get out is stuck, a user who cannot see the scroll keys is only
// inconvenienced.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.quit, k.interrupt, k.send, k.newline},
		{k.nextLoop, k.prevLoop, k.foldAll, k.panel},
		{k.page, k.ends, k.move, k.complete},
	}
}

// keyPanelIndent is the panel's left inset in columns. It equals styles.BoxStyle's horizontal
// frame (the ▌ accent edge + one space of left padding), so the legend's first key column
// starts at the same column as the composer's text directly above it.
const keyPanelIndent = 2

// keyPanelHelp is the configured help renderer for width w. It is built per call rather than
// stored on Screen because it holds no state beyond that width — help.Model's only field the
// panel varies is the width SetWidth records, and the styles are constants.
//
// The two styles are the existing card tokens, not help's own defaults: CardKeyStyle (bold
// brand blue) for the keys and CardHintStyle (faint) for the labels, the same pairing the
// gate cards' footer key hints use, so a key reads as pressable in both places. The
// separator and ellipsis take the faint token as well so a dropped column's "…" recedes.
func keyPanelHelp(w int) help.Model {
	h := help.New()
	h.Styles.FullKey = styles.CardKeyStyle
	h.Styles.FullDesc = styles.CardHintStyle
	h.Styles.FullSeparator = styles.CardHintStyle
	h.Styles.Ellipsis = styles.CardHintStyle
	h.SetWidth(w)
	return h
}

// keyPanelView renders at most maxRows of the transient key legend, or "" when the panel is
// closed, has no row budget, or is too narrow for even one column (FullHelpView returns ""
// once every column has been dropped).
//
// HEIGHT STABILITY (the layout contract): the rendered legend depends ONLY on the frame
// WIDTH — help.FullHelpView lays the groups out as columns and drops trailing ones to fit
// SetWidth, so its height is max(rows per surviving column). maxRows then clamps that
// result; it never feeds back into the render. So clamping the panel to its OWN height is
// the identity, i.e.
//
//	keyPanelView(Height(keyPanelView(budget))) == keyPanelView(budget)
//
// which is the same fixed point the completion tray's ViewWindow has, and for the same
// reason: layout() and composeBody BOTH render this, layout() with the remaining-row budget
// and composeBody with the panelH that measured, and the rows below the panel sit at the
// wrong terminal rows the instant those two disagree.
func (m Screen) keyPanelView(maxRows int) string {
	if !m.keyPanelOpen || maxRows <= 0 {
		return ""
	}
	inner := m.width - keyPanelIndent
	if inner <= 0 {
		return ""
	}
	view := keyPanelHelp(inner).FullHelpView(globalKeys.FullHelp())
	// FullHelpView normally drops columns to fit, ending at worst with a lone "…". It has one
	// degenerate case: when the frame is too narrow for even that ellipsis (inner <= 2 columns)
	// its shouldAddItem check flips and it emits EVERY column at full, unclamped width. A
	// legend wider than the frame it sits in is not a legend, so the panel simply does not
	// appear on a frame that narrow.
	if view == "" || lipgloss.Width(view) > inner {
		return ""
	}
	rows := strings.Split(view, "\n")
	if len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	indent := strings.Repeat(" ", keyPanelIndent)
	for i, row := range rows {
		rows[i] = indent + row
	}
	return strings.Join(rows, "\n")
}

// keyPanelToggles reports whether msg is the `?` that OPENS/CLOSES the key panel rather than
// a literal question mark bound for the composer.
//
// The collision is the whole point: `?` is an ordinary printable character, so it may only be
// stolen when it cannot possibly be input. That means compose mode (a prompt's card owns its
// own accelerators, and an answer prompt's field takes text) with NO tray open (the trays
// swallow every key they do not bind, and a `?` there means nothing) and an EMPTY editor —
// once there is a single character in the composer the user is writing, and a `?` belongs to
// what they are writing. Emptiness is tested EXACTLY, not trimmed: a lone space is still
// something the user typed.
//
// Matching goes through key.Matches, which compares k.String(). Matching msg.Code would be
// the bug: Code is the unshifted, unmodified key, so on a US layout `?` is shift+/ and
// arrives with Code == '/' — the same Code a plain slash carries, which would open the panel
// every time someone started a slash command. Guarding on msg.Mod instead is no better: num
// lock is reported as a modifier on ordinary printable keys.
func (m Screen) keyPanelToggles(msg tea.KeyPressMsg) bool {
	if !key.Matches(msg, globalKeys.panel) {
		return false
	}
	if m.interaction.mode != modeCompose {
		return false
	}
	if m.runtimeTray != nil || m.sessionTray != nil {
		return false
	}
	return m.interaction.input.Value() == ""
}

// keyPanelHeight measures the panel's rows within a budget of remaining frame rows, guarding
// the empty-panel case: lipgloss.Height("") is 1, not 0, so a closed panel measured naively
// would steal a row from the transcript on every frame.
func (m Screen) keyPanelHeight(budget int) int {
	panel := m.keyPanelView(budget)
	if panel == "" {
		return 0
	}
	return lipgloss.Height(panel)
}
