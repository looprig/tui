package tui

import (
	"strings"
	"testing"

	"github.com/looprig/cli/tui/styles"
)

// TestRailNode verifies the node-row builder: leading spine columns per depth, the
// pre-styled 2-column glyph, and hanging width-wrapped text whose continuation lines
// align under the first line's text (spine + a railWidth blank) rather than the glyph.
func TestRailNode(t *testing.T) {
	tests := []struct {
		name      string
		glyph     string
		text      string
		depth     int
		width     int
		wantFirst string // stripANSI of the first line
		wantLines int    // exact line count when >0; ignored when 0
	}{
		{name: "depth 0 lit dot", glyph: styles.LitDot, text: "hello", depth: 0, width: 40, wantFirst: "● hello", wantLines: 1},
		{name: "depth 1 tool ok", glyph: styles.ToolNode(styles.NodeOK), text: "Read(x)", depth: 1, width: 40, wantFirst: "│ ○ Read(x)", wantLines: 1},
		{name: "empty text glyph only", glyph: styles.LitDot, text: "", depth: 0, width: 40, wantFirst: "●", wantLines: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := railNode(tt.glyph, tt.text, tt.depth, tt.width)
			if len(got) == 0 {
				t.Fatalf("railNode returned no lines")
			}
			if first := stripANSI(got[0]); first != tt.wantFirst {
				t.Errorf("railNode first line = %q, want %q", first, tt.wantFirst)
			}
			if tt.wantLines > 0 && len(got) != tt.wantLines {
				t.Errorf("railNode line count = %d, want %d", len(got), tt.wantLines)
			}
		})
	}
}

// TestRailNodeWrap verifies that long node text wraps onto continuation lines that hang
// under the text (spine + railWidth blank), never under the glyph.
func TestRailNodeWrap(t *testing.T) {
	t.Parallel()
	// Narrow width forces a wrap: budget = width - railWidth*(depth+1) = 10-2 = 8 cols.
	got := railNode(styles.LitDot, "hello world here", 0, 10)
	if len(got) < 2 {
		t.Fatalf("railNode did not wrap: got %d line(s): %q", len(got), got)
	}
	cont := stripANSI(got[1])
	// Continuation aligns under the first line's TEXT: depth 0 spine is empty, so it is
	// exactly railWidth (2) leading blanks, then the wrapped word — never the glyph.
	if !strings.HasPrefix(cont, "  ") {
		t.Errorf("continuation line = %q, want it to start with a railWidth blank", cont)
	}
	if strings.Contains(cont, "●") {
		t.Errorf("continuation line = %q must not repeat the node glyph", cont)
	}
	if strings.TrimSpace(cont) == "" {
		t.Errorf("continuation line = %q carries no wrapped text", cont)
	}
}

// TestRailDetail verifies detail rows sit one rail level deeper than their node
// (railSpine(depth+1)) and carry faint width-wrapped text.
func TestRailDetail(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		depth int
		width int
		want  []string // stripANSI of each row
	}{
		{name: "depth 0 detail", text: "42 lines", depth: 0, width: 40, want: []string{"│ 42 lines"}},
		{name: "depth 1 detail", text: "12 lines", depth: 1, width: 40, want: []string{"│ │ 12 lines"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := railDetail(tt.text, tt.depth, tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("railDetail line count = %d, want %d (%q)", len(got), len(tt.want), got)
			}
			for i := range got {
				if s := stripANSI(got[i]); s != tt.want[i] {
					t.Errorf("railDetail line %d = %q, want %q", i, s, tt.want[i])
				}
			}
		})
	}
}

// TestRailConnector verifies the bare connector row is the node column continued as a
// rail with no glyph — just the vertical bars, trimmed of any trailing space.
func TestRailConnector(t *testing.T) {
	tests := []struct {
		name  string
		depth int
		want  string // stripANSI
	}{
		{name: "depth 0 connector", depth: 0, want: "│"},
		{name: "depth 1 connector", depth: 1, want: "│ │"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stripANSI(railConnector(tt.depth)); got != tt.want {
				t.Errorf("railConnector(%d) = %q, want %q", tt.depth, got, tt.want)
			}
		})
	}
}
