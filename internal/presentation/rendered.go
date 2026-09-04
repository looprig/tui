package presentation

import (
	"regexp"
	"strings"

	"github.com/looprig/tui/styles"
)

// renderedLine is one rendered transcript line carrying BOTH its drawn form and the
// provenance the modern viewport needs. styled is the ANSI-saturated string the
// existing entry renderer produced (drawn verbatim); plain is the ANSI-free visible
// text — the ONLY thing selection measures, extracts and copies, so no escape sequence
// ever reaches the clipboard and cell↔rune math operates on measurable text. entry and
// sub locate the line: entry is the source entry's displayID (for click-to-collapse and
// stable anchoring) and sub is the 0-based line index within that entry. clickable is
// deliberately row-level provenance: only a header whose click produces a visible fold
// change sets it, so hover styling can never make passive transcript text look actionable.
type renderedLine struct {
	styled    string    // drawn (ANSI) — from the existing renderer, unchanged
	plain     string    // ANSI-free visible text; the ONLY thing selection extracts/copies
	entry     displayID // provenance: which entry this line belongs to
	sub       int       // intra-entry line index (0-based)
	clickable bool      // true only when clicking this exact row performs a visible action
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

// toolRunSummaryLines renders a collapsed contiguous run of tool entries as one semantic
// activity summary node. Its first line carries sub == 0 and entry == the run's
// first displayID (runID), so a click toggles the whole run's fold via the existing
// header-click handler; ctrl+t (ToggleAll) flips it with the global default. The node is
// red-tinted when any call failed.
func toolRunSummaryLines(run []entry, width int) []renderedLine {
	runID := run[0].ID
	calls := make([]ToolCallView, 0, len(run))
	for i := range run {
		if len(run[i].Calls) > 0 {
			calls = append(calls, run[i].Calls[0])
		}
	}
	text, anyFailed := toolRunSummary(calls)
	status := styles.NodeOK
	if anyFailed {
		status = styles.NodeFailed
	}
	styled := railNodeStyled(styles.ToolNode(status), text, styles.ToolCallStyle, 0, width)
	out := make([]renderedLine, len(styled))
	for i, line := range styled {
		out[i] = renderedLine{styled: line, plain: plainFromStyled(line), entry: runID, sub: i, clickable: i == 0}
	}
	return out
}

// The escape-stripping passes, compiled once. plainFromStyled feeds the system
// clipboard (a trust boundary), and tool-card bodies pass raw subprocess bytes through
// VERBATIM, so a styled line may carry escape families the renderer itself never emits.
// The passes therefore cover far more than glamour/lipgloss output, and a final guard
// makes "no 0x1b byte" a hard guarantee (fail secure: over-stripping exotic malformed
// content at the clipboard boundary is the correct tradeoff).
var (
	// ansiOSC matches an OSC string sequence: ESC ']', a body, and a BEL (0x07) or ST
	// (ESC '\\') terminator. It covers the OSC-8 hyperlink wrappers glamour emits for
	// markdown links, stripping the wrapper to its visible link text.
	ansiOSC = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)")
	// ansiString matches the other string sequences that can appear in raw payloads —
	// DCS (ESC P), SOS (ESC X), PM (ESC ^) and APC (ESC _) — up to their ST or BEL
	// terminator. Stripped BEFORE the generic single-escape pass so the introducer byte
	// (P/X/^/_) is not mis-eaten as a bare escape's final byte.
	ansiString = regexp.MustCompile("\x1b[P^X_].*?(?:\x1b\\\\|\x07)")
	// ansiCSI matches a CSI sequence: ESC '[', parameter bytes (0-9 ; : ?), optional
	// intermediate bytes (0x20–0x2F) and one final byte (0x40–0x7E). The ':' allows the
	// ISO-8613-6 colon sub-parameter form (e.g. truecolor "38:2:r:g:b") a subprocess may
	// emit, which the plain ';'/'?' class would miss.
	ansiCSI = regexp.MustCompile("\x1b\\[[0-9;:?]*[ -/]*[@-~]")
	// ansiEscape matches an nF / two-byte / charset-select escape: ESC, optional
	// intermediate bytes (0x20–0x2F) and one final byte (0x30–0x7E) — e.g. "\x1b(B",
	// "\x1bM". It runs AFTER the string-sequence and CSI passes so a well-formed OSC/DCS/
	// CSI is already gone and only genuine single-byte-final escapes remain to match.
	ansiEscape = regexp.MustCompile("\x1b[ -/]*[0-~]")
)

