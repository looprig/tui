package components

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
)

// ValueItem is one typed runtime choice. ID is the opaque payload returned on selection.
type ValueItem struct {
	ID          string
	Provider    string
	Label       string
	Description string
	Aliases     []string
	Current     bool
}

// ValueComplete is the runtime-choice tray -- in practice the model picker -- over the
// shared list engine.
//
// items is EVERY choice in catalog order, not just the matching ones: the engine owns
// filtering and hands back the UNFILTERED index of the selection, so the payload lookup
// stays a direct index rather than a second, parallel slice that could drift out of step
// with the rows the engine is actually showing.
type ValueComplete struct {
	list      *trayList
	items     []ValueItem
	rowToItem []int
	title     string
	query     string
}

// NewValueComplete builds the tray for items, narrowed to query. It returns nil when nothing
// matches: nil means "no panel at all", and the caller commits a notice rather than showing
// an empty tray that the arrow keys cannot move within.
func NewValueComplete(items []ValueItem, query string) *ValueComplete {
	return newValueComplete(items, query, false)
}

// NewModelComplete builds the grouped model picker. Provider headings are rows in the same
// panel so they stay aligned with the selection, but the shared tray marks them inert: a
// cursor or pointer may only select a model payload.
func NewModelComplete(items []ValueItem) *ValueComplete {
	return newValueComplete(items, "", true)
}

func newValueComplete(items []ValueItem, query string, grouped bool) *ValueComplete {
	choices := make([]ValueItem, len(items))
	for i, item := range items {
		// The aliases are copied because the tray outlives this call: the slice behind
		// them belongs to whoever supplied the choices and a catalog refresh can rewrite
		// it, and a live tray must not change what the next keystroke filters against.
		item.Aliases = append([]string(nil), item.Aliases...)
		choices[i] = item
	}
	if len(choices) == 0 {
		return nil
	}

	rows := make([]completionTrayRow, 0, len(choices))
	rowToItem := make([]int, 0, len(choices))
	if grouped {
		rows, rowToItem = groupedModelRows(choices)
	} else {
		for i, item := range choices {
			rows = append(rows, completionTrayRow{primary: item.Label, secondary: item.Description, filter: item.Label, current: item.Current})
			rowToItem = append(rowToItem, i)
		}
	}

	// Width zero: this panel is only ever drawn through ViewWindow, which is handed the
	// terminal's width at render time, so there is no width worth guessing here.
	tray := newTrayList(rows, 0, trayLayout{})
	if grouped {
		tray.SetFilterFunc(modelFilter(choices, rows, rowToItem))
	} else {
		tray.SetFilterFunc(valueFilter(choices))
	}
	// trayList.Filter leaves a blank query UNFILTERED rather than applying "", which is what
	// keeps UnfilteredCursor below honest; see its doc for why an empty term is not harmless.
	tray.Filter(query)
	if tray.ChoiceLen() == 0 {
		return nil
	}
	result := &ValueComplete{list: tray, items: choices, rowToItem: rowToItem}
	if grouped {
		result.title = "MODELS"
	}
	return result
}

