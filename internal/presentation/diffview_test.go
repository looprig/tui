package presentation

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderDiffTintsByPrefix(t *testing.T) {
	diff := "--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,2 @@\n-var old = \"value\"\n+var new = \"value\"\n ctx\n"

	lines := renderDiff(diff, "a.go", 60)

	if len(lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "old") || !strings.Contains(joined, "new") {
		t.Fatalf("content lost:\n%s", joined)
	}
	for i, line := range lines {
		if width := ansi.StringWidth(line); width > 60 {
			t.Errorf("line %d is %d columns, want <= 60: %q", i, width, line)
		}
	}
	for _, index := range []int{1, 2} {
		line := lines[index]
		if !strings.Contains(line, "\x1b[48;") {
			t.Errorf("changed row %d has no background tint: %q", index, line)
		}
		if !strings.Contains(line, "\x1b[38;2;") {
			t.Errorf("changed row %d lost syntax foreground: %q", index, line)
		}
		segments := strings.Split(line, "\x1b[0m")
		for segment := 1; segment < len(segments)-1; segment++ {
			if !strings.HasPrefix(segments[segment], "\x1b[48;") {
				t.Errorf("changed row %d loses its tint after reset %d: %q", index, segment, line)
			}
		}
	}
}

func TestRenderDiffDropsTheFileHeader(t *testing.T) {
	diff := "--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-a\n+b\n"

	joined := strings.Join(renderDiff(diff, "a.go", 60), "\n")

	if strings.Contains(joined, "--- a/") || strings.Contains(joined, "+++ b/") {
		t.Errorf("file header was not dropped:\n%s", joined)
	}
}

func TestRenderDiffDoesNotMistakeChangedContentForHeaders(t *testing.T) {
	diff := "--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n--- old flag\n+++ new flag\n"

	joined := ansi.Strip(strings.Join(renderDiff(diff, "a.go", 60), "\n"))

	if !strings.Contains(joined, "--- old flag") || !strings.Contains(joined, "+++ new flag") {
		t.Fatalf("changed content that resembles a file header was dropped: %q", joined)
	}
}

func TestRenderDiffEmptyInputYieldsNoLines(t *testing.T) {
	if lines := renderDiff("", "a.go", 60); len(lines) != 0 {
		t.Fatalf("got %d lines for an empty diff", len(lines))
	}
}

func TestRenderDiffUnknownExtensionPreservesContent(t *testing.T) {
	lines := renderDiff("@@ -1 +1 @@\n-opaque old\n+opaque new\n", "data.unknown-extension", 60)

	if got := ansi.Strip(strings.Join(lines, "\n")); got != "@@ -1 +1 @@\n-opaque old\n+opaque new" {
		t.Fatalf("rendered content = %q", got)
	}
}

func TestRenderDiffHunkIsFaintAndContextIsNotTinted(t *testing.T) {
	lines := renderDiff("@@ -1 +1 @@\n context\n", "a.go", 60)

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[0], "\x1b[2m") {
		t.Errorf("hunk header is not faint: %q", lines[0])
	}
	if strings.Contains(lines[1], "\x1b[48;") {
		t.Errorf("context row has a background tint: %q", lines[1])
	}
	if got := ansi.Strip(lines[1]); got != " context" {
		t.Errorf("context row = %q", got)
	}
}

func TestRenderDiffTruncatesHighlightedRowsByDisplayWidth(t *testing.T) {
	diff := "+var message = \"" + strings.Repeat("界", 80) + "\"\n"

	lines := renderDiff(diff, "a.go", 20)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if width := ansi.StringWidth(lines[0]); width > 20 {
		t.Fatalf("rendered width = %d, want <= 20: %q", width, lines[0])
	}
	if !strings.Contains(ansi.Strip(lines[0]), "…") {
		t.Errorf("truncated row has no ellipsis: %q", lines[0])
	}
}

func TestRenderDiffNormalizesCRLFWithoutTrailingBlankRow(t *testing.T) {
	lines := renderDiff("@@ -1 +1 @@\r\n-old\r\n+new\r\n", "a.go", 60)

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	if strings.Contains(ansi.Strip(strings.Join(lines, "\n")), "\r") {
		t.Fatalf("rendered diff retained a carriage return: %#v", lines)
	}
}

