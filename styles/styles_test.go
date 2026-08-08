package styles

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// TestToolStyles verifies the tool-call and tool-result styles render their input
// to non-empty output (mirrors the role-style expectations). Faint styling may add
// ANSI escapes; the text content must always survive.
func TestToolStyles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		style interface{ Render(...string) string }
		in    string
	}{
		{name: "tool call style renders text", style: ToolCallStyle, in: "└ ReadFile  config.yaml  ✓"},
		{name: "tool result style renders text", style: ToolResultStyle, in: "    port: 8080"},
		{name: "tool call style empty input", style: ToolCallStyle, in: ""},
		{name: "tool result style empty input", style: ToolResultStyle, in: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := tt.style.Render(tt.in)
			if tt.in == "" {
				return // empty input may render empty; just must not panic.
			}
			if !strings.Contains(out, tt.in) {
				t.Errorf("Render(%q) = %q, want to contain the input text", tt.in, out)
			}
		})
	}
}

func TestNewMarkdownRenderer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		width int
	}{
		{name: "standard width", width: 80},
		{name: "narrow width", width: 20},
		{name: "wide width", width: 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := NewMarkdownRenderer(tt.width)
			if err != nil {
				t.Fatalf("NewMarkdownRenderer(%d) error = %v, want nil", tt.width, err)
			}
			if r == nil {
				t.Fatalf("NewMarkdownRenderer(%d) returned nil renderer", tt.width)
			}

			out, err := r.Render("# hi")
			if err != nil {
				t.Fatalf("Render() error = %v, want nil", err)
			}
			if strings.TrimSpace(out) == "" {
				t.Errorf("Render(\"# hi\") = empty, want non-empty output")
			}
		})
	}
}

// TestMarkdownTableSeparatesWrappedBodyRows verifies that terminal-width wrapping
// stays visually grouped inside its logical Markdown row. The first description
// deliberately wraps; a horizontal separator must follow its continuation before
// the next source row begins.
func TestMarkdownTableSeparatesWrappedBodyRows(t *testing.T) {
	t.Parallel()

	r, err := NewMarkdownRenderer(52)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
	}
	out, err := RenderMarkdown(r, `| Harness | Description |
| --- | --- |
| looprig | In-process Harness loop using a configured gateway/client |
| claude-code | Claude Code ACP harness |
| codex | Codex ACP harness |`)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}

	plain := markdownSGR.ReplaceAllString(out, "")
	lines := strings.Split(plain, "\n")
	continuation := lineContaining(lines, "gateway/client")
	middleRow := lineContaining(lines, "claude-code")
	nextRow := lineContaining(lines, "codex")
	if continuation < 0 || middleRow < 0 || nextRow < 0 || continuation >= middleRow || middleRow >= nextRow {
		t.Fatalf("rendered table lines = %#v, want wrapped looprig row before claude-code and codex rows", lines)
	}
	if !linesContainHorizontalRule(lines[continuation+1 : middleRow]) {
		t.Errorf("rendered table lines = %#v, want a separator after wrapped looprig row", lines)
	}
	if !linesContainHorizontalRule(lines[middleRow+1 : nextRow]) {
		t.Errorf("rendered table lines = %#v, want a separator between claude-code and codex rows", lines)
	}
}

func lineContaining(lines []string, substring string) int {
	for i, line := range lines {
		if strings.Contains(line, substring) {
			return i
		}
	}
	return -1
}

func linesContainHorizontalRule(lines []string) bool {
	return horizontalRuleLine(lines) != ""
}

func horizontalRuleLine(lines []string) string {
	for _, line := range lines {
		if strings.Contains(line, "─") {
			return line
		}
	}
	return ""
}

func TestMarkdownRendererLeavesNonTableMarkdownUnchanged(t *testing.T) {
	t.Parallel()

	r, err := NewMarkdownRenderer(52)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
	}
	const markdown = "## Heading\n\n- alpha\n- bravo\n\nA normal paragraph."
	got, err := RenderMarkdown(r, markdown)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	want, err := r.Render(markdown)
	if err != nil {
		t.Fatalf("underlying Render() error = %v, want nil", err)
	}
	if got != want {
		t.Errorf("non-table rendering changed:\ngot  %q\nwant %q", got, want)
	}
}