func groupedModelRows(items []ValueItem) ([]completionTrayRow, []int) {
	type providerGroup struct {
		name    string
		indices []int
	}
	groups := make([]providerGroup, 0, len(items))
	byProvider := make(map[string]int, len(items))
	for i, item := range items {
		provider := strings.TrimSpace(item.Provider)
		if provider == "" {
			provider = "Other"
		}
		key := strings.ToLower(provider)
		group, ok := byProvider[key]
		if !ok {
			group = len(groups)
			byProvider[key] = group
			groups = append(groups, providerGroup{name: provider})
		}
		groups[group].indices = append(groups[group].indices, i)
	}

	rows := make([]completionTrayRow, 0, 2*len(groups)+len(items))
	rowToItem := make([]int, 0, 2*len(groups)+len(items))
	for _, group := range groups {
		// A blank row separates the previous provider's models from this heading, so the
		// groups read as blocks rather than one unbroken column. Never before the FIRST
		// heading: that gap would sit under the tray header, which already ends in one.
		if len(rows) > 0 {
			rows = append(rows, completionTrayRow{kind: trayRowSpacer})
			rowToItem = append(rowToItem, -1)
		}
		rows = append(rows, completionTrayRow{primary: strings.ToUpper(group.name), filter: group.name, kind: trayRowHeading})
		rowToItem = append(rowToItem, -1)
		for _, index := range group.indices {
			item := items[index]
			rows = append(rows, completionTrayRow{primary: item.Label, secondary: item.Description, filter: item.Label, current: item.Current})
			rowToItem = append(rowToItem, index)
		}
	}
	return rows, rowToItem
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

// modelFilter keeps provider headings only when that provider or one of its models matches.
// A provider hit keeps its entire group, while name hits preserve useful underlines and alias
// or opaque-ID hits deliberately carry no underline because those strings are not on the row.
//
// A group's leading spacer survives with it, EXCEPT when the group would be first in the
// filtered projection: a gap above the topmost heading is a blank row hanging off the tray
// header, not separation between anything.
func modelFilter(items []ValueItem, rows []completionTrayRow, rowToItem []int) list.FilterFunc {
	return func(term string, labels []string) []list.Rank {
		var ranks []list.Rank
		for start := 0; start < len(rows); {
			if rows[start].kind != trayRowSpacer && rows[start].kind != trayRowHeading {
				start++
				continue
			}
			head := start
			for head < len(rows) && rows[head].kind == trayRowSpacer {
				head++
			}
			if head >= len(rows) || rows[head].kind != trayRowHeading {
				start = head // a spacer with no heading behind it belongs to no group
				continue
			}
			end := head + 1
			for end < len(rows) && rowToItem[end] >= 0 {
				end++
			}
			providerMatch := len(list.DefaultFilter(term, []string{labels[head]})) > 0
			groupRanks := make([]list.Rank, 0, end-head-1)
			for row := head + 1; row < end; row++ {
				item := items[rowToItem[row]]
				name := list.DefaultFilter(term, []string{item.Label})
				switch {
				case providerMatch:
					groupRanks = append(groupRanks, list.Rank{Index: row})
				case len(name) > 0:
					groupRanks = append(groupRanks, list.Rank{Index: row, MatchedIndexes: name[0].MatchedIndexes})
				case len(list.DefaultFilter(term, item.Aliases)) > 0 || len(list.DefaultFilter(term, []string{item.ID})) > 0:
					groupRanks = append(groupRanks, list.Rank{Index: row})
				}
			}
			if len(groupRanks) > 0 {
				if len(ranks) > 0 {
					for row := start; row < head; row++ {
						ranks = append(ranks, list.Rank{Index: row})
					}
				}
				ranks = append(ranks, list.Rank{Index: head})
				ranks = append(ranks, groupRanks...)
			}
			start = end
		}
		return ranks
	}
}

// Selected is the choice under the cursor. It resolves through the engine's UNFILTERED
// index, so the opaque ID survives filtering however the matcher reordered the rows.
func (v *ValueComplete) Selected() ValueItem {
	if v.list.ChoiceLen() == 0 {
		return ValueItem{}
	}
	row := v.list.UnfilteredCursor()
	if row < 0 || row >= len(v.rowToItem) {
		return ValueItem{}
	}
	i := v.rowToItem[row]
	if i < 0 || i >= len(v.items) {
		return ValueItem{} // fail-safe: a tray can be emptied between a keypress and a render
	}
	return v.items[i]
}

// Cursor is the selected index among the MATCHING choices, not among all of them.
func (v *ValueComplete) Cursor() int { return v.list.Cursor() }

// Len is how many choices matched, which is what the tray draws.
func (v *ValueComplete) Len() int { return v.list.ChoiceLen() }

// Up and Down move the cursor, wrapping at both ends.
func (v *ValueComplete) Up()   { v.list.Up() }
func (v *ValueComplete) Down() { v.list.Down() }

// Filter updates a live model search without recreating the picker. Plain runtime pickers use
// the same method when a caller wants it, while their existing constructor query remains valid.
func (v *ValueComplete) Filter(query string) {
	v.query = query
	if strings.TrimSpace(query) == "" {
		v.list.ResetFilter()
		return
	}
	v.list.Filter(query)
}

func (v *ValueComplete) summary() string {
	matched, total := v.Len(), len(v.items)
	noun := "models"
	if total == 1 {
		noun = "model"
	}
	if strings.TrimSpace(v.query) == "" {
		return fmt.Sprintf("%d %s", total, noun)
	}
	return fmt.Sprintf("%d of %d %s", matched, total, noun)
}

// SelectWindowRow moves the cursor to a visual row of the rendered window and reports
// whether the selection changed.
func (v *ValueComplete) SelectWindowRow(row, maxRows int) bool {
	if v.title != "" {
		if row < trayHeaderHeight || maxRows <= trayHeaderHeight {
			return false
		}
		return v.list.SelectWindowRow(row-trayHeaderHeight, maxRows-trayHeaderHeight)
	}
	return v.list.SelectWindowRow(row, maxRows)
}

// ViewWindow renders at most maxRows rows at width columns, keeping the selection on screen.
func (v *ValueComplete) ViewWindow(width, maxRows int) string {
	if v.title == "" {
		return v.list.ViewWindow(width, maxRows)
	}
	if width <= 0 || maxRows < trayHeaderHeight {
		return ""
	}
	header := renderTrayHeader(width, v.title, v.summary())
	bodyRows := maxRows - trayHeaderHeight
	if body := v.list.ViewWindow(width, bodyRows); body != "" {
		return header + "\n" + body
	}
	if strings.TrimSpace(v.query) != "" && bodyRows > 0 {
		return header + "\n" + renderTrayHint(width, "No matching models")
	}
	return header
}