func TestRenderDiffMakesUntrustedTerminalControlsVisible(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "SGR", raw: "\x1b[31mred\x1b[0m", want: `\x1b[31mred\x1b[0m`},
		{name: "cursor and erase CSI", raw: "\x1b[2J\x1b[H", want: `\x1b[2J\x1b[H`},
		{name: "OSC BEL", raw: "\x1b]52;c;SGVsbG8=\a", want: `\x1b]52;c;SGVsbG8=\a`},
		{name: "OSC ST", raw: "\x1b]0;title\x1b\\", want: `\x1b]0;title\x1b\\`},
		{name: "lone ESC", raw: "\x1b", want: `\x1b`},
		{name: "lone carriage return", raw: "\r", want: `\r`},
		{name: "tab", raw: "\t", want: `\t`},
		{name: "C0 and DEL", raw: "\x00\a\b\v\f\x1f\x7f", want: `\x00\a\b\v\f\x1f\x7f`},
		{name: "C1", raw: "\u0080\u0085\u0090\u009b\u009d\u009f", want: `\u0080\u0085\u0090\u009b\u009d\u009f`},
		{
			name: "Unicode bidi controls",
			raw:  "\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069",
			want: `\u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069`,
		},
	}
	prefixes := []string{"-", "+", " "}
	for _, tt := range tests {
		for _, prefix := range prefixes {
			t.Run(tt.name+"/prefix="+prefix, func(t *testing.T) {
				lines := renderDiff("@@ -1 +1 @@\n"+prefix+"before "+tt.raw+" after\n", "data.unknown-extension", 300)
				if len(lines) != 2 {
					t.Fatalf("got %d lines, want 2", len(lines))
				}
				row := lines[1]
				if got, want := ansi.Strip(row), prefix+"before "+tt.want+" after"; got != want {
					t.Errorf("visible row = %q, want %q", got, want)
				}
				if tt.raw != "\x1b" && strings.Contains(row, tt.raw) {
					t.Errorf("row retained input terminal controls %q: %q", tt.raw, row)
				}
				if width := ansi.StringWidth(row); width > 300 {
					t.Errorf("row width = %d, want <= 300", width)
				}
				assertOnlyRendererSGR(t, row)
				if prefix != " " {
					if !strings.Contains(row, "\x1b[48;") {
						t.Errorf("changed row lost renderer tint: %q", row)
					}
					if !strings.HasSuffix(row, "\x1b[m") && !strings.HasSuffix(row, "\x1b[0m") {
						t.Errorf("changed row does not reset renderer styling: %q", row)
					}
				} else if strings.ContainsRune(row, '\x1b') {
					t.Errorf("unstyled context row contains an escape byte: %q", row)
				}
			})
		}
	}
}

func TestRenderDiffSanitizesBeforeSyntaxHighlighting(t *testing.T) {
	diff := "+var message = \"\x1b[31mred\x1b[0m\"\n"

	lines := renderDiff(diff, "a.go", 100)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	row := lines[0]
	if got, want := ansi.Strip(row), `+var message = "\x1b[31mred\x1b[0m"`; got != want {
		t.Errorf("visible row = %q, want %q", got, want)
	}
	if !strings.Contains(row, "\x1b[38;2;") || !strings.Contains(row, "\x1b[48;") {
		t.Errorf("syntax foreground or diff background was lost: %q", row)
	}
	assertOnlyRendererSGR(t, row)
}

func TestRenderDiffSanitizesHunkAndDefaultRows(t *testing.T) {
	diff := "notice \x1b]52;c;clipboard\a\n@@ -1 +1 @@ \x1b[2J\n"

	lines := renderDiff(diff, "data.unknown-extension", 80)

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if got, want := ansi.Strip(lines[0]), `notice \x1b]52;c;clipboard\a`; got != want {
		t.Errorf("default row = %q, want %q", got, want)
	}
	if got, want := ansi.Strip(lines[1]), `@@ -1 +1 @@ \x1b[2J`; got != want {
		t.Errorf("hunk row = %q, want %q", got, want)
	}
	for _, line := range lines {
		assertOnlyRendererSGR(t, line)
	}
}

func TestRenderDiffSanitizationPreservesUnicodeAndWidthBound(t *testing.T) {
	diff := "+变量 := \"界🙂\" + \x1b[31m" + strings.Repeat("x", 80) + "\n"

	lines := renderDiff(diff, "data.unknown-extension", 32)

	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if visible := ansi.Strip(lines[0]); !strings.Contains(visible, `+变量 := "界🙂" + \x1b[31m`) {
		t.Errorf("normal Unicode or visible escape was lost: %q", visible)
	}
	if width := ansi.StringWidth(lines[0]); width > 32 {
		t.Fatalf("row width = %d, want <= 32: %q", width, lines[0])
	}
	assertOnlyRendererSGR(t, lines[0])
}

