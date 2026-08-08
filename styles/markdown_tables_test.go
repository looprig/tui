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

func TestMarkdownTableNarrowLayout(t *testing.T) {
	t.Parallel()

	const markdown = "| A long label | Notes |\n|---|---|\n| x | a narrative value with enough words to wrap |"
	plain, _ := renderResponsiveTableForTest(t, markdown, 22)
	for _, want := range []string{"A long label", "  x", "Notes", "  a narrative"} {
		if !strings.Contains(plain, want) {
			t.Errorf("stacked records do not contain %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "A long label │ Notes") {
		t.Errorf("narrow table stayed in grid layout:\n%s", plain)
	}
}

func TestMarkdownTableNarrowLongLabelFitsWidth(t *testing.T) {
	t.Parallel()

	const width = 16
	const markdown = "| An extraordinarily long label | N |\n|---|---|\n| x | a narrative value with enough words to wrap |"
	_, out := renderResponsiveTableForTest(t, markdown, width)
	for _, line := range strings.Split(out, "\n") {
		if got := xansi.StringWidth(line); got > width {
			t.Errorf("stacked label line width = %d, want <= %d: %q", got, width, xansi.Strip(line))
		}
	}
}

func TestMarkdownTableRichCells(t *testing.T) {
	t.Parallel()

	const markdown = "| Key | Notes |\n|---|---|\n| `core` | See **shared types** at [docs](https://example.com/docs) for the complete description. |"
	plain, out := renderResponsiveTableForTest(t, markdown, 42)
	for _, want := range []string{"core", "shared types", "docs"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rich records do not contain %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(out, "38;2;162;210;255") {
		t.Errorf("inline code color missing from %q", out)
	}
	if !strings.Contains(out, ";1m") {
		t.Errorf("bold styling missing from %q", out)
	}
	if !strings.Contains(out, "\x1b]8;") {
		t.Errorf("OSC hyperlink missing from %q", out)
	}
}

func TestMarkdownTableNonemptyCellFallsBackWithoutContentLoss(t *testing.T) {
	t.Parallel()

	const width = 36
	const markdown = `| Key | Notes |
|---|---|
| core | This narrative value has enough words to require responsive records. |
| broken | <!A |`
	r, err := NewMarkdownRenderer(width)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer(%d): %v", width, err)
	}
	direct, err := r.Render(markdown)
	if err != nil {
		t.Fatalf("direct Render(): %v", err)
	}
	if !strings.Contains(xansi.Strip(direct), "<!A") {
		t.Fatalf("test precondition failed: direct Glamour output lost the cell: %q", direct)
	}
	got, err := RenderMarkdown(r, markdown, width)
	if err != nil {
		t.Fatalf("RenderMarkdown(): %v", err)
	}
	if got != direct {
		t.Fatalf("RenderMarkdown() did not fall back after a nonempty cell rendered empty:\ngot  %q\nwant %q", got, direct)
	}
}

func TestMarkdownTableRichHeadersPreserveInlineRendering(t *testing.T) {
	t.Parallel()

	const markdown = "| [Key](https://example.com/key) | `Notes` **Details** |\n|---|---|\n| core | This narrative value has enough words to require responsive records. |"
	plain, out := renderResponsiveTableForTest(t, markdown, 44)
	for _, want := range []string{"Key", "Notes", "Details", "core"} {
		if !strings.Contains(plain, want) {
			t.Errorf("rich header output does not contain %q:\n%s", want, plain)
		}
	}
	if !strings.Contains(out, "\x1b]8;") || !strings.Contains(out, "https://example.com/key") {
		t.Errorf("header hyperlink/OSC sequence missing from %q", out)
	}
	if !strings.Contains(out, "\x1b]8;;\a") {
		t.Errorf("header hyperlink closing OSC sequence missing from %q", out)
	}
	if !strings.Contains(out, "38;2;162;210;255") {
		t.Errorf("header inline-code color missing from %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if got := xansi.StringWidth(line); got > 44 {
			t.Errorf("rich-header line width = %d, want <= 44: %q", got, xansi.Strip(line))
		}
	}
}

func TestMarkdownTableUnicodeWidth(t *testing.T) {
	t.Parallel()

	const width = 32
	const markdown = "| 名前 | 説明 |\n|---|---|\n| 核心 | これは十分な単語を含む長い説明 text with several words |"
	plain, out := renderResponsiveTableForTest(t, markdown, width)
	for _, want := range []string{"名前", "核心", "説明"} {
		if !strings.Contains(plain, want) {
			t.Errorf("Unicode records do not contain %q:\n%s", want, plain)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if got := xansi.StringWidth(line); got > width {
			t.Errorf("Unicode line width = %d, want <= %d: %q", got, width, xansi.Strip(line))
		}
	}
}

func TestMarkdownTableEmptyUnevenCells(t *testing.T) {
	t.Parallel()

	const markdown = "| Key | State | Notes |\n|---|---|---|\n| core | ready | This narrative has enough words to require records. |\n| tui |"
	plain, _ := renderResponsiveTableForTest(t, markdown, 30)
	for _, want := range []string{"Key", "core", "State", "ready", "Notes", "tui"} {
		if !strings.Contains(plain, want) {
			t.Errorf("uneven records do not contain %q:\n%s", want, plain)
		}
	}
	if count := strings.Count(plain, "State"); count != 2 {
		t.Errorf("State label occurs %d times, want 2:\n%s", count, plain)
	}
	if count := strings.Count(plain, "Notes"); count != 2 {
		t.Errorf("Notes label occurs %d times, want 2:\n%s", count, plain)
	}
}

func TestMarkdownTableBlockquotePrefix(t *testing.T) {
	t.Parallel()

	const markdown = `> | Key | Notes |
> |---|---|
> | core | This narrative value has enough words to require a responsive record. |
> | tui | Another narrative value has enough words to require a responsive record. |`
	plain, _ := renderResponsiveTableForTest(t, markdown, 44)
	for _, line := range strings.Split(plain, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "│ ") {
			t.Errorf("blockquoted record line lost rendered container prefix: %q", line)
		}
	}
}

func TestMarkdownTableListNestedFallsBackWithoutPrefixDuplication(t *testing.T) {
	t.Parallel()

	const width = 44
	const markdown = `- intro

  | A | Notes |
  |---|---|
  | one | This narrative has enough words to require responsive records. |`
	r, err := NewMarkdownRenderer(width)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer(%d): %v", width, err)
	}
	direct, err := r.Render(markdown)
	if err != nil {
		t.Fatalf("direct Render(): %v", err)
	}
	got, err := RenderMarkdown(r, markdown, width)
	if err != nil {
		t.Fatalf("RenderMarkdown(): %v", err)
	}
	plain := xansi.Strip(got)
	if count := strings.Count(plain, "intro"); count != 1 {
		t.Errorf("intro occurs %d times, want once:\n%s", count, plain)
	}
	for _, want := range []string{"A", "Notes", "one", "This narrative"} {
		if !strings.Contains(plain, want) {
			t.Errorf("list-nested table lost %q:\n%s", want, plain)
		}
	}
	if got != direct {
		t.Errorf("list-nested responsive table did not safely fall back to direct Glamour output")
	}
}

func TestStructuralRenderedTablePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{name: "empty", prefix: "", want: true},
		{name: "whitespace", prefix: "   ", want: true},
		{name: "blockquote rail", prefix: "\x1b[2m│\x1b[m ", want: true},
		{name: "nested blockquote rails", prefix: "│ │ ", want: true},
		{name: "list item content", prefix: "• intro ", want: false},
		{name: "ordinary text", prefix: "intro ", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isStructuralRenderedTablePrefix(tt.prefix); got != tt.want {
				t.Errorf("isStructuralRenderedTablePrefix(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

func TestMarkdownTableResize(t *testing.T) {
	t.Parallel()

	const markdown = "| Key | Notes |\n|---|---|\n| core | This narrative value contains enough words that it wraps in a narrow grid. |"
	widePlain, wideOut := renderResponsiveTableForTest(t, markdown, 140)
	narrowPlain, _ := renderResponsiveTableForTest(t, markdown, 38)
	if !strings.Contains(widePlain, "Key") || !strings.Contains(widePlain, "│") {
		t.Errorf("wide table did not remain a grid:\n%s", widePlain)
	}
	r, err := NewMarkdownRenderer(140)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer(140): %v", err)
	}
	direct, err := r.Render(markdown)
	if err != nil {
		t.Fatalf("direct Render(): %v", err)
	}
	if wideOut != direct {
		t.Errorf("wide grid changed from direct Glamour output")
	}
	if strings.Contains(narrowPlain, "Key │ Notes") || !strings.Contains(narrowPlain, " Key") {
		t.Errorf("narrow table did not switch to records:\n%s", narrowPlain)
	}
}

func renderResponsiveTableForTest(t *testing.T, markdown string, width int) (string, string) {
	t.Helper()
	r, err := NewMarkdownRenderer(width)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer(%d): %v", width, err)
	}
	out, err := RenderMarkdown(r, markdown, width)
	if err != nil {
		t.Fatalf("RenderMarkdown(width=%d): %v", width, err)
	}
	return xansi.Strip(out), out
}
