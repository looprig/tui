package components

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestValueCompleteReturnsOpaqueIDAndFiltersAliases(t *testing.T) {
	tray := NewValueComplete([]ValueItem{
		{ID: "base", Label: "Default", Aliases: []string{"normal"}},
		{ID: "review", Label: "Review", Description: "read carefully"},
	}, "normal")
	if tray == nil || tray.Selected().ID != "base" {
		t.Fatalf("selected = %#v, want opaque base id", tray)
	}
	if NewValueComplete([]ValueItem{{ID: "x", Label: "Alpha"}}, "missing") != nil {
		t.Fatal("zero-result query returned a visible tray")
	}
}

func TestValueCompleteNavigationWrapsAndViewClamps(t *testing.T) {
	tray := NewValueComplete([]ValueItem{{ID: "a", Label: "Alpha"}, {ID: "b", Label: "Beta"}}, "")
	tray.Up()
	if tray.Selected().ID != "b" {
		t.Fatalf("Up() selected %q, want b", tray.Selected().ID)
	}
	tray.Down()
	if tray.Selected().ID != "a" {
		t.Fatalf("Down() selected %q, want a", tray.Selected().ID)
	}
	for _, line := range strings.Split(tray.ViewWindow(8, 2), "\n") {
		if ansi.StringWidth(line) > 8 {
			t.Fatalf("line exceeds width: %q", line)
		}
	}
	if !tray.SelectWindowRow(1, 2) || tray.Selected().ID != "b" {
		t.Fatalf("mouse row selection = %q, want b", tray.Selected().ID)
	}
}

func TestModelCompleteGroupsProvidersWithoutSelectingThem(t *testing.T) {
	t.Parallel()

	tray := NewModelComplete([]ValueItem{
		{ID: "gpt-5.4", Provider: "OpenAI", Label: "GPT-5.4", Description: "coding and reasoning", Aliases: []string{"gpt"}},
		{ID: "claude-sonnet-4.5", Provider: "Anthropic", Label: "Claude Sonnet 4.5", Description: "balanced everyday work", Aliases: []string{"sonnet"}},
	})
	if tray == nil {
		t.Fatal("NewModelComplete() = nil, want two choices")
	}
	if got := tray.Selected().ID; got != "gpt-5.4" {
		t.Fatalf("initial selection = %q, want first model rather than OPENAI", got)
	}

	lines := strings.Split(tray.ViewWindow(100, 10), "\n")
	plain := make([]string, len(lines))
	for i, line := range lines {
		plain[i] = ansi.Strip(line)
	}
	if !strings.Contains(plain[1], "MODELS") || !strings.Contains(plain[2], "2 models") {
		t.Fatalf("model header = %q, want a top spacer then bold MODELS with a muted count", plain[:3])
	}
	if !strings.Contains(plain[4], "OPENAI") || !strings.Contains(plain[7], "ANTHROPIC") {
		t.Fatalf("provider headings = %q, want OPENAI then ANTHROPIC", plain)
	}
	if !strings.Contains(plain[5], "GPT-5.4") || !strings.Contains(plain[5], "coding and reasoning") {
		t.Errorf("OpenAI model row = %q, want the compact name and description", plain[5])
	}
	// The gap between one provider's last model and the next heading. It is a rail-carrying
	// row, not an empty string: the panel's left edge runs unbroken through the gap.
	if body := strings.TrimSpace(strings.TrimPrefix(plain[6], "▌")); body != "" {
		t.Errorf("row between the groups = %q, want a blank rail row separating them", plain[6])
	}

	if tray.SelectWindowRow(4, 10) {
		t.Fatal("provider heading selection reported a move, want headings inert")
	}
	if tray.SelectWindowRow(6, 10) {
		t.Fatal("group spacer selection reported a move, want spacers inert")
	}
	tray.Down()
	if got := tray.Selected().ID; got != "claude-sonnet-4.5" {
		t.Fatalf("Down() selected %q, want the next model rather than ANTHROPIC", got)
	}

	tray.Filter("openai")
	if got := tray.Selected().ID; got != "gpt-5.4" {
		t.Errorf("provider filter selected %q, want gpt-5.4", got)
	}
	tray.Filter("sonnet")
	if got := tray.Selected().ID; got != "claude-sonnet-4.5" {
		t.Errorf("alias filter selected %q, want claude-sonnet-4.5", got)
	}
}

// TestModelCompleteGroupGapsSurviveFiltering pins where the group gap goes once a filter has
// dropped rows: it stays BETWEEN two surviving groups, and it is dropped above whichever
// group ends up first, where it would only hang a blank row off the tray header.
func TestModelCompleteGroupGapsSurviveFiltering(t *testing.T) {
	t.Parallel()

	tray := NewModelComplete([]ValueItem{
		// No descriptions: this test is about which ROWS the projection contains, so the
		// rows are compared whole rather than searched for a substring.
		{ID: "gpt-5.4", Provider: "OpenAI", Label: "GPT-5.4"},
		{ID: "claude-sonnet-4.5", Provider: "Anthropic", Label: "Claude Sonnet 4.5"},
		{ID: "claude-opus-5", Provider: "Anthropic", Label: "Claude Opus 5"},
	})
	if tray == nil {
		t.Fatal("NewModelComplete() = nil, want three choices")
	}

	body := func() []string {
		lines := strings.Split(tray.ViewWindow(100, 12), "\n")[trayHeaderHeight:]
		plain := make([]string, 0, len(lines))
		for _, line := range lines {
			plain = append(plain, strings.TrimSpace(strings.TrimPrefix(ansi.Strip(line), "▌")))
		}
		return plain
	}

	// "claude" matches both Anthropic models and nothing in the OpenAI group, so the
	// surviving group is now first: its leading gap goes with the group above it.
	tray.Filter("claude")
	if got, want := body(), []string{"ANTHROPIC", "Claude Sonnet 4.5", "Claude Opus 5"}; !slices.Equal(got, want) {
		t.Errorf("filtered tray = %q, want no leading gap above the first group %q", got, want)
	}

	// Both groups survive "5", so the gap between them is back.
	tray.Filter("5")
	if got, want := body(), []string{"OPENAI", "GPT-5.4", "", "ANTHROPIC", "Claude Sonnet 4.5", "Claude Opus 5"}; !slices.Equal(got, want) {
		t.Errorf("filtered tray = %q, want a gap between the two groups %q", got, want)
	}

	// Clearing the filter restores the unfiltered projection, gap included.
	tray.Filter("")
	if got, want := body(), []string{"OPENAI", "GPT-5.4", "", "ANTHROPIC", "Claude Sonnet 4.5", "Claude Opus 5"}; !slices.Equal(got, want) {
		t.Errorf("unfiltered tray = %q, want %q", got, want)
	}
}

