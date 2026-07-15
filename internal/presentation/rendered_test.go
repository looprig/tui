package presentation

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/looprig/core/content"
	"github.com/looprig/tui/styles"
)

// hasEscape reports whether s carries any terminal escape (a 0x1b byte). The
// provenance contract requires plain to be entirely ANSI-free, so this is the guard
// every "no escapes" assertion runs.
func hasEscape(s string) bool { return strings.ContainsRune(s, 0x1b) }

// thinkingEntry builds a real kindAssistant entry with multi-line reasoning and
// narration — the fixture whose thinking fold makes collapsed and expanded renders
// differ in line count.
func thinkingEntry(id displayID) entry {
	return entry{
		ID:   id,
		Kind: kindAssistant,
		Blocks: []content.Block{
			&content.ThinkingBlock{Thinking: "weighing\noptions\ncarefully\ndeeply"},
			&content.TextBlock{Text: "Here is the plan."},
		},
	}
}

// toolCardEntry builds a real kindTool entry: one resolved Bash card whose result
// exceeds previewLineCap so the card renders its header, body preview and trim marker.
func toolCardEntry(id displayID) entry {
	result := make([]string, 0, previewLineCap+3)
	for i := 0; i < previewLineCap+3; i++ {
		result = append(result, "line")
	}
	return entry{
		ID:   id,
		Kind: kindTool,
		Calls: []ToolCallView{{
			ToolExecutionID: callID(1),
			ToolName:        "Bash",
			Summary:         "ls -la",
			Status:          ToolOK,
			Result:          result,
		}},
	}
}

// TestPlainFromStyled locks the escape stripper: SGR color spans, a truecolor span,
// an empty-param reset, and OSC-8 hyperlink wrappers (BEL- and ST-terminated) are all
// removed to their visible text; a plain string and the empty string pass through
// unchanged. Every case additionally asserts the result carries no 0x1b byte.
func TestPlainFromStyled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "plain unchanged", in: "hello world", want: "hello world"},
		{name: "sgr color stripped", in: "\x1b[31mred\x1b[0m", want: "red"},
		{name: "truecolor stripped", in: "\x1b[38;2;1;2;3mx\x1b[0m", want: "x"},
		{name: "empty-param reset stripped", in: "\x1b[mtext\x1b[m", want: "text"},
		{name: "multiple spans stripped", in: "\x1b[1ma\x1b[0m\x1b[4mb\x1b[0m", want: "ab"},
		{name: "osc8 hyperlink BEL stripped to text", in: "\x1b]8;;https://example.com\x07link\x1b]8;;\x07", want: "link"},
		{name: "osc8 hyperlink ST stripped to text", in: "\x1b]8;;https://x\x1b\\link\x1b]8;;\x1b\\", want: "link"},
		{name: "osc then sgr around text", in: "\x1b]8;;u\x07\x1b[4mlink\x1b[m\x1b]8;;\x07", want: "link"},
		{name: "wide rune preserved", in: "\x1b[1m世\x1b[0m", want: "世"},
		// Raw-payload escape families the renderer never emits but a tool result can carry
		// verbatim — each must be stripped to its visible text (fail-secure boundary).
		{name: "colon-form sgr stripped", in: "\x1b[38:2:1:2:3mx\x1b[0m", want: "x"},
		{name: "dcs string sequence stripped", in: "\x1bP1;2q data\x1b\\visible", want: "visible"},
		{name: "sos string sequence stripped", in: "\x1bXjunk\x1b\\keep", want: "keep"},
		{name: "apc string sequence stripped", in: "\x1b_junk\x07keep", want: "keep"},
		{name: "charset select stripped", in: "\x1b(Btext", want: "text"},
		{name: "two-byte escape stripped", in: "a\x1bMb", want: "ab"},
		{name: "bare escape removed by guard", in: "\x1b", want: ""},
		{name: "trailing lone escape removed by guard", in: "text\x1b", want: "text"},
		{name: "double lone escape removed by guard", in: "ab\x1b\x1b", want: "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := plainFromStyled(tt.in)
			if got != tt.want {
				t.Errorf("plainFromStyled(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if hasEscape(got) {
				t.Errorf("plainFromStyled(%q) = %q, still contains a 0x1b escape byte", tt.in, got)
			}
		})
	}
}

