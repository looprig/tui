package styles

import (
	"reflect"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
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
		{
			name: "header-only table stays a grid",
			markdown: `| A very long header | Another long header |
| --- | --- |`,
			width:      12,
			responsive: false,
		},
		{
			name: "intrinsic width equal to available stays a grid",
			markdown: `| A | B |
| --- | --- |
| one | two |`,
			width:      13,
			responsive: false,
		},
		{
			name: "exactly four words classify narrative",
			markdown: `| X | Y |
| --- | --- |
| one | alpha bravo charlie delta |`,
			width:      24,
			responsive: true,
		},
		{
			name: "exactly 28 columns classify narrative",
			markdown: `| X | Y |
| --- | --- |
| one | abcdefghijklmnopqrstuvwxyz12 |`,
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
	want := []int{3, 3, 3}
	if !reflect.DeepEqual(allocations, want) {
		t.Fatalf("allocateGridColumns() = %v, want %v", allocations, want)
	}
	total := 0
	for _, allocation := range allocations {
		total += allocation
	}
	if total != available {
		t.Fatalf("allocateGridColumns() = %v totaling %d, want total %d", allocations, total, available)
	}
}

func TestMarkdownTableRendersResponsiveRecords(t *testing.T) {
	t.Parallel()

	const width = 118
	const markdown = `| Module | Language | What it does |
| --- | --- | --- |
| core | Go | Foundational shared types: the content block/message vocabulary used across every module, plus logging and uuid. No deps. Everyone imports this; it imports nothing. |
| storage | Go | Neutral, stdlib-only storage contracts — four primitives: Ledger, Leaser, KV, and Blobs. Zero third-party deps. |
| fsstore | Go | Storage primitives implemented over the local filesystem. Single-host durable backend. |
| natsstore | Go | Storage primitives over NATS JetStream — embedded in-process or remote broker. The only NATS-dependent module. |`
	r, err := NewMarkdownRenderer(width)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer(%d): %v", width, err)
	}
	out, err := RenderMarkdown(r, markdown, width)
	if err != nil {
		t.Fatalf("RenderMarkdown(): %v", err)
	}
	plain := xansi.Strip(out)

	wantFragments := []string{
		"Module        core",
		"Language      Go",
		"What it does  Foundational shared types:",
		"              and uuid.",
		"Module        storage",
		"              party deps.",
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(plain, fragment) {
			t.Errorf("rendered records do not contain %q:\n%s", fragment, plain)
		}
	}
	if strings.Contains(plain, "Module │ Language") {
		t.Errorf("rendered records still contain grid header:\n%s", plain)
	}
	if count := strings.Count(plain, "Module        "); count != 4 {
		t.Errorf("Module label occurs %d times, want 4:\n%s", count, plain)
	}

	bodyValues := []string{
		"core", "Go", "Foundational shared types:",
		"storage", "Go", "Neutral, stdlib-only storage contracts",
		"fsstore", "Go", "Storage primitives implemented over the local filesystem.",
		"natsstore", "Go", "Storage primitives over NATS JetStream",
	}
	position := 0
	for _, value := range bodyValues {
		index := strings.Index(plain[position:], value)
		if index < 0 {
			t.Fatalf("body value %q missing or out of source order:\n%s", value, plain)
		}
		position += index + len(value)
	}

	separator := strings.Repeat("─", width)
	if count := strings.Count(plain, separator); count != 3 {
		t.Errorf("full-width separator occurs %d times, want 3:\n%s", count, plain)
	}
	for _, line := range strings.Split(out, "\n") {
		if lineWidth := xansi.StringWidth(line); lineWidth > width {
			t.Errorf("rendered line width = %d, want <= %d: %q", lineWidth, width, xansi.Strip(line))
		}
	}
}