func TestMarkdownRendererLeavesTableShapedCodeFenceUnchanged(t *testing.T) {
	t.Parallel()

	r, err := NewMarkdownRenderer(52)
	if err != nil {
		t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
	}
	const markdown = "```markdown\n| Harness | Description |\n| --- | --- |\n| looprig | first |\n| codex | second |\n```"
	got, err := RenderMarkdown(r, markdown)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	want, err := r.Render(markdown)
	if err != nil {
		t.Fatalf("underlying Render() error = %v, want nil", err)
	}
	if got != want {
		t.Errorf("table-shaped code fence changed:\ngot  %q\nwant %q", got, want)
	}
}

func TestMarkdownTableSeparatorsFollowGoldmarkRowBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		markdown        string
		before          string
		after           string
		final           string
		separatorPrefix string
	}{
		{
			name: "pipe-less body row",
			markdown: `| A | B |
| --- | --- |
| first | value |
second
| third | value |`,
			before: "first",
			after:  "second",
			final:  "third",
		},
		{
			name: "table nested in blockquote",
			markdown: `> | A | B |
> | --- | --- |
> | first | value |
> | second | value |`,
			before:          "first",
			after:           "second",
			separatorPrefix: "│ ",
		},
		{
			name: "table nested in definition description",
			markdown: `Term
:   | A | B |
    | --- | --- |
    | first | value |
    | second | value |`,
			before: "first",
			after:  "second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r, err := NewMarkdownRenderer(52)
			if err != nil {
				t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
			}
			out, err := RenderMarkdown(r, tt.markdown)
			if err != nil {
				t.Fatalf("RenderMarkdown() error = %v, want nil", err)
			}
			lines := strings.Split(markdownSGR.ReplaceAllString(out, ""), "\n")
			before := lineContaining(lines, tt.before)
			after := lineContaining(lines, tt.after)
			if before < 0 || after <= before {
				t.Fatalf("rendered table lines = %#v, want %q before %q", lines, tt.before, tt.after)
			}
			separator := horizontalRuleLine(lines[before+1 : after])
			if separator == "" {
				t.Fatalf("rendered table lines = %#v, want a separator between %q and %q", lines, tt.before, tt.after)
			}
			if tt.separatorPrefix != "" && !strings.HasPrefix(separator, tt.separatorPrefix) {
				t.Fatalf("separator = %q, want container prefix %q preserved", separator, tt.separatorPrefix)
			}
			if tt.final != "" {
				final := lineContaining(lines, tt.final)
				if final <= after || !linesContainHorizontalRule(lines[after+1:final]) {
					t.Fatalf("rendered table lines = %#v, want a separator between %q and %q", lines, tt.after, tt.final)
				}
			}
		})
	}
}

// TestNewMarkdownRendererPalette verifies the Nexus color overrides applied over
// glamour's DarkStyleConfig: markdown headings render in MarkdownHeadingColor and
// inline `code` spans in MarkdownInlineCodeColor — both #A2D2FF — instead of
// glamour's heading blue (ANSI 256 "39") and inline-code red (ANSI 256 "203"). The
// inline `code` span also drops glamour's background fill (ANSI 256 "236") and its
// U+00A0 prefix/suffix padding. glamour emits color via x/ansi (no terminal
// color-profile downgrade), so the rendered output carries the literal truecolor SGR
// escape regardless of TTY.
func TestNewMarkdownRendererPalette(t *testing.T) {
	t.Parallel()

	const (
		brandBlueSGR = "38;2;162;210;255" // #A2D2FF foreground
		glamourBlue  = "38;5;39"          // old heading color
		glamourRed   = "38;5;203"         // old inline-code color
		codeBgSGR    = "48;5;236"         // old inline-code background fill
		nbsp         = "\u00a0"           // old inline-code prefix/suffix padding
	)

	tests := []struct {
		name    string
		md      string
		wantSGR string   // truecolor escape that MUST appear
		absent  []string // substrings that must NOT appear
	}{
		{name: "heading uses brand blue, no hash marker", md: "## Section", wantSGR: brandBlueSGR, absent: []string{glamourBlue, "#"}},
		{
			name:    "inline code: brand blue, no background, no nbsp padding",
			md:      "run `go test` now",
			wantSGR: brandBlueSGR,
			absent:  []string{glamourRed, codeBgSGR, nbsp},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := NewMarkdownRenderer(80)
			if err != nil {
				t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
			}
			out, err := r.Render(tt.md)
			if err != nil {
				t.Fatalf("Render(%q) error = %v, want nil", tt.md, err)
			}
			if !strings.Contains(out, tt.wantSGR) {
				t.Errorf("Render(%q) = %q, want it to contain SGR %q", tt.md, out, tt.wantSGR)
			}
			for _, a := range tt.absent {
				if strings.Contains(out, a) {
					t.Errorf("Render(%q) = %q, must NOT contain %q", tt.md, out, a)
				}
			}
		})
	}
}