// TestPlainFromStyledWidth locks that a stripped line is measurable by lipgloss.Width
// and that wide runes count 2 cells — the property later cell↔rune mapping relies on.
// Each styled input wraps its runes in an SGR span; the width is asserted on the plain.
func TestPlainFromStyledWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        string
		wantPlain string
		wantWidth int
	}{
		{name: "ascii is one cell per rune", in: "\x1b[1mabc\x1b[0m", wantPlain: "abc", wantWidth: 3},
		{name: "cjk is two cells per rune", in: "\x1b[1m世界\x1b[0m", wantPlain: "世界", wantWidth: 4},
		{name: "emoji is two cells", in: "\x1b[1m🚀\x1b[0m", wantPlain: "🚀", wantWidth: 2},
		{name: "mixed narrow and wide", in: "\x1b[38;2;1;2;3ma世\x1b[m", wantPlain: "a世", wantWidth: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := plainFromStyled(tt.in)
			if got != tt.wantPlain {
				t.Errorf("plainFromStyled(%q) = %q, want %q", tt.in, got, tt.wantPlain)
			}
			if hasEscape(got) {
				t.Errorf("plainFromStyled(%q) = %q, still contains a 0x1b escape byte", tt.in, got)
			}
			if w := lipgloss.Width(got); w != tt.wantWidth {
				t.Errorf("lipgloss.Width(%q) = %d, want %d", got, w, tt.wantWidth)
			}
		})
	}
}

// TestRenderEntryLinesNoEscapes renders REAL entries (a thinking assistant row in
// both fold states, a resolved tool card, and a user row carrying a markdown link that
// glamour emits as an OSC-8 hyperlink) and asserts EVERY renderedLine.plain is entirely
// ANSI-free — the styled saturation never leaks into the copy/measure text.
func TestRenderEntryLinesNoEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		e         entry
		collapsed bool
	}{
		{name: "thinking collapsed", e: thinkingEntry(1), collapsed: true},
		{name: "thinking expanded", e: thinkingEntry(1), collapsed: false},
		{name: "tool card", e: toolCardEntry(1), collapsed: true},
		{
			name:      "user with markdown link",
			e:         entry{ID: 1, Kind: kindUser, Blocks: []content.Block{&content.TextBlock{Text: "see [link](https://example.com) now"}}},
			collapsed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := renderEntryLines(tt.e, 80, tt.collapsed)
			if len(lines) == 0 {
				t.Fatalf("renderEntryLines(%s) returned no lines", tt.name)
			}
			for i, rl := range lines {
				if hasEscape(rl.plain) {
					t.Errorf("%s line %d plain = %q, must be ANSI-free (no 0x1b)", tt.name, i, rl.plain)
				}
			}
		})
	}
}

// TestRenderEntryLinesRawEscapes locks the fail-secure boundary on REAL rendered
// output: a tool-card body passes ToolCallView.Result through VERBATIM, so a raw
// subprocess byte stream can carry escape families the renderer never emits (DCS,
// SOS/APC, charset-select, colon-form SGR, a lone ESC). Rendered through
// renderEntryLines, NO line's plain may contain a 0x1b byte — nothing leaks to the
// clipboard. Each case also asserts the raw escape genuinely reached the styled output,
// so the fixture truly exercises the strip path rather than passing vacuously.
func TestRenderEntryLinesRawEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result []string
	}{
		{name: "dcs body", result: []string{"before\x1bP1;2qPAYLOAD\x1b\\after"}},
		{name: "charset select body", result: []string{"before\x1b(Bafter"}},
		{name: "colon-form sgr body", result: []string{"before\x1b[38:2:1:2:3mafter\x1b[0m"}},
		{name: "sos and apc bodies", result: []string{"a\x1bXsos\x1b\\b", "c\x1b_apc\x07d"}},
		{name: "lone escape body", result: []string{"tail\x1b"}},
		{
			name: "mixed exotic families in one card",
			result: []string{
				"dcs\x1bP0qX\x1b\\end",
				"charset\x1b(Bmid",
				"colon\x1b[38:2:9:9:9mred\x1b[0m lone\x1b",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := entry{ID: 7, Kind: kindTool, Calls: []ToolCallView{{
				ToolExecutionID: callID(2),
				ToolName:        "Bash",
				Summary:         "run",
				Status:          ToolOK,
				Result:          tt.result,
			}}}
			lines := renderEntryLines(e, 80, true)
			if len(lines) == 0 {
				t.Fatalf("renderEntryLines(%s) returned no lines", tt.name)
			}
			sawStyledESC := false
			for _, rl := range lines {
				if hasEscape(rl.styled) {
					sawStyledESC = true
				}
				if hasEscape(rl.plain) {
					t.Errorf("%s sub=%d plain = %q, must be ANSI-free (raw escape leaked to clipboard)", tt.name, rl.sub, rl.plain)
				}
			}
			if !sawStyledESC {
				t.Errorf("%s: no styled line carried a raw ESC; fixture does not exercise the strip path", tt.name)
			}
		})
	}
}

