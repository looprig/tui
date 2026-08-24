# Bubbles v2 Component Adoption Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Adopt two new Bubbles v2 subpackages (`list`, `help`) plus `textarea` in a new role behind the module's existing surfaces, gaining fuzzy command filtering, real cursor editing in form gates, and a width-aware key panel — without changing a single rendered pixel that was not deliberately chosen.

**Architecture:** Every adoption goes *behind* the existing public shape. The four completion panels keep their `Selected()/Cursor()/Up()/Down()/SelectWindowRow()/ViewWindow*()` API and swap `list.Model` in underneath via a custom `ItemDelegate` that calls the current renderer, so `screen.go`, `interaction.go` and ~700 lines of existing tests stay untouched. Form fields gain a `textarea.Model` per `gate.FieldText` row, growing as content wraps. A new `?` key panel uses `help.FullHelpView`; the `/help` slash command is unchanged. One selection treatment is extracted so the tray, the AskUser card and the permission card cannot drift apart.

**Tech Stack:** Go 1.26.6, `charm.land/bubbletea/v2 v2.0.8`, `charm.land/bubbles/v2 v2.1.1`, `charm.land/lipgloss/v2 v2.0.6`. New transitive dep: `github.com/sahilm/fuzzy` (via `bubbles/list`).

---

## Standing constraint: this is greenfield

There is no backwards-compatibility obligation. Do not preserve an old component, an old renderer, or an old behaviour merely because it exists. If the new shared component subsumes something, **delete the old thing** rather than keeping it alongside.

Corollaries every task inherits:
- An existing test that pins a RETIRED behaviour is deleted, not adapted. (A test pinning behaviour that still matters is still the contract — the distinction is whether the behaviour survives, not whether the test is old.)
- **No "insurance" tests for behaviour that cannot currently fail.** An assertion that is unobservable today is dead weight; delete it and let the test come back with the feature if the feature ever comes back.
- **No defensive redundancy** kept "just in case" when a single guard already covers it.
- `staticcheck` (U1000, via `make lint`) is the backstop and it is authoritative: if it flags something as unused, that is the answer — delete it. Do not silence it or keep a caller alive to satisfy it.

## Decisions already made (do not relitigate)

| Area | Decision |
|---|---|
| Tray engine | `bubbles/list` + a custom `ItemDelegate` reusing `renderCompletionTrayBackground`. Pixel-identical unfiltered. |
| Tray scope | **All four trays share ONE component**: slash (`/` commands), files (`@path`), runtime (**the model list**, `ValueComplete`), sessions (`/resume`). `screen.go:849` confirms these are the whole set. |
| Tray capabilities | The shared component gains `Stacked`, `PadV` and `PadH` so the session tray's two-line layout becomes a CONFIGURATION of it, not a second renderer. `sessioncomplete.go`'s bespoke `renderSessionLine` is deleted. |
| Model list filtering | Fuzzy, same as the others — `sn45` finds `claude-sonnet-4-5`. Today's `valueMatches` substring check is replaced. |
| Tray selection fill | App blue `#A2D2FF`, the shared `MarkdownHeadingColor` token. |
| Tray descriptions | Aligned to a common column, computed from the widest primary among **visible** rows. |
| Tray cursor | `InfiniteScrolling = true` — wraps, matching today's modulo cursor. |
| Tray filtering | `list.DefaultFilter` (fuzzy, `sahilm/fuzzy`) with per-rune match underline via `MatchesForItem`. |
| AskUser | **Vertical numbered list.** Inline chips were prototyped and **rejected**. |
| AskUser accelerators | Bracketed, bold blue: `[1] main`. |
| Gate | Selectable action rows, bracketed `[y]/[a]/[n]`, **no per-action consequence notes**. |
| Selection glyph | **None.** No `▸`. The filled band is the only selection affordance, everywhere. |
| Selection API | `styles.SelectedRow(row string, width int)` — ONE argument. It strips the row internally when the fill is light, so a caller never builds the same row twice. It pads to `width` but does NOT truncate; clamping stays the caller's job. |
| Form fields | `bubbles/textarea` per `gate.FieldText` row — grows with content. |
| Help | `/help` unchanged. New `?` opens a transient `help.FullHelpView` panel below the composer, auto-dismissed by the next key. |
| bubblezone | **Not adopted.** Not a transitive dep; would be a new direct one; does not do text-cursor positioning. |

**All design questions are resolved.** The three that were open are settled as:

| Question | Answer | Consequence |
|---|---|---|
| Tray selection colour | **App blue `#A2D2FF`** (`MarkdownHeadingColor`) | One semantic blue across rails, accelerators and selection. The token is LIGHT, so a selected row's text flips near-black and its bold-blue accelerator loses its blue on that row — deliberate, accepted. |
| `?` panel dismissal | **Auto-dismiss on next key** | The panel is a peek. The dismissing key still performs its normal job — it is a dismiss, not a swallow. The frame returns to full height immediately, so `panelH` is short-lived. |
| Form field widget | **`textarea`, not `textinput`** | Fields grow as content wraps. `enter` must be explicitly unbound so it still submits; newline binds to `shift+enter`/`ctrl+j`, matching `components/input.go`. The form card recomputes height per render. Sidesteps the `textinput` +1 width quirk entirely. |

**Dependency note:** with `textarea` chosen for form fields, `bubbles/textinput` is **no longer needed** — `textarea` is already an approved subpackage. Task 0's approval list therefore adds only `list` and `help`.

---

## Gotchas discovered while prototyping (all verified against v2.1.1 source)

- **`textinput.View()` renders `SetWidth()+1` columns** when the cursor sits at the end of the text: it appends the cursor cell and *then* pads out to `Width()`. Any caller filling a fixed-width row must budget the extra column, or the row overflows and the terminal wraps it. `textarea` does not do this.
- **`textinput` has no height concept at all** — no `Height`/`MinHeight`/`MaxHeight`. It is single-line and scrolls horizontally. Anything that must grow uses `textarea`.
- **`list.DefaultDelegate` silently renders nothing** unless items implement `list.DefaultItem` (`Title()` + `Description()`). No error, no panic. We use our own delegate, so this only matters if someone reaches for the stock one.
- **`list.Model` clamps at the ends by default.** `InfiniteScrolling` must be set explicitly to match today's wrap.
- **`list.New` hardcodes dark styles** (`DefaultStyles(true)`, with an `XXX:` in the source). Harmless — we override wholesale — and it happens to suit this module's deliberate avoidance of OSC-11 background queries.
- **`FilterValue()` drives `MatchesForItem` rune indices.** Return the *primary* string only, so the indices map 1:1 onto the column being underlined.
- **The space bar does NOT stringify to `" "`.** `ultraviolet.Key.String()` is `if len(k.Text) > 0 && k.Text != " " { return k.Text }; return k.Keystroke()`, and the keystroke table maps `KeySpace: "space"`. Any handler that collects printable input with `len(msg.String()) == 1` **silently eats the space bar**. Use `msg.Code` with `unicode.IsPrint` (what `prompt.typeRune` already does correctly) or `msg.Text`. Never `String()`.
- **The composer rail is `InputAccent #737373`, not `CardBorderColor #A2D2FF`.** `BoxStyle.BorderForeground(InputAccent)` (styles.go:319). The blue rail is reserved for gate and AskUser cards — action-required affordances. Painting the composer's edge blue makes the input box read as a card demanding a response.
- **`textarea` wraps at `SetWidth()`, not at the box width.** If a field looks like it "is not wrapping", check the column budget before suspecting the widget: at width 70 you need ~70 characters before the first wrap. Surface the wrap point in any debug UI.
- **`textarea.MaxHeight` is BOTH the visible cap AND an input gate** — once the logical line count reaches it, the textarea silently refuses further newlines and drops content. Park it high (`components/input.go` uses 10000) and apply the visible cap separately via `SetHeight` after every mutation.

---

## Task 0: Amend the approved-packages block

**Files:**
- Modify: `tui/CLAUDE.md` (the `<!-- Approved external packages -->` list)

**Step 1: Edit the bubbles entry**

The current entry sanctions only `textarea`, `viewport`, `key`. Replace it with:

```markdown
- `github.com/charmbracelet/bubbles` — textarea + viewport widgets for the TUI. **v2 approved (2026-06-15)** (co-required by Bubble Tea v2). v2 import path: `charm.land/bubbles/v2`. **Subpackages `list` and `help` additionally approved (2026-08-22)** for the completion tray and the `?` key panel. `textarea` (already approved) additionally serves form-gate text fields.
- `github.com/sahilm/fuzzy` — transitive (indirect) dep of `bubbles/list`, which imports it for `DefaultFilter`; approved as part of `list`, not chosen directly
```

**Step 2: Verify no other dep moved**

Run: `cd tui && GOWORK=off go mod graph | grep -c bubblezone`
Expected: `0` — bubblezone must not enter the graph.