// TestNewMarkdownRendererHeadingNoHashes verifies the H2–H6 prefix override: every
// heading level renders its text WITHOUT glamour's literal "#" markers, while the
// heading text itself survives. H1 is covered separately (it uses a background bar,
// never a "#" marker).
func TestNewMarkdownRendererHeadingNoHashes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		md   string
		text string // the heading text that MUST still appear
	}{
		{name: "h2", md: "## Alpha", text: "Alpha"},
		{name: "h3", md: "### Bravo", text: "Bravo"},
		{name: "h4", md: "#### Charlie", text: "Charlie"},
		{name: "h5", md: "##### Delta", text: "Delta"},
		{name: "h6", md: "###### Echo", text: "Echo"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := NewMarkdownRenderer(80)
			if err != nil {
				t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
			}
			out, err := r.Render(tt.md)
			if err != nil {
				t.Fatalf("Render(%q) error = %v, want nil", tt.md, err)
			}
			if !strings.Contains(out, tt.text) {
				t.Errorf("Render(%q) = %q, want it to contain the heading text %q", tt.md, out, tt.text)
			}
			if strings.Contains(out, "#") {
				t.Errorf("Render(%q) = %q, must NOT contain a '#' hash marker", tt.md, out)
			}
		})
	}
}

// markdownSGR matches CSI ... m (SGR) escape sequences in glamour output.
var markdownSGR = regexp.MustCompile("\x1b\\[([0-9;]*)m")

// sgrParamIsRed reports whether a single SGR param list sets a RED foreground.
// It parses tokens (split on ';') so 256-color/truecolor introducers are matched as
// whole color tokens — NOT naive substrings (which would falsely match "38;5;1"
// inside "38;5;187"). Red = basic 31/91, the reddish 256-color palette glamour's
// code-block chroma theme emits (Operator #EF8080 → "210", GenericDeleted #FD5B5B →
// "203", plus other true reds), or a truecolor with a high-R, low-G/B foreground.
func sgrParamIsRed(params string) bool {
	red256 := map[int]bool{
		1: true, 9: true, 196: true, 203: true, 210: true,
		160: true, 161: true, 167: true, 197: true, 204: true,
	}
	atoi := func(s string) int {
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				return -1
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	toks := strings.Split(params, ";")
	for i := 0; i < len(toks); i++ {
		if toks[i] == "31" || toks[i] == "91" {
			return true
		}
		if toks[i] == "38" && i+2 < len(toks) && toks[i+1] == "5" {
			if red256[atoi(toks[i+2])] {
				return true
			}
			i += 2
			continue
		}
		if toks[i] == "38" && i+4 < len(toks) && toks[i+1] == "2" {
			r, g, b := atoi(toks[i+2]), atoi(toks[i+3]), atoi(toks[i+4])
			if r >= 180 && g >= 0 && g <= 150 && b >= 0 && b <= 150 {
				return true
			}
			i += 4
			continue
		}
	}
	return false
}

// renderHasRedRun reports whether any non-whitespace text run in the rendered output
// carries a red SGR foreground (the leading SGR escape before the run). Walks the SGR
// escapes and inspects the text between each escape and the next.
func renderHasRedRun(out string) (string, bool) {
	idxs := markdownSGR.FindAllStringSubmatchIndex(out, -1)
	for i, m := range idxs {
		params := out[m[2]:m[3]]
		if !sgrParamIsRed(params) {
			continue
		}
		runStart := m[1]
		runEnd := len(out)
		if i+1 < len(idxs) {
			runEnd = idxs[i+1][0]
		}
		run := out[runStart:runEnd]
		if strings.TrimSpace(run) != "" {
			return run, true
		}
	}
	return "", false
}

// TestNewMarkdownRendererCodeBlockNoRedSymbols is the regression guard for the TUI
// bug where structural symbols in CODE BLOCKS rendered RED. Root cause: glamour's
// DarkStyleConfig code-block chroma theme colors the Operator token ("/", "+", "-",
// "=", "->", …) salmon-red (#EF8080 → ANSI 256 "210") and the GenericDeleted token
// (diff "-" lines) red (#FD5B5B → "203"). NewMarkdownRenderer now retones both to a
// neutral gray. This asserts NO red SGR run survives for representative code blocks
// exercising those symbols; before the fix the Operator/GenericDeleted runs were red.
func TestNewMarkdownRendererCodeBlockNoRedSymbols(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		md   string
	}{
		{
			name: "go operators (slash, plus, minus, arrow)",
			md:   "```go\nx := a / b + c - d\ny <- ch\n```\n",
		},
		{
			name: "diff add/remove markers",
			md:   "```diff\n- removed cmd/coderig/ line\n+ added line\n```\n",
		},
		{
			name: "sql arithmetic operators",
			md:   "```sql\nSELECT a/b, c+d FROM t WHERE x = 1;\n```\n",
		},
		{
			name: "python operators",
			md:   "```python\nx = a / b + c - d\n```\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := NewMarkdownRenderer(80)
			if err != nil {
				t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
			}
			out, err := r.Render(tt.md)
			if err != nil {
				t.Fatalf("Render(%q) error = %v, want nil", tt.md, err)
			}
			if run, red := renderHasRedRun(out); red {
				t.Errorf("Render(%q): a code-block run rendered RED: %q\nfull=%q", tt.md, run, out)
			}
		})
	}
}