func TestRenderDiffVisibleEncodingDistinguishesControlsFromLiteralEscapes(t *testing.T) {
	tests := []struct {
		name        string
		control     string
		literal     string
		wantControl string
		wantLiteral string
	}{
		{name: "ESC", control: "\x1b", literal: `\x1b`, wantControl: `\x1b`, wantLiteral: `\\x1b`},
		{name: "carriage return", control: "left\rright", literal: `left\rright`, wantControl: `left\rright`, wantLiteral: `left\\rright`},
		{name: "C1 CSI", control: "\u009b", literal: `\u009b`, wantControl: `\u009b`, wantLiteral: `\\u009b`},
		{
			name:        "mixture",
			control:     "left\x1b/mid\r/right\u009b",
			literal:     `left\x1b/mid\r/right\u009b`,
			wantControl: `left\x1b/mid\r/right\u009b`,
			wantLiteral: `left\\x1b/mid\\r/right\\u009b`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control := renderedDiffBody(t, tt.control)
			literal := renderedDiffBody(t, tt.literal)
			if control != tt.wantControl {
				t.Errorf("control visible body = %q, want %q", control, tt.wantControl)
			}
			if literal != tt.wantLiteral {
				t.Errorf("literal visible body = %q, want %q", literal, tt.wantLiteral)
			}
			if control == literal {
				t.Errorf("distinct input collided as %q", control)
			}
		})
	}
}

func TestRenderDiffVisibleEncodingPreservesInvalidBytesDistinctly(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "invalid ff", raw: string([]byte{0xff}), want: `\xff`},
		{name: "invalid fe", raw: string([]byte{0xfe}), want: `\xfe`},
		{name: "malformed sequence", raw: string([]byte{0xe2, '(', 0xa1}), want: `\xe2(\xa1`},
		{name: "truncated sequence", raw: string([]byte{0xf0, 0x9f}), want: `\xf0\x9f`},
		{name: "genuine replacement rune", raw: "\uFFFD", want: "\uFFFD"},
		{name: "literal invalid-byte escape", raw: `\xff`, want: `\\xff`},
		{
			name: "mixed invalid replacement and literal",
			raw:  string([]byte{0xff}) + "\uFFFD" + `\xff`,
			want: `\xff` + "\uFFFD" + `\\xff`,
		},
	}
	seen := make(map[string]string, len(tests))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderedDiffBody(t, tt.raw)
			if got != tt.want {
				t.Errorf("visible body = %q, want %q", got, tt.want)
			}
			if prior, exists := seen[got]; exists {
				t.Errorf("visible body %q collides with %s", got, prior)
			}
			seen[got] = tt.name
		})
	}
}

func renderedDiffBody(t *testing.T, body string) string {
	t.Helper()
	lines := renderDiff(" "+body+"\n", "data.unknown-extension", 300)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	row := lines[0]
	assertOnlyRendererSGR(t, row)
	if width := ansi.StringWidth(row); width > 300 {
		t.Fatalf("row width = %d, want <= 300", width)
	}
	visible := ansi.Strip(row)
	if !strings.HasPrefix(visible, " ") {
		t.Fatalf("context row lost its prefix: %q", visible)
	}
	return visible[1:]
}

// assertOnlyRendererSGR allows the SGR sequences emitted by Chroma/lipgloss but
// rejects every other control byte and every CSI whose final byte could move the
// cursor, erase the screen, or escape the approval surface.
func assertOnlyRendererSGR(t *testing.T, row string) {
	t.Helper()
	for i := 0; i < len(row); {
		r, size := utf8.DecodeRuneInString(row[i:])
		if r != '\x1b' {
			if unicode.IsControl(r) {
				t.Fatalf("row contains non-SGR control %U: %q", r, row)
			}
			i += size
			continue
		}
		if i+2 >= len(row) || row[i+1] != '[' {
			t.Fatalf("row contains non-CSI or truncated ESC at byte %d: %q", i, row)
		}
		end := i + 2
		for end < len(row) && ((row[end] >= '0' && row[end] <= '9') || row[end] == ';' || row[end] == ':') {
			end++
		}
		if end >= len(row) || row[end] != 'm' {
			t.Fatalf("row contains non-SGR CSI at byte %d: %q", i, row)
		}
		i = end + 1
	}
}
