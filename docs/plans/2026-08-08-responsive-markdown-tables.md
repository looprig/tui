# Responsive Markdown Tables Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Render description-heavy Markdown tables as responsive Codex-style key/value records while preserving compact grids and all cell content.

**Architecture:** Keep `styles.RenderMarkdown` as the TUI-owned seam around Glamour. Parse real GFM tables with the existing Goldmark dependency, classify and measure their cells at the active render width, replace only unreadable tables with collision-free placeholders, render the surrounding document through Glamour once, and substitute ANSI-aware record layouts for those placeholders. Compact tables and every non-table block continue through Glamour unchanged; any responsive-rendering failure falls back to rendering the original Markdown.

**Tech Stack:** Go, Goldmark GFM AST, Glamour v2, Lip Gloss v2, `github.com/charmbracelet/x/ansi`, standard `strings`/`bytes` packages.

---

### Task 1: Extract a structured table model and select responsive tables

**Files:**
- Create: `styles/markdown_tables.go`
- Create: `styles/markdown_tables_test.go`
- Modify: `styles/styles.go:428-544`

**Step 1: Write failing model and selection tests**

Add focused table-driven tests in `styles/markdown_tables_test.go`:

```go
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
```

Also assert that the extracted first table has the exact headers and body values, including an empty padded cell in an uneven row.

**Step 2: Run the focused test and verify RED**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test -race ./styles -run 'TestResponsiveMarkdownTables|TestMarkdownTableExtraction' -count=1
```

Expected: build failure because `parseResponsiveTables` and its model do not exist.

**Step 3: Implement the table model and content-based classification**

Move the Goldmark-specific table code out of `styles/styles.go` into `styles/markdown_tables.go`. Define cohesive private types:

```go
const (
	narrativeMinimumWords = 4
	narrativeMinimumWidth = 28
	minimumGridColumnWidth = 3
)

type markdownTableCell struct {
	raw   string
	plain string
}

type markdownTable struct {
	start   int
	end     int
	prefix  string
	headers []markdownTableCell
	rows    [][]markdownTableCell
}

type markdownColumnMetrics struct {
	maxWidth int
	words    int
	nonEmpty int
	narrative bool
}
```

Parse with the same extensions Glamour uses:

```go
parser := goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
).Parser()
document := parser.Parse(text.NewReader(source))
```

For each `*tableast.Table`, read the `*tableast.TableHeader` and body `*tableast.TableRow` children. Read raw inline Markdown from the cell's first `Lines()` segment and plain text from `cell.Text(source)`. Normalize every body row to `len(table.Alignments)` by truncating excess cells and padding missing cells with empty values.

Derive the source replacement range from the first header line start through the final body row's line ending. Preserve the source prefix between the physical line start and the header node position so blockquotes and definition-description indentation remain attached to the placeholder.

Classify a column as narrative when its non-empty body cells average at least four whitespace-delimited words or 28 display columns. Use `xansi.StringWidth` for all widths.

Select record mode only when the intrinsic grid is wider than the available width and either:

- a narrative column's intrinsic width exceeds its computed allocation; or
- the available width cannot retain `minimumGridColumnWidth` for every column after cell padding and separators.

Allocate compact columns up to their natural widths first, reserve a 16-column soft floor for each narrative column, and split remaining width among narrative columns. This is intentionally more eager than Codex: the first narrative wrap selects record mode.

Delete the marker-boundary types and helpers from `styles/styles.go` only after their replacement tests are green.

**Step 4: Run the focused tests and verify GREEN**

Run the command from Step 2.

Expected: PASS under the race detector.

**Step 5: Commit the model**

Run `make secure` as required by `AGENTS.md`, then:

```bash
git add styles/markdown_tables.go styles/markdown_tables_test.go styles/styles.go
git commit -m "refactor(tui): model responsive markdown tables"
```

### Task 2: Render selected tables as aligned key/value records

**Files:**
- Modify: `styles/markdown_tables.go`
- Modify: `styles/markdown_tables_test.go`
- Modify: `styles/styles.go:428-440`
- Modify: `internal/presentation/render.go:69-76,493-500`

**Step 1: Write the failing reported-width rendering test**

Add a regression test using the user's complete `Module / Language / What it does` sample at width 118. Strip SGR for layout assertions and require:

```go
wantFragments := []string{
	"Module        core",
	"Language      Go",
	"What it does  Foundational shared types:",
	"              block/message vocabulary",
	"Module        storage",
	"Zero third-party deps.",
}
```

Assert that:

- every original cell value occurs in output in source-row order;
- the grid header line (`Module │ Language`) is absent;
- each record repeats all three labels;
- exactly one full-width `─` rule occurs between each pair of records; and
- no rendered line exceeds width 118.

Call the revised API explicitly with the active width:

```go
out, err := RenderMarkdown(r, markdown, 118)
```

**Step 2: Run the regression test and verify RED**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test -race ./styles -run TestMarkdownTableRendersResponsiveRecords -count=1
```