// sgrParamIsRedBackground reports whether a single SGR param list sets a RED
// BACKGROUND. It is the background analogue of sgrParamIsRed: it parses tokens
// (split on ';') so 256-color/truecolor introducers are matched as whole color
// tokens, never naive substrings. Red background = basic 41/101, the reddish
// 256-color palette glamour's chroma Error token emits (#F05B5B → "203"), or a
// truecolor with high-R, low-G/B. Foreground introducers (38;...) are skipped.
func sgrParamIsRedBackground(params string) bool {
	red256 := map[int]bool{
		1: true, 9: true, 196: true, 203: true, 210: true,
		160: true, 161: true, 167: true, 197: true, 204: true,
	}
	atoi := func(s string) int {
		n := 0
		for _, c := range s {
			if c < '0' || c > '9' {
				return -1
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	toks := strings.Split(params, ";")
	for i := 0; i < len(toks); i++ {
		// Skip foreground color introducers so their args aren't misread as bg.
		if toks[i] == "38" && i+1 < len(toks) && toks[i+1] == "5" {
			i += 2
			continue
		}
		if toks[i] == "38" && i+1 < len(toks) && toks[i+1] == "2" {
			i += 4
			continue
		}
		if toks[i] == "41" || toks[i] == "101" {
			return true
		}
		if toks[i] == "48" && i+2 < len(toks) && toks[i+1] == "5" {
			if red256[atoi(toks[i+2])] {
				return true
			}
			i += 2
			continue
		}
		if toks[i] == "48" && i+4 < len(toks) && toks[i+1] == "2" {
			r, g, b := atoi(toks[i+2]), atoi(toks[i+3]), atoi(toks[i+4])
			if r >= 180 && g >= 0 && g <= 150 && b >= 0 && b <= 150 {
				return true
			}
			i += 4
			continue
		}
	}
	return false
}

// renderHasRedBackgroundRun reports whether any non-whitespace text run in the
// rendered output carries a red SGR background (the leading SGR escape before the
// run). Mirrors renderHasRedRun but for background fills.
func renderHasRedBackgroundRun(out string) (string, bool) {
	idxs := markdownSGR.FindAllStringSubmatchIndex(out, -1)
	for i, m := range idxs {
		params := out[m[2]:m[3]]
		if !sgrParamIsRedBackground(params) {
			continue
		}
		runStart := m[1]
		runEnd := len(out)
		if i+1 < len(idxs) {
			runEnd = idxs[i+1][0]
		}
		run := out[runStart:runEnd]
		if strings.TrimSpace(run) != "" {
			return run, true
		}
	}
	return "", false
}

// TestNewMarkdownRendererCodeBlockNoRedBackground is the regression guard for the
// TUI bug where box-drawing tree characters (├ └ │ ─) rendered on a RED BACKGROUND.
// Root cause: chroma's strict lexers (e.g. Go) cannot tokenize box-drawing glyphs,
// so they emit them as the Error token, and glamour's DarkStyleConfig styles that
// token with background_color #F05B5B (red, → ANSI 256 "48;5;203"). The earlier
// fix only retoned foreground tokens (Operator, GenericDeleted) and never touched
// Chroma.Error, so the red BACKGROUND survived. NewMarkdownRenderer now clears the
// Error token's red background. This asserts NO red-background run survives for
// code blocks containing untokenizable glyphs across several fence languages; before
// the fix the ├──/└── runs carried "48;5;203".
func TestNewMarkdownRendererCodeBlockNoRedBackground(t *testing.T) {
	t.Parallel()

	tree := ".\n├── foo\n│   └── bar.go\n└── baz\n"
	tests := []struct {
		name string
		md   string
	}{
		{name: "go fence tree", md: "```go\n" + tree + "```\n"},
		{name: "rust fence tree", md: "```rust\n" + tree + "```\n"},
		{name: "python fence tree", md: "```python\n" + tree + "```\n"},
		{name: "json fence tree", md: "```json\n" + tree + "```\n"},
		{name: "no-language fence tree", md: "```\n" + tree + "```\n"},
		{name: "go operators still neutral", md: "```go\nx := a / b + c - d\n```\n"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, err := NewMarkdownRenderer(80)
			if err != nil {
				t.Fatalf("NewMarkdownRenderer error = %v, want nil", err)
			}
			out, err := r.Render(tt.md)
			if err != nil {
				t.Fatalf("Render(%q) error = %v, want nil", tt.md, err)
			}
			if run, red := renderHasRedBackgroundRun(out); red {
				t.Errorf("Render(%q): a code-block run rendered on a RED BACKGROUND: %q\nfull=%q", tt.md, run, out)
			}
		})
	}
}

// TestFillLineBackgroundWith pins the load-bearing re-open contract of the modern gray
// fill: the background must be RE-OPENED immediately after EVERY inner SGR reset (both the
// long "\x1b[0m" glamour emits and the short "\x1b[m" lipgloss emits), the visible content
// must be padded out to the requested width, the line must end with a single terminating
// reset (no stranded background), and an empty open must return the line unchanged
// (fail-safe). A plain lipgloss Background() wrap would tint only up to the first inner
// reset — this test distinguishes that broken behavior from the correct fill.
func TestFillLineBackgroundWith(t *testing.T) {
	t.Parallel()

	const open = "\x1b[48;5;236m" // a synthetic background-open SGR
	const reset = "\x1b[m"
	// A line carrying BOTH inner reset forms; visible text "abcdef" is 6 columns.
	const line = "ab" + "\x1b[0m" + "cd" + "\x1b[m" + "ef"

	tests := []struct {
		name          string
		line          string
		width         int
		open          string
		reset         string
		wantUnchanged bool
		wantWidth     int // expected display width of the result (when not unchanged)
	}{
		{name: "both reset forms, padded to width", line: line, width: 12, open: open, reset: reset, wantWidth: 12},
		{name: "width at content adds no pad", line: line, width: 6, open: open, reset: reset, wantWidth: 6},
		{name: "width below content adds no pad", line: line, width: 3, open: open, reset: reset, wantWidth: 6},
		{name: "empty open returns unchanged (fail-safe)", line: line, width: 12, open: "", reset: reset, wantUnchanged: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FillLineBackgroundWith(tt.line, tt.width, tt.open, tt.reset)

			if tt.wantUnchanged {
				if got != tt.line {
					t.Fatalf("empty-open fill = %q, want the line unchanged %q", got, tt.line)
				}
				return
			}

			// (a) the open is re-opened immediately after EACH inner reset form.
			if !strings.Contains(got, "\x1b[0m"+tt.open) {
				t.Errorf("fill did not re-open the background after the long reset \\x1b[0m; got %q", got)
			}
			if !strings.Contains(got, "\x1b[m"+tt.open) {
				t.Errorf("fill did not re-open the background after the short reset \\x1b[m; got %q", got)
			}
			// The fill opens at the very start too.
			if !strings.HasPrefix(got, tt.open) {
				t.Errorf("fill does not open with the background SGR; got %q", got)
			}
			// (b) visible content is padded out to the requested width.
			if w := lipgloss.Width(got); w != tt.wantWidth {
				t.Errorf("fill display width = %d, want %d; got %q", w, tt.wantWidth, got)
			}
			// (c) ends with a single terminating reset — no stranded background trailing it.
			if !strings.HasSuffix(got, tt.reset) {
				t.Errorf("fill does not end with the terminating reset %q; got %q", tt.reset, got)
			}
			if strings.HasSuffix(got, tt.open) {
				t.Errorf("fill ends with a dangling open (stranded background); got %q", got)
			}
			// The plain visible text survives intact behind the fill.
			if plain := stripANSI(got); !strings.Contains(plain, "abcdef") {
				t.Errorf("fill lost the visible text; plain=%q", plain)
			}
		})
	}
}

// TestFillLineBackgroundModernPanel pins the ModernPanelBg convenience: FillLineBackground
// derives its open from ModernPanelBg (DeriveBackgroundSGR) and re-opens it after an inner
// reset, exactly like FillLineBackgroundWith. It guards that the shared gray user rows and
// composer are filled with the same, correctly-re-opened background.
func TestFillLineBackgroundModernPanel(t *testing.T) {
	t.Parallel()

	open, _ := DeriveBackgroundSGR(PanelBg)
	if open == "" {
		t.Fatal("DeriveBackgroundSGR(ModernPanelBg) returned an empty open")
	}
	line := "x" + "\x1b[0m" + "y"
	got := FillLineBackground(line, 10)
	if !strings.HasPrefix(got, open) {
		t.Errorf("FillLineBackground does not open with the ModernPanelBg SGR; got %q", got)
	}
	if !strings.Contains(got, "\x1b[0m"+open) {
		t.Errorf("FillLineBackground did not re-open the ModernPanelBg fill after the inner reset; got %q", got)
	}
	if w := lipgloss.Width(got); w != 10 {
		t.Errorf("FillLineBackground display width = %d, want 10; got %q", w, got)
	}
}

// stripANSI removes SGR escape sequences so a test can assert on visible glyphs.
func stripANSI(s string) string {
	return regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(s, "")
}

// TestRailNodeGlyphs verifies each unified-rail node style renders the expected glyph
// and is exactly 2 columns wide (matching LitDot) so the timeline stays aligned.
func TestRailNodeGlyphs(t *testing.T) {
	tests := []struct {
		name  string
		got   string
		glyph string
	}{
		{"lit ai node", LitDot, "●"},
		{"tool ok node", ToolNode(NodeOK), "○"},
		{"tool failed node", ToolNode(NodeFailed), "○"},
		{"tool running node", ToolNode(NodeRunning), "◍"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(tt.got, tt.glyph) {
				t.Errorf("style = %q, want glyph %q", tt.got, tt.glyph)
			}
			if lipgloss.Width(tt.got) != 2 {
				t.Errorf("width = %d, want 2", lipgloss.Width(tt.got))
			}
		})
	}
}