**Step 3: Commit**

```bash
git add tui/CLAUDE.md
git commit -m "docs: approve bubbles list and help subpackages"
```

---

## Task 1: Extract the one selection treatment — DONE (commit `9d5b908` + review fixes)

Today the tray bands the selected row (`CardSelectedStyle` + `FillLineBackgroundWith`) while `choiceRow` uses `"▸ " + label`. Two treatments, two surfaces. Unified in `styles/selection.go` so every later task inherits one.

**Final API — this is what later tasks call:**

```go
// SelectedRow bands an already-styled row with the selection fill across width columns.
// It takes the row ONCE: when the fill is light it strips the row internally and
// re-renders it near-black for contrast, so a caller never builds the same row twice and
// cannot get two arguments backwards. There is deliberately NO cursor glyph — the band is
// the cursor. It pads out to width but deliberately does NOT truncate; clamping is the
// caller's job, because only the caller knows which trailing segment is safe to drop.
func SelectedRow(row string, width int) string
```

Behaviour later tasks rely on:
- The fill is `SelectionBg`, the shared `CardBorderColor` / `#A2D2FF` app-blue token.
- Whether inner styling survives is **derived** from the fill's luma, never stored — so it can never contradict the colour it describes.
- A degenerate fill (`DeriveBackgroundSGR` returns `open == ""`, or a nil colour) returns the row **untouched**: no band, but still legible. It must never emit a foreground with no background behind it.

Review findings that shaped this (all verified empirically, do not regress them):
- Applying the near-black foreground before checking the fill exists renders black-on-black.
- A two-argument `(styled, plain)` form silently accepts swapped or duplicated arguments.
- Storing darkness alongside the colour lets the two contradict into invisible output.

---

## Task 2: Route the existing tray through SelectedRow

**Files:**
- Modify: `tui/components/completiontray.go` (`renderCompletionTrayBackground`)
- Test: `tui/components/completiontray_test.go` (new)

**Step 1: Write a characterisation test FIRST**

This is the safety net for every later change. Capture today's exact bytes before touching anything.

```go
func TestTrayRenderIsStable(t *testing.T) {
	rows := []completionTrayRow{
		{primary: "/clear", secondary: "start a new conversation"},
		{primary: "/compact", secondary: "compact the current conversation"},
	}
	got := renderCompletionTray(rows, 1, 44)
	// Golden: regenerate ONLY when a pixel change is deliberate.
	if want := trayGolden; got != want {
		t.Errorf("tray render changed:\n got %q\nwant %q", got, want)
	}
}
```

Generate `trayGolden` by printing `%q` of the current output once, then paste it in.

**Step 2: Run to confirm it passes against today's code**

Run: `cd tui && go test ./components/ -run TestTrayRenderIsStable -v`
Expected: PASS. If it fails, the golden was captured wrong — fix before proceeding.

**Step 3: Swap the selected branch to SelectedRow**

In `renderCompletionTrayBackground`, replace the selected-row construction so the row is styled first and banded second:

```go
	pad := strings.Repeat(" ", max(0, descCol-ansi.StringWidth(row.primary)))
	styled := rail + " " + primary + pad
	if row.secondary != "" {
		styled += "  " + styles.CardHintStyle.Render(row.secondary)
	}
	if i == selected {
		// SelectedRow takes the row ONCE and strips it internally when the fill is
		// light. Truncation stays the caller's job — SelectedRow pads to width but
		// deliberately does not clamp, because only the caller knows which trailing
		// segment is safe to drop.
		rendered[i] = styles.SelectedRow(ansi.Truncate(styled, width, ""), width)
		continue
	}
```

**Step 4: Run the golden test**

Run: `cd tui && go test -race ./components/ -v`
Expected: PASS — unchanged bytes. If it fails, the refactor changed pixels; fix it, do not update the golden.

**Step 5: Commit**

```bash
git add tui/components/completiontray.go tui/components/completiontray_test.go
git commit -m "refactor(components): tray selection uses the shared treatment"
```

---

## Task 3: Aligned description column

**Files:**
- Modify: `tui/components/completiontray.go`

**Step 1: Write the failing test**

```go
func TestDescriptionsAlignToCommonColumn(t *testing.T) {
	rows := []completionTrayRow{
		{primary: "/clear", secondary: "aaa"},
		{primary: "/sandbox", secondary: "bbb"},
	}
	got := strings.Split(renderCompletionTray(rows, 0, 60), "\n")
	a := strings.Index(stripANSI(got[0]), "aaa")
	b := strings.Index(stripANSI(got[1]), "bbb")
	if a != b {
		t.Errorf("secondaries start at columns %d and %d, want the same", a, b)
	}
}
```

