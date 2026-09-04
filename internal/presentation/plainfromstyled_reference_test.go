package presentation

import (
	"strings"
	"testing"
)

// plainFromStyledReference is the ORIGINAL, regex-only implementation of plainFromStyled,
// retained verbatim as the executable specification of the escape-stripping grammar. The
// production function was optimized (introducer guards plus a hand-scanned CSI pass)
// because it ran over every rendered line on every frame; this reference is what proves
// that optimization changed only the cost, never the output. It intentionally shares the
// package's compiled regexes, so a change to the grammar updates both at once.
func plainFromStyledReference(styled string) string {
	s := ansiOSC.ReplaceAllString(styled, "")
	s = ansiString.ReplaceAllString(s, "")
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiEscape.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "\x1b", "")
}

// The cases below deliberately include sequences the renderer never emits: tool-card
// bodies pass raw subprocess bytes through verbatim, and plainFromStyled feeds the system
// clipboard, so malformed and exotic input is in scope.
func TestPlainFromStyledMatchesRegexpReference(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"no escapes", "just visible text"},
		{"utf8 no escapes", "héllo 世界 🎉 combining é"},
		{"simple sgr", "\x1b[31mred\x1b[0m"},
		{"truecolor semicolon", "\x1b[38;2;255;128;0morange\x1b[0m"},
		{"truecolor colon subparams", "\x1b[38:2:255:128:0morange\x1b[0m"},
		{"csi private marker", "\x1b[?25lhidden\x1b[?25h"},
		{"csi with intermediates", "\x1b[1 qtext"},
		{"osc8 hyperlink", "\x1b]8;;https://example.com\x07link text\x1b]8;;\x07"},
		{"osc st terminated", "\x1b]0;window title\x1b\\rest"},
		{"osc unterminated", "\x1b]8;;https://example.com no terminator"},
		{"dcs", "\x1bPsomething\x1b\\after"},
		{"apc bel terminated", "\x1b_payload\x07after"},
		{"pm sequence", "\x1b^private\x1b\\tail"},
		{"sos sequence", "\x1bXdata\x1b\\tail"},
		{"charset select", "\x1b(Btext"},
		{"two byte escape", "\x1bMtext"},
		{"bare escape only", "\x1b"},
		{"trailing bare escape", "text\x1b"},
		{"esc bracket truncated", "text\x1b["},
		{"esc bracket params truncated", "text\x1b[38;2;255"},
		{"nested-looking escapes", "\x1b[31m\x1b]8;;u\x07\x1b[0mtext"},
		{"csi split by osc removal", "\x1b[3\x1b]0;t\x071m"},
		{"repeated escapes", strings.Repeat("\x1b[1m\x1b[0m", 50) + "end"},
		{"escape inside utf8", "世\x1b[31m界"},
		{"only escapes", "\x1b[0m\x1b[0m"},
		{"esc followed by high byte", "\x1b\xc3\xa9text"},
		{"csi final out of range", "\x1b[38\x7ftext"},
		{"osc containing esc", "\x1b]8;;\x1b[31m\x07text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := plainFromStyledReference(tt.input)
			got := plainFromStyled(tt.input)
			if got != want {
				t.Errorf("plainFromStyled(%q)\n got: %q\nwant: %q", tt.input, got, want)
			}
			if strings.ContainsRune(got, ansiESC) {
				t.Errorf("plainFromStyled(%q) left an ESC byte in %q", tt.input, got)
			}
		})
	}
}

// FuzzPlainFromStyledMatchesRegexp is the real guarantee: for ANY input, the optimized
// implementation must agree byte for byte with the regex reference, and must never leave
// an ESC behind (the fail-secure property at the clipboard boundary).
func FuzzPlainFromStyledMatchesRegexp(f *testing.F) {
	seeds := []string{
		"", "plain", "\x1b[31mred\x1b[0m", "\x1b]8;;url\x07text", "\x1bPdcs\x1b\\",
		"\x1b[38:2:1:2:3m", "\x1b(B", "\x1b", "\x1b[", "世\x1b[0m界", "\x1b^pm\x07",
		"\x1b[3\x1b]0;t\x071m", "\x1b_apc\x1b\\", "\x1bX\x07",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, styled string) {
		want := plainFromStyledReference(styled)
		got := plainFromStyled(styled)
		if got != want {
			t.Fatalf("divergence for %q\n got: %q\nwant: %q", styled, got, want)
		}
		if strings.ContainsRune(got, ansiESC) {
			t.Fatalf("ESC survived stripping of %q: %q", styled, got)
		}
	})
}