Expected: compile failure until `RenderMarkdown` accepts the width, then an assertion failure showing Glamour's grid output.

**Step 3: Implement placeholder replacement and record rendering**

Change the adapter signature to make width an explicit input rather than hidden mutable state:

```go
func RenderMarkdown(r *glamour.TermRenderer, markdown string, width int) (string, error)
```

Update both presentation call sites to pass the exact width already supplied to `NewMarkdownRenderer`:

```go
renderWidth := max(0, width-dotWidth)
r, err := styles.NewMarkdownRenderer(renderWidth)
out, err := styles.RenderMarkdown(r, md, renderWidth)
```

The adapter should:

1. Parse selected responsive tables.
2. Replace each complete table source range, from last to first, with a collision-free private-use marker on a line carrying the original container prefix.
3. Render the marked document once with `r.Render`.
4. Render each table's cells independently through `r.Render(cell.raw)` so inline Markdown becomes ANSI before layout.
5. Find the corresponding rendered marker line, retain the ANSI prefix before the marker, and substitute the record lines.
6. If any marker or cell cannot be rendered, discard the partial result and return `r.Render(markdown)`.

Record layout constants and helpers belong in `styles/markdown_tables.go`:

```go
const (
	recordLeadingPadding = 1
	recordFieldGap = 2
	recordMinimumValueWidth = 24
	recordStackedIndent = 2
)
```

For aligned records, compute the widest plain header. Wrap each ANSI-rendered value with `xansi.Wrap` to `width - leadingPadding - labelWidth - fieldGap`. Prefix the first line with a bold label and later lines with spaces of exactly the same display width. For narrow records, put the bold label on its own line and indent every wrapped value by two columns. Separate records with `strings.Repeat("─", width)` and do not append a rule after the final record.

Use a small helper to trim only Glamour's outer blank lines from individually rendered cells; do not strip ANSI or meaningful spaces within the cell.

**Step 4: Run the regression and presentation tests**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test -race ./styles ./internal/presentation -run 'TestMarkdownTableRendersResponsiveRecords|TestRenderMD' -count=1
```

Expected: PASS, with the reported table in record mode and both assistant/user paths compiling with the explicit width.

**Step 5: Commit the responsive renderer**

Run `make secure`, then:

```bash
git add styles/markdown_tables.go styles/markdown_tables_test.go styles/styles.go internal/presentation/render.go
git commit -m "fix(tui): render descriptive tables as records"
```

### Task 3: Preserve compact grids, rich cells, nesting, and narrow layouts

**Files:**
- Modify: `styles/markdown_tables_test.go`
- Modify: `styles/markdown_tables.go`
- Modify: `styles/styles_test.go`

**Step 1: Add failing boundary and preservation tests**

Add table-driven cases for:

```go
tests := []struct {
	name     string
	markdown string
	width    int
	want     []string
}{
	{
		name: "narrow width stacks labels above values",
		markdown: "| A long label | Notes |\n|---|---|\n| x | a narrative value with enough words to wrap |",
		width: 22,
		want: []string{"A long label", "  x", "Notes", "  a narrative"},
	},
	{
		name: "rich inline values survive",
		markdown: "| Key | Notes |\n|---|---|\n| `core` | See **shared types** at [docs](https://example.com/docs) for the complete description. |",
		width: 42,
		want: []string{"core", "shared types", "docs"},
	},
	{
		name: "unicode uses terminal display width",
		markdown: "| 名前 | 説明 |\n|---|---|\n| 核心 | これは十分な単語を含む長い説明 text with several words |",
		width: 32,
		want: []string{"名前", "核心", "説明"},
	},
}
```

Check for the relevant ANSI sequences around inline code, bold text, and links rather than only checking stripped text. Add separate tests that blockquoted responsive table lines retain the rendered `│ ` prefix and that fenced table-shaped code is byte-for-byte equal to direct Glamour output.

Add a resize-selection test that renders the same table at a width where it fits intrinsically and at a width where its narrative cell wraps; assert grid mode for the former and records for the latter.

**Step 2: Run the new tests and verify RED**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test -race ./styles -run 'TestMarkdownTable(Narrow|Rich|Unicode|Blockquote|Resize)|TestMarkdownRendererLeavesTableShapedCodeFenceUnchanged' -count=1
```

Expected: at least the narrow layout and rich/nested preservation cases fail before their edge handling exists.

**Step 3: Complete ANSI-aware and container-aware layout**

Use `xansi.StringWidth`, `xansi.Wrap`, and `xansi.Cut` consistently so ANSI/OSC control sequences and wide Unicode runes do not consume layout columns. When replacing a rendered marker line, derive the visible container prefix from the rendered output rather than recreating it; prepend that exact prefix to every record line and subtract its display width from the separator/value budget.

Do not recursively invoke `RenderMarkdown` for cells. Use `r.Render` so a cell containing table-looking text cannot start a nested responsive pass. Normalize empty cell output to one empty line. Preserve hyperlink/control spans through wrapping.