// plainFromStyled strips ALL terminal escape sequences from a styled line so the result
// is exactly the visible text — no 0x1b byte remains, GUARANTEED. This is the correct
// semantic for "copy what you see": selection measures and extracts plain, never the
// styled string (whose ANSI would corrupt cell math and reach the clipboard). Because
// tool-card bodies carry raw subprocess bytes, the styled line may hold escape families
// the renderer never emits; the passes cover OSC, DCS/SOS/PM/APC, CSI (with colon
// sub-parameters) and generic single escapes, in an order where string sequences are
// removed before the generic pass, then a final guard removes any residual ESC left by a
// malformed or truncated sequence — the hard fail-secure guarantee at the clipboard
// boundary.
//
// The passes are applied in the same order and to the same intermediate strings as the
// regex-only original (each pass sees the previous pass's output — a sequence a single
// fused scan would NOT reproduce, since removing one sequence can join its neighbours
// into another). What changed is only how much work each pass does:
//
//   - Each string-sequence pass is guarded by a cheap substring test for its introducer.
//     A pass whose introducer is absent provably cannot match, so skipping it cannot
//     change the result. Glamour/Lipgloss output carries CSI colour sequences and
//     nothing else, so in the overwhelmingly common case only the CSI pass runs.
//   - The CSI pass is hand-scanned rather than compiled. Its three character classes are
//     mutually disjoint ([0-9;:?], then 0x20-0x2F, then one 0x40-0x7E), so greedy
//     matching is deterministic and needs no backtracking — which is exactly what made
//     the regex version costly, at ~12us per line across thousands of lines per frame.
//
// TestPlainFromStyledMatchesRegexpReference and FuzzPlainFromStyledMatchesRegexp pin this
// to the original regex implementation, which is retained verbatim in the test as the
// reference. Any divergence is a test failure, so the fast path cannot drift from the
// grammar the security comment above describes.
func plainFromStyled(styled string) string {
	if !strings.ContainsRune(styled, ansiESC) {
		return styled
	}
	s := styled
	if strings.Contains(s, "\x1b]") {
		s = ansiOSC.ReplaceAllString(s, "")
	}
	if containsESCIntroducer(s, "P^X_") {
		s = ansiString.ReplaceAllString(s, "")
	}
	s = stripCSI(s)
	if strings.ContainsRune(s, ansiESC) {
		s = ansiEscape.ReplaceAllString(s, "")
		s = strings.ReplaceAll(s, "\x1b", "")
	}
	return s
}

// ansiESC is the escape byte introducing every sequence stripped here.
const ansiESC = '\x1b'

// containsESCIntroducer reports whether s holds an ESC immediately followed by one of
// the introducer bytes — the precondition for ansiString to match anywhere in s.
func containsESCIntroducer(s string, introducers string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == ansiESC && strings.IndexByte(introducers, s[i+1]) >= 0 {
			return true
		}
	}
	return false
}

// stripCSI removes every CSI sequence, exactly as ansiCSI.ReplaceAllString(s, "") does:
// ESC '[', parameter bytes [0-9;:?], intermediate bytes 0x20-0x2F, one final byte
// 0x40-0x7E. A position that fails to complete the sequence is left intact and the scan
// resumes one byte later, matching the regex engine's leftmost-match retry. Scanning by
// BYTE is safe for UTF-8 text: every byte of a multi-byte rune is >= 0x80 and so falls
// outside all three classes and cannot be mistaken for part of a sequence.
func stripCSI(s string) string {
	start := strings.Index(s, "\x1b[")
	if start < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:start])

	for i := start; i < len(s); {
		if s[i] != ansiESC || i+1 >= len(s) || s[i+1] != '[' {
			b.WriteByte(s[i])
			i++
			continue
		}
		j := i + 2
		for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == ';' || s[j] == ':' || s[j] == '?') {
			j++
		}
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2F {
			j++
		}
		if j < len(s) && s[j] >= 0x40 && s[j] <= 0x7E {
			i = j + 1 // complete sequence: drop it
			continue
		}
		// Incomplete: not a CSI match here, so the ESC stays and the scan retries at i+1.
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
