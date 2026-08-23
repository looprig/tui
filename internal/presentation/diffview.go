package presentation

import (
	"bytes"
	"image/color"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
	"github.com/looprig/tui/styles"
)

// renderDiff colorizes an already-rendered unified diff. It deliberately does no
// diffing of its own: the tool is responsible for the mutation description and the
// TUI only turns its stable line prefixes into a compact presentation.
func renderDiff(diff, path string, width int) []string {
	if diff == "" || width <= 0 {
		return nil
	}

	lexer := lexerForDiffPath(path)
	rawLines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(rawLines))
	seenHunk := false
	for i, line := range rawLines {
		if i == len(rawLines)-1 && line == "" {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			seenHunk = true
		}
		if !seenHunk && (strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ")) {
			continue
		}
		line = visibleDiffText(line)

		var rendered string
		switch {
		case strings.HasPrefix(line, "@@"):
			rendered = styles.StatusStyle.Render(line)
		case strings.HasPrefix(line, "-"):
			rendered = tintDiffRow("-"+highlightDiffBody(line[1:], lexer), styles.DiffDeletionBackgroundColor)
		case strings.HasPrefix(line, "+"):
			rendered = tintDiffRow("+"+highlightDiffBody(line[1:], lexer), styles.DiffAdditionBackgroundColor)
		case strings.HasPrefix(line, " "):
			rendered = " " + highlightDiffBody(line[1:], lexer)
		default:
			rendered = line
		}
		lines = append(lines, truncate(rendered, width))
	}
	return lines
}

// visibleDiffText is the trust boundary between file content and terminal markup.
// Unified-diff newlines have already been split structurally; every remaining terminal
// control becomes a printable Go-style escape before Chroma or lipgloss can add trusted
// ANSI. Unicode's narrow Bidi_Control set is escaped too so source text cannot reorder
// the approval display; ordinary Unicode (including joining, combining, and emoji code
// points) remains intact. Each invalid UTF-8 byte gets its own \xNN escape, and literal
// backslashes are doubled, making the visible encoding injective over input bytes after
// the intentional CRLF/newline framing step.
func visibleDiffText(text string) string {
	var visible strings.Builder
	visible.Grow(len(text))
	for len(text) > 0 {
		r, size := utf8.DecodeRuneInString(text)
		switch {
		case r == utf8.RuneError && size == 1:
			const hex = "0123456789abcdef"
			b := text[0]
			visible.WriteString(`\x`)
			visible.WriteByte(hex[b>>4])
			visible.WriteByte(hex[b&0x0f])
		case r == '\\':
			// Backslash introduces every visible escape emitted by this function.
			// Doubling an input backslash keeps the encoding injective: literal
			// source text "\\x1b" cannot collide with an encoded ESC byte.
			visible.WriteString(`\\`)
		case unicode.IsControl(r) || isBidiControl(r):
			quoted := strconv.QuoteRuneToASCII(r)
			visible.WriteString(quoted[1 : len(quoted)-1])
		default:
			visible.WriteString(text[:size])
		}
		text = text[size:]
	}
	return visible.String()
}

// isBidiControl is the Unicode Bidi_Control property. It is intentionally narrower
// than the full Cf category, which also contains useful source characters such as the
// zero-width joiner.
func isBidiControl(r rune) bool {
	switch r {
	case '\u061c', '\u200e', '\u200f',
		'\u202a', '\u202b', '\u202c', '\u202d', '\u202e',
		'\u2066', '\u2067', '\u2068', '\u2069':
		return true
	default:
		return false
	}
}

func lexerForDiffPath(path string) chroma.Lexer {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil || lexer.Config() == nil || lexer.Config().Name == "fallback" {
		return nil
	}
	return chroma.Coalesce(lexer)
}

// highlightDiffBody fails back to the exact input whenever Chroma cannot safely
// tokenize and format it. The caller styles the prefix independently, so the lexer
// never sees the unified-diff marker as source code.
func highlightDiffBody(body string, lexer chroma.Lexer) (highlighted string) {
	if lexer == nil || body == "" {
		return body
	}
	defer func() {
		if recover() != nil {
			highlighted = body
		}
	}()

	iterator, err := lexer.Tokenise(nil, body)
	if err != nil {
		return body
	}
	style := chromastyles.Get("github-dark")
	if style == nil {
		return body
	}
	var output bytes.Buffer
	if err := formatters.TTY16m.Format(&output, style, iterator); err != nil {
		return body
	}
	highlighted = output.String()
	if ansi.Strip(highlighted) != body {
		return body
	}
	return highlighted
}

// tintDiffRow restores the row background after Chroma's per-token resets. Without
// this, only the prefix and first token would retain the deletion/addition tint.
func tintDiffRow(row string, background color.Color) string {
	tint := lipgloss.NewStyle().Background(background)
	const probe = "x"
	probeRendered := tint.Render(probe)
	probeIndex := strings.Index(probeRendered, probe)
	if probeIndex < 0 {
		return tint.Render(row)
	}
	backgroundStart := probeRendered[:probeIndex]
	restored := strings.ReplaceAll(row, "\x1b[0m", "\x1b[0m"+backgroundStart)
	return tint.Render(restored)
}
