package styles

import (
	"reflect"
	"testing"
)

func TestResponsiveMarkdownTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		markdown   string
		width      int
		responsive bool
	}{
		{
			name: "reported module descriptions switch to records",
			markdown: `| Module | Language | What it does |
| --- | --- | --- |
| core | Go | Foundational shared types: the content block/message vocabulary used across every module, plus logging and uuid. |
| storage | Go | Neutral, stdlib-only storage contracts with append-only and compare-and-swap behavior. |`,
			width:      118,
			responsive: true,
		},
		{
			name: "compact table stays a grid",
			markdown: `| Name | State |
| --- | --- |
| core | ready |
| tui | done |`,
			width:      40,
			responsive: false,
		},
		{
			name: "arbitrary header uses content classification",
			markdown: `| X | Y |
| --- | --- |
| one | This prose has enough words to be narrative and exceed its grid allocation. |`,
			width:      36,
			responsive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseResponsiveTables(tt.markdown, tt.width)
			if (len(got) > 0) != tt.responsive {
				t.Fatalf("parseResponsiveTables() returned %d tables, want responsive=%v", len(got), tt.responsive)
			}
		})
	}
}

func TestMarkdownTableExtraction(t *testing.T) {
	t.Parallel()

	const markdown = `Intro

> | **Module** | Language | Notes |
> | --- | --- | --- |
> | ` + "`core`" + ` | Go |

Outro`
	tables := extractMarkdownTables(markdown)
	if len(tables) != 1 {
		t.Fatalf("extractMarkdownTables() returned %d tables, want 1", len(tables))
	}
	table := tables[0]

	wantHeaders := []markdownTableCell{
		{raw: "**Module**", plain: "Module"},
		{raw: "Language", plain: "Language"},
		{raw: "Notes", plain: "Notes"},
	}
	if !reflect.DeepEqual(table.headers, wantHeaders) {
		t.Errorf("headers = %#v, want %#v", table.headers, wantHeaders)
	}
	wantRows := [][]markdownTableCell{{
		{raw: "`core`", plain: "core"},
		{raw: "Go", plain: "Go"},
		{},
	}}
	if !reflect.DeepEqual(table.rows, wantRows) {
		t.Errorf("rows = %#v, want %#v", table.rows, wantRows)
	}
	if table.prefix != "> " {
		t.Errorf("prefix = %q, want %q", table.prefix, "> ")
	}
	wantSource := "> | **Module** | Language | Notes |\n> | --- | --- | --- |\n> | `core` | Go |\n"
	if got := markdown[table.start:table.end]; got != wantSource {
		t.Errorf("replacement source = %q, want %q", got, wantSource)
	}
}

func TestMarkdownTableColumnAllocationStaysWithinWidth(t *testing.T) {
	t.Parallel()

	metrics := []markdownColumnMetrics{
		{maxWidth: 3},
		{maxWidth: 40, narrative: true},
		{maxWidth: 50, narrative: true},
	}
	const available = 9
	allocations := allocateGridColumns(metrics, available)
	total := 0
	for _, allocation := range allocations {
		total += allocation
	}
	if total != available {
		t.Fatalf("allocateGridColumns() = %v totaling %d, want total %d", allocations, total, available)
	}
}