// TestRenderEntryLinesPlainEqualsVisible locks that plain equals the exact visible
// text — the color codes are gone and the characters are correct — for simple entries
// with a deterministic single-line render.
func TestRenderEntryLinesPlainEqualsVisible(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		e    entry
		want string
	}{
		{
			name: "interrupted tombstone",
			e:    entry{ID: 1, Kind: kindInterrupted},
			want: interruptedTombstone,
		},
		{
			name: "notice text behind the accent bar",
			e:    entry{ID: 1, Kind: kindNotice, Level: noticeInfo, Blocks: []content.Block{&content.TextBlock{Text: "hello world"}}},
			want: styles.AccentBarPrompt + "hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := renderEntryLines(tt.e, 80, false)
			if len(lines) != 1 {
				t.Fatalf("renderEntryLines(%s) = %d lines, want exactly 1", tt.name, len(lines))
			}
			if lines[0].plain != tt.want {
				t.Errorf("plain = %q, want %q", lines[0].plain, tt.want)
			}
			if hasEscape(lines[0].plain) {
				t.Errorf("plain = %q, must be ANSI-free", lines[0].plain)
			}
		})
	}
}

// TestRenderEntryLinesProvenance locks that every renderedLine carries the source
// entry's displayID and a contiguous 0-based sub index (0..n-1 in order), across a
// range of entry kinds and both fold states. The multi-line fixtures (expanded thinking,
// a two-line user row) make the sub sequence non-trivial.
func TestRenderEntryLinesProvenance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		e         entry
		collapsed bool
	}{
		{name: "thinking expanded", e: thinkingEntry(42), collapsed: false},
		{name: "thinking collapsed", e: thinkingEntry(42), collapsed: true},
		{name: "tool card", e: toolCardEntry(7), collapsed: true},
		{
			name:      "multi-line user row",
			e:         entry{ID: 5, Kind: kindUser, Blocks: []content.Block{&content.TextBlock{Text: "line one\nline two"}}},
			collapsed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := renderEntryLines(tt.e, 80, tt.collapsed)
			if len(lines) == 0 {
				t.Fatalf("renderEntryLines(%s) returned no lines", tt.name)
			}
			for i, rl := range lines {
				if rl.entry != tt.e.ID {
					t.Errorf("%s line %d entry = %d, want %d", tt.name, i, rl.entry, tt.e.ID)
				}
				if rl.sub != i {
					t.Errorf("%s line %d sub = %d, want %d", tt.name, i, rl.sub, i)
				}
			}
		})
	}
}

// TestRenderEntryLinesWidthMatchesStyled locks that stripping ANSI does not change a
// line's display width: for every rendered line lipgloss.Width(plain) equals
// lipgloss.Width(styled). The fixtures include wide CJK content, so this proves plain
// preserves the styled line's CELL width across wide runes (the cell↔rune invariant).
func TestRenderEntryLinesWidthMatchesStyled(t *testing.T) {
	t.Parallel()

	entries := []entry{
		thinkingEntry(1),
		toolCardEntry(2),
		{ID: 3, Kind: kindUser, Blocks: []content.Block{&content.TextBlock{Text: "世界 domains 世界"}}},
	}
	for _, e := range entries {
		for _, collapsed := range []bool{true, false} {
			for i, rl := range renderEntryLines(e, 80, collapsed) {
				if pw, sw := lipgloss.Width(rl.plain), lipgloss.Width(rl.styled); pw != sw {
					t.Errorf("entry %d collapsed=%v line %d: width(plain)=%d width(styled)=%d, want equal", e.ID, collapsed, i, pw, sw)
				}
			}
		}
	}
}

// TestRenderEntryLinesCollapse locks that the collapse flag is threaded through to the
// underlying fold: a thinking entry rendered collapsed yields FEWER lines than the same
// entry expanded (collapsed folds the reasoning to a one-line summary; expanded shows
// the full rail). Both renders keep contiguous sub indices.
func TestRenderEntryLinesCollapse(t *testing.T) {
	t.Parallel()

	e := thinkingEntry(1)
	collapsed := renderEntryLines(e, 80, true)
	expanded := renderEntryLines(e, 80, false)

	if len(collapsed) >= len(expanded) {
		t.Errorf("collapsed=%d lines, expanded=%d lines, want collapsed < expanded", len(collapsed), len(expanded))
	}
	for _, lines := range [][]renderedLine{collapsed, expanded} {
		for i, rl := range lines {
			if rl.sub != i {
				t.Errorf("sub index %d out of order (got %d)", i, rl.sub)
			}
		}
	}
}