Keep the fast path exact:

```go
tables := parseResponsiveTables(markdown, width)
if len(tables) == 0 {
	return r.Render(markdown)
}
```

This guarantees compact tables, fenced code, and non-table Markdown remain identical to direct Glamour rendering.

**Step 4: Run all styles and presentation tests**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test -race ./styles ./internal/presentation -count=1
```

Expected: PASS.

**Step 5: Commit edge-case support**

Run `make secure`, then:

```bash
git add styles/markdown_tables.go styles/markdown_tables_test.go styles/styles_test.go
git commit -m "test(tui): cover responsive markdown table layouts"
```

### Task 4: Replace the marker workaround and strengthen fuzz coverage

**Files:**
- Modify: `styles/styles.go`
- Modify: `styles/styles_test.go`
- Modify: `styles/markdown_table_fuzz_test.go`
- Modify: `docs/plans/2026-08-08-markdown-table-row-separators-design.md`

**Step 1: Rewrite the fuzz target for the responsive adapter**

Replace `FuzzMarkTableBodyBoundaries` with a public-seam fuzz target:

```go
func FuzzRenderMarkdownTables(f *testing.F) {
	f.Add("| A | Notes |\n|---|---|\n| one | a sufficiently long narrative value with several words |", 40)
	f.Add("```markdown\n| A | B |\n|---|---|\n| one | two |\n```", 30)
	f.Add("ordinary prose\n\n- with a list", 80)

	f.Fuzz(func(t *testing.T, markdown string, width int) {
		if width < 1 || width > 240 {
			t.Skip()
		}
		r, err := NewMarkdownRenderer(width)
		if err != nil {
			t.Fatalf("NewMarkdownRenderer(%d): %v", width, err)
		}
		out, err := RenderMarkdown(r, markdown, width)
		if err != nil {
			t.Fatalf("RenderMarkdown(): %v", err)
		}
		if strings.TrimSpace(markdown) != "" && strings.TrimSpace(out) == "" {
			t.Fatalf("non-empty Markdown rendered empty")
		}
	})
}
```

**Step 2: Run the seed corpus and verify RED if obsolete helpers remain referenced**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test -race ./styles -run '^$' -fuzz '^FuzzRenderMarkdownTables$' -fuzztime=1x
```

Expected: build failure until old marker-specific tests/helpers are removed or updated, then PASS for the seed corpus.

**Step 3: Remove the old workaround completely**

Delete `markTableBodyBoundaries`, private-use marker row generation, rendered marker-row replacement, and separator reconstruction from `styles/styles.go`. Delete tests that assert separators inside Glamour grids; replace them with responsive-record assertions or compact-grid identity assertions. Update the old design note to state that commit `765ba06` was superseded by the responsive record renderer because row separators did not solve detached wrapped descriptions.

Retain only generic test helpers still used elsewhere. Confirm no old symbols remain:

```bash
rg -n 'markTableBodyBoundaries|markerTableRow|replaceMarkedTableRows|tableRowSeparator|unusedTableMarker' styles
```

Expected: no matches.

**Step 4: Run the styles suite and a 30-second fuzz campaign**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test -race ./styles -count=1
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache go test ./styles -run '^$' -fuzz '^FuzzRenderMarkdownTables$' -fuzztime=30s
```

Expected: PASS with no panic or empty rendering failure.

**Step 5: Commit cleanup and fuzz coverage**

Run `make secure`, then:

```bash
git add styles/styles.go styles/styles_test.go styles/markdown_table_fuzz_test.go docs/plans/2026-08-08-markdown-table-row-separators-design.md
git commit -m "refactor(tui): replace table row marker workaround"
```

### Task 5: Verify the complete TUI change

**Files:**
- Verify only; modify files only if a check exposes a defect, using a new RED/GREEN cycle.

**Step 1: Format and inspect the diff**

Run:

```bash
make fmt
git diff --check
git status --short
```

Expected: formatting completes, `git diff --check` emits nothing, and status lists only intended files.

**Step 2: Run the complete race-enabled suite**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache make test
```

Expected: all packages PASS with `go test -race ./...`.

**Step 3: Run security checks**

Run:

```bash
GOWORK=off GOCACHE=/private/tmp/looprig-responsive-tables-cache STATICCHECK_CACHE=/private/tmp/looprig-responsive-tables-staticcheck make secure
```

Expected: gofmt, vet, staticcheck, and gosec pass; module verification succeeds; `govulncheck` reports no vulnerabilities.

**Step 4: Build the production shape**

Run:

```bash
GOWORK=off CGO_ENABLED=0 GOCACHE=/private/tmp/looprig-responsive-tables-cache go build -trimpath ./...
```

Expected: successful build with no output.

**Step 5: Review the final patch and commit any verification-only corrections**

Run:

```bash
git diff main...HEAD --stat
git diff main...HEAD --check
git log --oneline main..HEAD
```

If verification required a correction, run `make secure` again and commit only that correction. Otherwise leave the branch clean for code review and integration.

