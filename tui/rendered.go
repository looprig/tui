package tui

import (
	"regexp"
)

// renderedLine is one rendered transcript line carrying BOTH its drawn form and the
// provenance the modern viewport needs. styled is the ANSI-saturated string the
// existing entry renderer produced (drawn verbatim); plain is the ANSI-free visible
// text — the ONLY thing selection measures, extracts and copies, so no escape sequence
// ever reaches the clipboard and cell↔rune math operates on measurable text. entry and
// sub locate the line: entry is the source entry's displayID (for click-to-collapse and
// stable anchoring) and sub is the 0-based line index within that entry.
type renderedLine struct {
	styled string    // drawn (ANSI) — from the existing renderer, unchanged
	plain  string    // ANSI-free visible text; the ONLY thing selection extracts/copies
	entry  displayID // provenance: which entry this line belongs to
	sub    int       // intra-entry line index (0-based)
}

// renderEntryLines renders one committed entry to its provenance-carrying lines. It
// WRAPS the existing scrollback renderer (renderEntry) — the styled output is the exact
// scrollback lines, byte for byte — and, per styled line, attaches the ANSI-free plain
// text and the (entry, sub) provenance. collapsed is the modern viewport's fold state
// and is the INVERSE of renderEntry's expand flag, so a collapsed thinking fold yields
// fewer lines than an expanded one. It never re-implements or alters rendering.
func renderEntryLines(e entry, width int, collapsed bool) []renderedLine {
	styled := renderEntry(e, !collapsed, width)
	out := make([]renderedLine, len(styled))
	for i, line := range styled {
		out[i] = renderedLine{
			styled: line,
			plain:  plainFromStyled(line),
			entry:  e.ID,
			sub:    i,
		}
	}
	return out
}

// ansiCSI matches a CSI escape sequence: ESC '[', optional parameter bytes ([0-9;?]),
// optional intermediate bytes (0x20–0x2F) and one final byte (0x40–0x7E). It covers
// every SGR color/style span glamour and lipgloss emit. Compiled once as a package var.
var ansiCSI = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// ansiOSC matches an OSC escape sequence: ESC ']', a string body, and a BEL (0x07) or
// ST (ESC '\\') terminator. It covers the OSC-8 hyperlink wrappers glamour emits for
// markdown links, stripping the wrapper to its visible link text. Compiled once.
var ansiOSC = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")

// plainFromStyled strips ALL terminal escape sequences from a styled line so the result
// is exactly the visible text — no 0x1b byte remains. This is the correct semantic for
// "copy what you see": selection measures and extracts plain, never the styled string
// (whose ANSI would corrupt cell math and reach the clipboard). OSC wrappers are removed
// first (a hyperlink collapses to its text) then CSI/SGR spans.
func plainFromStyled(styled string) string {
	s := ansiOSC.ReplaceAllString(styled, "")
	return ansiCSI.ReplaceAllString(s, "")
}