// valueModels is the tray's real subject: a model list whose names are long, hyphenated and
// near-identical, which is precisely why nobody types them in full.
var valueModels = []ValueItem{
	{ID: "opus-5", Label: "claude-opus-5", Description: "deepest reasoning", Aliases: []string{"opus"}},
	{ID: "sonnet-4-5", Label: "claude-sonnet-4-5", Description: "balanced", Aliases: []string{"sonnet"}},
	{ID: "haiku-4-5", Label: "claude-haiku-4-5", Description: "cheap and quick", Aliases: []string{"fast"}},
}

// TestValueCompleteMatchesSubsequences pins the fuzzy matcher. "sn45" is not a substring of
// anything -- the matcher it replaced found nothing for it -- but it is how someone reaches
// for claude-sonnet-4-5 without typing seventeen characters, and it must land on that one
// model rather than on the whole catalog.
func TestValueCompleteMatchesSubsequences(t *testing.T) {
	t.Parallel()

	tray := NewValueComplete(valueModels, "sn45")
	if tray == nil {
		t.Fatal("sn45 matched nothing, want claude-sonnet-4-5")
	}
	if got := tray.Len(); got != 1 {
		t.Fatalf("sn45 matched %d choices, want just claude-sonnet-4-5", got)
	}
	if got := tray.Selected().ID; got != "sonnet-4-5" {
		t.Errorf("sn45 selected %q, want the sonnet-4-5 payload", got)
	}

	// The description is no longer searched, and that is deliberate: fuzzy is a
	// subsequence test, so matching prose would put most of the catalog behind any short
	// query with nothing underlined to explain why. See valueFilter.
	if NewValueComplete(valueModels, "deepest reasoning") != nil {
		t.Error("a term found only in a description matched, want the description ignored")
	}
}

// TestValueCompleteMatchesAliasesWithoutUnderlining pins both halves of the alias decision:
// an alias still finds its model, and an alias match contributes NO rune indices, so it can
// never underline a rune of the label that the user did not type.
//
// Two models match here so the assertion lands on an UNSELECTED row: styles.SelectedRow
// strips inner styling on its light fill, so the selected row never carries an underline and
// asserting there would test the band rather than the match.
func TestValueCompleteMatchesAliasesWithoutUnderlining(t *testing.T) {
	t.Parallel()

	// Neither label contains an f, so every match below can only have come from an alias.
	tray := NewValueComplete([]ValueItem{
		{ID: "haiku-4-5", Label: "claude-haiku-4-5", Aliases: []string{"fast"}},
		{ID: "flash", Label: "gemini-2-5-lite", Aliases: []string{"fastest"}},
	}, "fast")
	if tray == nil {
		t.Fatal("an alias term matched nothing, want both aliased models")
	}
	if got := tray.Len(); got != 2 {
		t.Fatalf("alias term matched %d choices, want 2", got)
	}
	if got := tray.Selected().ID; got != "haiku-4-5" {
		t.Errorf("selected = %q, want the first aliased model's payload", got)
	}

	lines := strings.Split(tray.ViewWindow(60, 2), "\n")
	if len(lines) != 2 {
		t.Fatalf("tray drew %d rows, want 2:\n%q", len(lines), lines)
	}
	// Composed through the tray's own style rather than spelled as a raw escape, so the
	// assertion cannot drift from what lipgloss emits.
	underline, _, _ := strings.Cut(trayMatchStyle.Render("x"), "x")
	if underline == "" {
		t.Fatal("trayMatchStyle no longer emits an opening sequence; the check below is vacuous")
	}
	if row := lines[1]; strings.Contains(row, underline) {
		t.Errorf("an alias-only match underlined runes of the label: %q", row)
	}
}

// TestValueCompleteCopiesAliases pins the defensive copy. The tray outlives the call that
// built it, and the aliases arrive on a slice the catalog still owns, so a refresh rewriting
// that slice must not change what a tray the user is already typing into matches or returns.
func TestValueCompleteCopiesAliases(t *testing.T) {
	t.Parallel()

	aliases := []string{"fast"}
	tray := NewValueComplete([]ValueItem{{ID: "haiku-4-5", Label: "claude-haiku-4-5", Aliases: aliases}}, "")
	if tray == nil {
		t.Fatal("an unfiltered tray of one choice is nil, want the choice")
	}
	aliases[0] = "slow"
	if got := tray.Selected().Aliases; len(got) != 1 || got[0] != "fast" {
		t.Errorf("aliases = %q, want [fast]: the tray is sharing the caller's slice", got)
	}
}
