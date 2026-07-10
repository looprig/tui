package styles

import "charm.land/lipgloss/v2"

// CardBorderColor tints the gate card's rounded border — and the pressable
// accelerator keys in its footer — a soft brand blue (the same calm accent as
// MarkdownHeadingColor). A pending permission/AskUser gate is an action-required
// FOREGROUND affordance, so it reads as a tinted card, distinct from the faint,
// borderless tool cards and the square-edged composer, and stays legible on a dark
// terminal.
var CardBorderColor = lipgloss.Color(MarkdownHeadingColor)

// CardStyle frames a gate (permission or AskUser) as a bordered, padded card. It
// REUSES PromptBoxStyle's rounded border (so the border glyphs stay identical to the
// prior prompt box) and adds one column of horizontal padding on each side plus the
// brand border tint, giving the body an airy inset the compact control lacked.
//
// Vertical padding is deliberately ZERO: the card's top+bottom frame must stay exactly
// two rows so surface.go's boxBorderH (the border rows the bottom-box height
// measurement reserves) remains correct. Deriving from PromptBoxStyle is safe — a
// lipgloss v2 Style is a value whose setters return an independent copy, so this never
// mutates the shared PromptBoxStyle.
var CardStyle = PromptBoxStyle.
	Padding(0, 1).
	BorderForeground(CardBorderColor)

// CardTitleStyle renders the bold card title row — "Approve Bash?", the AskUser
// question, or the "answer" label — so the required action reads at a glance above the
// card body.
var CardTitleStyle = lipgloss.NewStyle().Bold(true)

// CardKeyStyle renders the bracketed accelerator of a footer key hint ("[y]", "[o]")
// in the bold brand accent, so the pressable key stands out from its muted label.
var CardKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(CardBorderColor)

// CardHintStyle renders a card's muted secondary text — a key hint's label ("once"),
// the choice key legend, the free-text submit hint, and the "(+N more pending)"
// queue-depth note — faint so it recedes beneath the title and body.
var CardHintStyle = lipgloss.NewStyle().Faint(true)

// CardSelectedStyle highlights the selected choice row: bold with a subtle dark fill
// spanning the row, so the current selection reads as a filled bar (the opencode
// dialog look) rather than only the ▸ cursor. The fill reuses the neutral panel gray
// so it stays readable on a dark terminal; the caller sets the row Width so the fill
// spans the full card body.
var CardSelectedStyle = lipgloss.NewStyle().Bold(true).Background(ModernPanelBg)
