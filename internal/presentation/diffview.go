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

// diffHighlightStyle is resolved during single-threaded package initialization.
// Chroma's global style registry is not synchronized, and Glamour may register its
// code-block theme while the TUI concurrently renders a diff; retaining this immutable
// pointer keeps the render path from reading that registry during those writes.
var diffHighlightStyle = chromastyles.Get("github-dark")

// renderDiff colorizes an already-rendered unified diff. It deliberately does no
// diffing of its own: the tool is responsible for the mutation description and the
// TUI only turns its stable line prefixes into a compact presentation.
func renderDiff(diff, path string, width int) []string {
	if diff == "" || width <= 0 {
		return nil
	}

	lexer := lexerForDiffPath(path)
	return renderDiffRowsWithLexer(parseDiffRows(diff), lexer, width)
}

type diffRowKind uint8

const (
	diffRowPlain diffRowKind = iota
	diffRowHunk
	diffRowDeletion
	diffRowAddition
	diffRowContext
)

type diffRow struct {
	kind diffRowKind
	text string
}

// parseDiffRows performs only structural normalization and classification. It does
// not sanitize, tokenize or style content, so callers may cheaply count and select a
// visible window before any syntax-highlighting work occurs.
func parseDiffRows(diff string) []diffRow {
	if diff == "" {
		return nil
	}
	rawLines := strings.Split(strings.ReplaceAll(diff, "\r\n", "\n"), "\n")
	rows := make([]diffRow, 0, len(rawLines))
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
		kind := diffRowPlain
		switch {
		case strings.HasPrefix(line, "@@"):
			kind = diffRowHunk
		case strings.HasPrefix(line, "-"):
			kind = diffRowDeletion
		case strings.HasPrefix(line, "+"):
			kind = diffRowAddition
		case strings.HasPrefix(line, " "):
			kind = diffRowContext
		}
		rows = append(rows, diffRow{kind: kind, text: line})
	}
	return rows
}

func renderDiffRowsWithLexer(rows []diffRow, lexer chroma.Lexer, width int) []string {
	if width <= 0 || len(rows) == 0 {
		return nil
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		line := visibleTerminalText(row.text)
		var rendered string
		switch row.kind {
		case diffRowHunk:
			rendered = styles.StatusStyle.Render(line)
		case diffRowDeletion:
			rendered = tintDiffRow("-"+highlightDiffBody(line[1:], lexer), styles.DiffDeletionBackgroundColor)
		case diffRowAddition:
			rendered = tintDiffRow("+"+highlightDiffBody(line[1:], lexer), styles.DiffAdditionBackgroundColor)
		case diffRowContext:
			rendered = " " + highlightDiffBody(line[1:], lexer)
		default:
			rendered = line
		}
		lines = append(lines, truncate(rendered, width))
	}
	return lines
}

type permissionDiffRows struct {
	rows    []diffRow
	omitted int
}

// permissionDiffWindow plans the permission card's raw visible window before the
// sanitizer or Chroma sees any content. omitted counts only renderable rows: stripped
// file headers and a trailing newline never inflate the marker.
func permissionDiffWindow(diff string, visibleRows int) permissionDiffRows {
	return permissionDiffWindowFromRows(parseDiffRows(diff), visibleRows)
}

func permissionDiffWindowFromRows(rows []diffRow, visibleRows int) permissionDiffRows {
	if visibleRows < 0 {
		visibleRows = 0
	}
	if visibleRows >= len(rows) {
		return permissionDiffRows{rows: rows}
	}
	return permissionDiffRows{rows: rows[:visibleRows], omitted: len(rows) - visibleRows}
}

// visibleTerminalText is the trust boundary between untrusted display text and terminal
// markup. Every terminal control becomes a printable Go-style escape before Chroma or
// lipgloss can add trusted ANSI. Unicode's narrow Bidi_Control set is escaped too so text
// cannot reorder an approval display; ordinary Unicode (including joining, combining, and
// emoji code points) remains intact. Each invalid UTF-8 byte gets its own \xNN escape, and
// literal backslashes are doubled, making the visible encoding injective over input bytes.
func visibleTerminalText(text string) string {
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
	style := diffHighlightStyle
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