// TestRailStyleIsQuieterThanThinking pins the timeline rail to its own dark-neutral
// foreground. Reasoning text remains merely faint, while the structural rail recedes
// further and node glyphs keep their independent status colors.
func TestRailStyleIsQuieterThanThinking(t *testing.T) {
	t.Parallel()

	want := lipgloss.NewStyle().Foreground(lipgloss.Color(RailColor)).Render("│")
	if got := RailStyle.Render("│"); got != want {
		t.Errorf("RailStyle = %q, want dedicated subtle foreground %q", got, want)
	}
	if got := RailStyle.Render("│"); got == ThinkingStyle.Render("│") {
		t.Errorf("RailStyle = ThinkingStyle = %q, want the rail quieter than reasoning text", got)
	}
}

// TestSuccessfulToolNodeUsesBrandLime pins the completed-tool affordance: success keeps
// the quiet hollow-circle shape, but uses the same lime foreground as the filled assistant
// node. Failure remains independently red through the NodeFailed branch.
func TestSuccessfulToolNodeUsesBrandLime(t *testing.T) {
	t.Parallel()

	want := lipgloss.NewStyle().Foreground(DotColor).Render(dotNodeHollow) + " "
	if got := ToolNode(NodeOK); got != want {
		t.Errorf("ToolNode(NodeOK) = %q, want lime hollow node %q", got, want)
	}
}
