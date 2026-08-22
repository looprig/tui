package components

import (
	"charm.land/bubbles/v2/list"
)

// ValueItem is one typed runtime choice. ID is the opaque payload returned on selection.
type ValueItem struct {
	ID          string
	Label       string
	Description string
	Aliases     []string
}

// ValueComplete is the runtime-choice tray -- in practice the model picker -- over the
// shared list engine.
//
// items is EVERY choice in catalog order, not just the matching ones: the engine owns
// filtering and hands back the UNFILTERED index of the selection, so the payload lookup
// stays a direct index rather than a second, parallel slice that could drift out of step
// with the rows the engine is actually showing.
type ValueComplete struct {
	list  *trayList
	items []ValueItem
}

// NewValueComplete builds the tray for items, narrowed to query. It returns nil when nothing
// matches: nil means "no panel at all", and the caller commits a notice rather than showing
// an empty tray that the arrow keys cannot move within.
func NewValueComplete(items []ValueItem, query string) *ValueComplete {
	choices := make([]ValueItem, len(items))
	rows := make([]completionTrayRow, len(items))
	for i, item := range items {
		// The aliases are copied because the tray outlives this call: the slice behind
		// them belongs to whoever supplied the choices and a catalog refresh can rewrite
		// it, and a live tray must not change what the next keystroke filters against.
		item.Aliases = append([]string(nil), item.Aliases...)
		choices[i] = item
		rows[i] = completionTrayRow{primary: item.Label, secondary: item.Description}
	}

	// Width zero: this panel is only ever drawn through ViewWindow, which is handed the
	// terminal's width at render time, so there is no width worth guessing here.
	tray := newTrayList(rows, 0, trayLayout{})
	tray.SetFilterFunc(valueFilter(choices))
	// trayList.Filter leaves a blank query UNFILTERED rather than applying "", which is what
	// keeps UnfilteredCursor below honest; see its doc for why an empty term is not harmless.
	tray.Filter(query)
	if tray.Len() == 0 {
		return nil
	}
	return &ValueComplete{list: tray, items: choices}
}

// valueFilter is the picker's match rule: fuzzy over the LABEL, falling back to fuzzy over
// the item's aliases. Both go through list.DefaultFilter, so the panel matches exactly the
// way the engine's own filter does instead of growing a second notion of "fuzzy".
//
// Only label matches carry rune indices, and that is the point. The label is what
// trayItem.FilterValue returns and what trayDelegate underlines, so its indices map 1:1 onto
// the drawn column. An alias is never drawn, so there is nothing on screen its indices could
// point at; handing them back would underline unrelated runes of the label, and every
// underline after the first would sit one rune off. An alias hit is therefore reported as a
// match with NO indices: the row appears, un-underlined. Nothing is lost that the user can
// see -- the row matched something they typed which simply is not on screen -- and aliases
// keep doing the job they exist for, typing "opus" to reach claude-opus-5 or "fast" to reach
// a model whose name says nothing about speed.
//
// The DESCRIPTION is deliberately NOT searched, which the substring matcher this replaces
// did. Fuzzy is a subsequence test, and a short query is a subsequence of nearly any
// sentence, so searching prose would leave the picker showing almost every choice for the
// first two or three keystrokes -- and showing them with no underline to explain why. A
// description here says what a choice is FOR; it is not how anyone reaches for one.
func valueFilter(items []ValueItem) list.FilterFunc {
	return func(term string, labels []string) []list.Rank {
		ranks := list.DefaultFilter(term, labels)
		byLabel := make(map[int]bool, len(ranks))
		for _, rank := range ranks {
			byLabel[rank.Index] = true
		}
		// Appended AFTER the label matches, so a name the user can see always outranks a
		// nickname they cannot, and alias hits keep catalog order among themselves.
		for i, item := range items {
			if byLabel[i] || len(list.DefaultFilter(term, item.Aliases)) == 0 {
				continue
			}
			ranks = append(ranks, list.Rank{Index: i})
		}
		return ranks
	}
}

// Selected is the choice under the cursor. It resolves through the engine's UNFILTERED
// index, so the opaque ID survives filtering however the matcher reordered the rows.
func (v *ValueComplete) Selected() ValueItem {
	i := v.list.UnfilteredCursor()
	if i < 0 || i >= len(v.items) {
		return ValueItem{} // fail-safe: a tray can be emptied between a keypress and a render
	}
	return v.items[i]
}

// Cursor is the selected index among the MATCHING choices, not among all of them.
func (v *ValueComplete) Cursor() int { return v.list.Cursor() }

// Len is how many choices matched, which is what the tray draws.
func (v *ValueComplete) Len() int { return v.list.Len() }

// Up and Down move the cursor, wrapping at both ends.
func (v *ValueComplete) Up()   { v.list.Up() }
func (v *ValueComplete) Down() { v.list.Down() }

// SelectWindowRow moves the cursor to a visual row of the rendered window and reports
// whether the selection changed.
func (v *ValueComplete) SelectWindowRow(row, maxRows int) bool {
	return v.list.SelectWindowRow(row, maxRows)
}

// ViewWindow renders at most maxRows rows at width columns, keeping the selection on screen.
func (v *ValueComplete) ViewWindow(width, maxRows int) string {
	return v.list.ViewWindow(width, maxRows)
}