**Step 2: Run to verify it fails**

Run: `cd tui && go test ./components/ -run TestDescriptionsAlign -v`
Expected: FAIL — columns differ (today's ragged layout).

**Step 3: Compute `descCol` from the widest primary and pad**

Already sketched in Task 2's snippet — add the `descCol` computation at the top of `renderCompletionTrayBackground`.

**Step 4: Run tests, regenerate the golden**

Run: `cd tui && go test -race ./components/ -v`
Expected: `TestDescriptionsAlign` PASS; `TestTrayRenderIsStable` FAIL. **This is the one deliberate pixel change** — regenerate the golden and note why in the commit.

**Step 5: Commit**

```bash
git add tui/components/
git commit -m "feat(components): align tray descriptions to a common column"
```

---

## Tray component design (applies to Tasks 3–8)

Today only three of the four trays share `renderCompletionTray*`. `SessionComplete` hand-rolls `renderSessionLine` (`components/sessioncomplete.go:91`) because it renders **two lines per item** (title, then `lastUsed · shortID` metadata) plus a blank spacer — a shape the one-row shared renderer cannot express.

Rather than keep two renderers, the shared component gains the capabilities the session tray needs, and the bespoke one is deleted:

```go
// trayLayout configures how a tray lays out its rows. The zero value is today's
// one-row-per-item tray, so the three existing callers are unaffected by construction.
type trayLayout struct {
	// Stacked renders Secondary on its OWN row beneath Primary instead of inline in a
	// second column. The session tray needs this; the others do not.
	Stacked bool
	// PadV is blank rail-carrying rows inserted BELOW each item, separating entries in a
	// stacked tray. 0 for the dense trays, 1 for sessions.
	PadV int
	// PadH is columns of padding inside the rail, left and right, so a roomier tray can
	// breathe without every caller hand-padding its strings.
	PadH int
}
```

Per-tray configuration:

| Tray | Stacked | PadV | PadH | Notes |
|---|---|---|---|---|
| slash | false | 0 | 0 | zero value — byte-identical to today |
| files | false | 0 | 0 | zero value |
| runtime (model list) | false | 0 | 0 | zero value |
| sessions | **true** | **1** | 0 | replaces `renderSessionLine` |

**Consequence for testing:** Tasks 5–7 can assert byte-identity against today's output. **Task 8 cannot** — the session tray's output is *supposed* to change, since it moves onto the shared rail, fill and selection band. Its test pins structure (two rows per item, spacer between, selection band spans both rows) rather than exact bytes.

---

## Task 4: The list-backed tray engine

**Files:**
- Create: `tui/components/traylist.go`
- Test: `tui/components/traylist_test.go`

**Step 1: Write the failing test — pixel identity**

```go
// The list-backed tray must render BYTE-IDENTICAL output to the hand-rolled renderer.
// Everything the migration changes must be accounted for by Task 3's alignment, and
// nothing else about the pixels may move.
func TestListTrayMatchesHandRolled(t *testing.T) {
	rows := []completionTrayRow{
		{primary: "/clear", secondary: "start a new conversation"},
		{primary: "/compact", secondary: "compact the current conversation"},
	}
	want := renderCompletionTray(rows, 0, 44)
	tl := newTrayList(rows, 44)
	if got := tl.View(); got != want {
		t.Errorf("list-backed tray differs:\n got %q\nwant %q", got, want)
	}
}

func TestListTrayCursorWraps(t *testing.T) {
	tl := newTrayList([]completionTrayRow{{primary: "a"}, {primary: "b"}}, 20)
	tl.Up()
	if got := tl.Cursor(); got != 1 {
		t.Errorf("Up from 0 = %d, want wrap to 1", got)
	}
	tl.Down()
	if got := tl.Cursor(); got != 0 {
		t.Errorf("Down from last = %d, want wrap to 0", got)
	}
}
```

**Step 2: Run to verify it fails**

Run: `cd tui && go test ./components/ -run TestListTray -v`
Expected: FAIL, `undefined: newTrayList`

**Step 3: Implement the delegate and wrapper**

```go
package components

// trayList is the bubbles/list engine behind every completion panel. It exists so the
// four panels keep their current public API while gaining fuzzy filtering and per-rune
// match highlighting; the delegate calls the SAME renderer the hand-rolled tray uses, so
// adopting it changes no pixels.
type trayList struct{ m list.Model }

type trayItem struct{ primary, secondary string }

// FilterValue is the PRIMARY only, so list.MatchesForItem's rune indices map 1:1 onto
// the column trayDelegate underlines.
func (t trayItem) FilterValue() string { return t.primary }

type trayDelegate struct{}

func (trayDelegate) Height() int                         { return 1 }
func (trayDelegate) Spacing() int                        { return 0 }
func (trayDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d trayDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	// ... rail + primary (+ fuzzy underline via m.MatchesForItem) + aligned secondary,
	// then styles.SelectedRow for the cursor row, FillLineBackgroundWith otherwise.
}

func newTrayList(rows []completionTrayRow, width int) *trayList {
	items := make([]list.Item, len(rows))
	for i, r := range rows {
		items[i] = trayItem{r.primary, r.secondary}
	}
	l := list.New(items, trayDelegate{}, width, len(rows))
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(false)
	l.SetShowHelp(false)
	l.SetShowFilter(false)
	l.DisableQuitKeybindings()
	l.InfiniteScrolling = true // wrap, matching today's modulo cursor
	return &trayList{m: l}
}
```

**Step 4: Run to verify it passes**

Run: `cd tui && go test -race ./components/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add tui/components/traylist.go tui/components/traylist_test.go
git commit -m "feat(components): list-backed tray engine, pixel-identical"
```

---

## Task 5–8: Migrate the four panels onto `trayList`

One task each, in this order — `slashcomplete`, `filecomplete`, `valuecomplete`, `sessioncomplete`. **The existing test files are the contract; they must stay green unmodified.**

**Files (per panel, e.g. Task 5):**
- Modify: `tui/components/slashcomplete.go`
- Unchanged: `tui/components/slashcomplete_test.go` (340 lines — the regression guard)

**Step 1: Run the existing tests, confirm green**

Run: `cd tui && go test -race ./components/ -run TestSlash -v`

**Step 2: Replace the internal state, keep the API**

Swap `items []SlashCmd; cursor int; windowStart int; windowPinned bool` for an embedded `*trayList`. Keep every exported method signature: `Selected()`, `Cursor()`, `Up()`, `Down()`, `SelectWindowRow()`, `View()`, `ViewWidth()`, `ViewWindow()`, `ViewWindowBackground()`.

Keep `NewSlashCompleteWithCommands` returning `nil` on no matches — the nil-means-hidden contract is relied on by `interaction.go`.

**Step 3: Run the existing tests**

Run: `cd tui && go test -race ./components/ -run TestSlash -v`
Expected: PASS, with **zero edits to the test file**. Any needed edit means the API changed — revert and rework.

**Step 4: Commit**

```bash
git commit -am "refactor(components): slashcomplete on the list engine"
```

Repeat for `filecomplete` (Task 6), `valuecomplete` (Task 7), `sessioncomplete` (Task 8).

---

## Task 8b: Move sliding into the engine, delete the per-panel workarounds

**Why this exists.** `list.Model` PAGINATES; the panels SLIDE. `completionTrayWindowStart` pins the selection at the window edge and scrolls a row at a time; `list.Model` jumps a whole page. Measured on 5 items at `maxRows=2`:

| cursor | engine (paged) | hand-rolled (slid) |
|---|---|---|
| 2 | `three, four` | `two, three` |
| 4 | `five` — tray visibly SHRINKS | `four, five` |

**Sliding is the better behaviour** — a short final page makes the tray shrink under the cursor. So sliding wins, and it belongs in ONE place.

Because `trayList` shipped with only relative motion (`Up`/`Down`, no absolute `Select(int)`) and a paginating `ViewWindow`, each panel migration reimplemented sliding independently. Task 6 walks the cursor with counted loops and allocates a throwaway `trayList` per windowed frame; Task 5 renders the full list and slices lines, keeping `windowStart`/`windowPinned` for the hover pin; Task 7 could not reimplement it at all from the panel (paging lives in `trayList.render` via `list.Model.Paginator`, and the throwaway-engine trick would discard the filter state the match underlines depend on).

**The work:**

1. Add to `components/traylist.go`:
   - `Select(index int)` — absolute cursor placement. Its absence is what forced every panel to write a counted walker.
   - `Filter(query string)` and `Len()` — Task 7 currently reaches into `v.list.m` four times (`Filter`, `SetFilterText`, `VisibleItems`, `GlobalIndex`) because the engine does not re-export them. Close that hole rather than leaving four panels poking at the embedded model.
2. **Make `ViewWindow` slide.** Greenfield: do NOT keep pagination alongside it. Nothing should depend on paging, and two window semantics is exactly the drift this migration exists to remove. Delete the paginating path.
3. Delete the per-panel workarounds in all four panels. Whether `windowStart`/`windowPinned` survive is now an engine question: the hover pin (a pointed-at window must not slide out from under the pointer) is real behaviour and must be preserved — implement it **once**, in the engine.
4. Run `make lint` and act on every U1000. The workarounds will orphan helpers.

**Proof of correctness:** all four panels' existing tests stay green untouched. If consolidation is behaviour-neutral they must.

---

## Task 9: Enable fuzzy filtering

**Files:**
- Modify: `tui/components/slashcomplete.go`

**Step 1: Write the failing test**

```go
// The rank ladder is prefix/substring only, so a subsequence query finds nothing.
// list.DefaultFilter is fuzzy and does.
func TestFuzzyFindsSubsequence(t *testing.T) {
	s := NewSlashComplete("sbx")
	if s == nil {
		t.Fatal("fuzzy filter found no match for \"sbx\", want /sandbox")
	}
	if got := s.Selected().Name; got != "/sandbox" {
		t.Errorf("Selected() = %q, want /sandbox", got)
	}
}
```

**Step 2: Run to verify it fails**

Run: `cd tui && go test ./components/ -run TestFuzzy -v`
Expected: FAIL — `NewSlashComplete` returns nil.

**Step 3: Replace `slashMatchRank` with `SetFilterText`**

Delete `slashMatchRank` and `slashRelatedWords`; call `l.SetFilterText(query)` and return nil when `len(VisibleItems()) == 0`.

**Step 4: Run the whole package**

Run: `cd tui && go test -race ./components/ -v`
Expected: PASS. Some ordering assertions in `slashcomplete_test.go` may legitimately change — fuzzy ranks differently from the ladder. **Read each failure before editing a test**; only relax an assertion that was pinning the old ranking specifically.

**Step 5: Commit**

```bash
git commit -am "feat(components): fuzzy command filtering with match highlighting"
```

---

## Task 10: AskUser bracketed accelerators, no cursor glyph

**Files:**
- Modify: `tui/internal/presentation/prompt.go` (`choiceRow`)

**Step 1: Write the failing test**

```go
func TestChoiceRowHasNoCursorGlyph(t *testing.T) {
	got := choiceRow(1, "develop", true, 40)
	if strings.Contains(stripANSI(got), "▸") {
		t.Error("selected choice still uses a cursor glyph")
	}
	if !strings.Contains(stripANSI(got), "[2]") {
		t.Error("accelerator is not bracketed")
	}
}
```

**Step 2: Run to verify it fails**

Run: `cd tui && go test ./internal/presentation/ -run TestChoiceRow -v`
Expected: FAIL on both assertions.

**Step 3: Rewrite `choiceRow`**

```go
func choiceRow(index int, text string, selected bool, width int) string {
	key := "[" + strconv.Itoa(index+1) + "]"
	styled := " " + styles.CardKeyStyle.Render(key) + " " + truncate(text, width-choicePrefixWidth)
	if selected {
		return styles.SelectedRow(styled, width)
	}
	return styled
}
```

**Step 4: Run the package**

Run: `cd tui && go test -race ./internal/presentation/ -v`
Expected: PASS. Update any test asserting the old `"▸ 2. develop"` shape.

**Step 5: Commit**

```bash
git commit -am "feat(presentation): bracketed AskUser accelerators, banded selection"
```

---

## Task 11: Gate — selectable action rows

**Files:**
- Modify: `tui/internal/presentation/prompt.go` (`renderPermissionBox`, `approvalHints`)
- Modify: `tui/internal/presentation/interaction.go` (`permissionKey` — add up/down)

**Step 1: Write the failing test**

```go
func TestPermissionRowsAreSelectable(t *testing.T) {
	p := prompt{ToolName: "Bash", selected: 1}
	got := stripANSI(renderPermissionBox(p, 60, 1))
	for _, want := range []string{"[y] approve", "[n] deny"} {
		if !strings.Contains(got, want) {
			t.Errorf("permission card missing %q", want)
		}
	}
	if strings.Contains(got, "▸") {
		t.Error("permission card uses a cursor glyph")
	}
}
```

**Step 2: Run to verify it fails**

Run: `cd tui && go test ./internal/presentation/ -run TestPermissionRows -v`

**Step 3: Replace the flat footer with banded rows**

One row per `approvalHints` entry: `" " + CardKeyStyle.Render("["+h.key+"]") + " " + h.action`, banded via `styles.SelectedRow` when `i == p.selected`. **No consequence notes** — the labels carry the meaning, and the requirement tree above already lists what an "always" approval persists.

Add `up`/`down` to `permissionKey`, wrapping modulo `len(approvalHints)`. `y`/`a`/`n` keep working as direct accelerators — they must not become selection-only.

**Step 4: Run the package**

Run: `cd tui && go test -race ./internal/presentation/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git commit -am "feat(presentation): selectable approval actions with bracketed keys"
```

---

## Task 12: Form fields on `textarea`

**Files:**
- Modify: `tui/internal/presentation/prompt.go` (`formField`, `formFieldFromSchema`, `encode`, `satisfied`)
- Modify: `tui/internal/presentation/interaction.go` (`editFormField`, `formKey`)

**Step 1: Write the failing test**

```go
// Today's editor cannot move a cursor: it appends runes (typeRune) and backspaces from
// the END. A textarea-backed field can.
func TestFormFieldCursorMoves(t *testing.T) {
	m := newInteractionModel()
	// ... seed a form prompt with one gate.FieldText carrying "abcdef"
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.pending[0].Fields[0].input.Value(); got != "abcdf" {
		t.Errorf("value = %q, want %q (backspace after one left)", got, "abcdf")
	}
}

// The space bar must reach the field. Regression guard for the String() trap above.
func TestFormFieldAcceptsSpace(t *testing.T) {
	m := newInteractionModel()
	// ... seed a form prompt with one empty gate.FieldText
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := m.pending[0].Fields[0].input.Value(); got != " " {
		t.Errorf("value = %q, want a single space", got)
	}
}

// A long value wraps onto extra rows rather than scrolling in a one-row field.
func TestFormFieldGrows(t *testing.T) {
	f := newTextField(40)
	f.input.SetValue(strings.Repeat("wrap me ", 20))
	capFieldHeight(&f)
	if h := lipgloss.Height(f.input.View()); h < 2 {
		t.Errorf("field height = %d, want it to have wrapped", h)
	}
}
```

**Step 2: Run to verify it fails**

Run: `cd tui && go test ./internal/presentation/ -run TestFormField -v`
Expected: FAIL — no `input` field exists.

**Step 3: Give `formField` a `textarea.Model`**

Add `input textarea.Model` to `formField`, populated in `formFieldFromSchema` for `gate.FieldText` only (`FieldSelect`/`FieldConfirm` keep `Choice`/`Confirm` and are untouched). `encode()` and `satisfied()` read `input.Value()` for text fields; delete `Text`, `typeRune` and `backspaceText`.

Configure each field the way `components/input.go` configures the composer:

```go
ta := textarea.New()
ta.Prompt = ""
ta.ShowLineNumbers = false
ta.SetWidth(innerWidth)
// enter must stay free to SUBMIT the form, so newline binds elsewhere — the same
// two-key binding the composer uses (Kitty-capable terminals get shift+enter; every
// terminal gets ctrl+j).
ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "ctrl+j"))
s := ta.Styles()
s.Focused.CursorLine = lipgloss.NewStyle() // clear the default black CursorLine patch
ta.SetStyles(s)
ta.DynamicHeight = true
ta.MinHeight = 1
ta.MaxHeight = contentHeightSecurityLimit // NOT the visible cap — it is an input gate
```

Apply the visible cap after every mutation via `SetHeight(clamp(ta.Height(), 1, maxFormFieldRows))`.

**Signature change required:** `formKey` currently returns `(interactionModel, uiAction)`. `textarea.Focus()` returns a `tea.Cmd` (cursor blink), so widen it to `(interactionModel, uiAction, tea.Cmd)` and thread the cmd out through `Update`, which already returns one.

**Key routing:** `up`/`down` must stay field-focus movement (`moveFocus`) and NOT be swallowed by the textarea — a form is a list of fields first, an editor second. `left`/`right` and the word ops go to the field.

**Step 4: Run the package**

Run: `cd tui && go test -race ./internal/presentation/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git commit -am "feat(presentation): growing textarea fields in form gates"
```

---

## Task 13: Field width and growth regression guard

**Files:**
- Test: `tui/internal/presentation/formgate_test.go`

**Step 1: Write the test**

```go
// A form card row must never exceed the card width, or the terminal wraps it and the
// card's row accounting breaks. A GROWING field makes this sharper than a fixed one:
// height changes per keystroke, so the guard runs across content lengths.
func TestFormRowsNeverExceedCardWidth(t *testing.T) {
	const w = 60
	p := /* form prompt whose text field holds a value longer than w */
	for i, line := range strings.Split(renderFormBox(p, w, 1), "\n") {
		if lw := lipgloss.Width(line); lw > w {
			t.Errorf("row %d width %d > card %d", i, lw, w)
		}
	}
}
```

**Step 2–4: Run, fix any overflow, re-run**

Run: `cd tui && go test -race ./internal/presentation/ -run TestFormRows -v`

**Step 5: Commit**

```bash
git commit -am "test(presentation): pin form card row width across field growth"
```

---

## Task 14: The `?` key panel

**Files:**
- Create: `tui/internal/presentation/keypanel.go`
- Modify: `tui/internal/presentation/screen.go` (layout + key routing)

**Note:** this is additive. `/help` (`sessioncore.go:486` → `CommitSystem(helpText())`) is the **command catalogue** and is unchanged. The panel is the **key legend**, and it is transient chrome — never committed to the transcript.

**Step 1: Write the failing test**

```go
func TestKeyPanelTogglesOnlyWhenComposerEmpty(t *testing.T) {
	m := newScreenForTest()
	m.interaction.input.SetValue("what is ?")
	m, _ = m.handleKey(tea.KeyPressMsg{Code: '?'})
	if m.keyPanel {
		t.Error("? opened the panel while the composer had text; it must be a literal ?")
	}
}
```

**Step 2: Run to verify it fails**

Run: `cd tui && go test ./internal/presentation/ -run TestKeyPanel -v`

**Step 3: Implement**

Define a `keyMap` of `key.Binding`s for the global keys currently handled as raw `msg.String()` switches in `screen.go` (`ctrl+c`, `ctrl+t`, `ctrl+n`, `ctrl+p`, `esc`) plus the composer's `enter`/`shift+enter`. Render with `help.FullHelpView`, styled from `styles.CardKeyStyle`/`CardHintStyle`.

**Layout:** the panel sits below the composer and, like the tray, **takes rows from the transcript**. Add `panelH` to `screenLayout` and subtract it in `contentH` alongside `trayH`.

`?` toggles only when the composer is empty; `esc` closes.

**Step 4: Run the package**

Run: `cd tui && go test -race ./internal/presentation/ -v`

**Step 5: Commit**

```bash
git commit -am "feat(presentation): ? opens a transient key panel below the composer"
```

---

## Task 15: Full verification

**Step 1: Format**

Run: `cd tui && make fmt && make fmt-check`
Expected: clean exit

**Step 2: Race tests**

Run: `cd tui && go test -race ./...`
Expected: all PASS

**Step 3: Standalone against pinned deps**

Run: `cd tui && GOWORK=off go test ./...`
Expected: all PASS — this is what catches a dep that only resolves via the workspace.

**Step 4: Security + vuln**

Run: `cd tui && make secure`
Expected: clean (gofmt + vet + staticcheck + gosec, then `go mod verify` + govulncheck). `sahilm/fuzzy` is new to the graph — govulncheck must be clean on it.

**Step 5: Confirm bubblezone never entered**

Run: `cd tui && GOWORK=off go mod graph | grep -c bubblezone`
Expected: `0`

**Step 6: Commit**

```bash
git commit -am "chore: bubbles adoption verification pass"
```

---

## Out of scope

- **`bubbles/viewport`** — rejected in `2026-07-09-modern-viewport-design.md:108` and still correct: no selection model, no per-line provenance.
- **`bubbles/spinner`** — dropped by the user; `anim.go`'s shared blink tick drives both the status dot and `liveRunningNode`, which a per-instance spinner Model does not fit.
- **`bubbles/table`, `filepicker`, `paginator`, `stopwatch`, `timer`** — assessed and rejected; see the component survey.
- **`bubblezone`** — not adopted. It is a mouse *zone* hit-tester, not a text-cursor library, and `textinput`/`textarea` have no mouse handling at all. Click-to-position-cursor, if wanted later, is ~10 lines: enable `tea.MouseModeCellMotion`, subtract the field origin from the click column, call `SetCursor`.
- **`bubbles/progress`** — revisit when the context-usage indicator from `2026-07-10-context-usage-indicator-design.md` is actually built. It is currently unimplemented.
