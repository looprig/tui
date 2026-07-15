package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/looprig/tui/styles"
)

// TestRailNode verifies the node-row builder: leading spine columns per depth, the
// pre-styled 2-column glyph, and hanging width-wrapped text whose continuation lines
// align under the first line's text (spine + a RailWidth blank) rather than the glyph.
func TestRailNode(t *testing.T) {
	tests := []struct {
		name      string
		glyph     string
		text      string
		depth     int
		width     int
		wantFirst string // ansi.Strip of the first line
		wantLines int    // exact line count when >0; ignored when 0
	}{
		{name: "depth 0 lit dot", glyph: styles.LitDot, text: "hello", depth: 0, width: 40, wantFirst: "● hello", wantLines: 1},
		{name: "depth 1 tool ok", glyph: styles.ToolNode(styles.NodeOK), text: "Read(x)", depth: 1, width: 40, wantFirst: "│ ○ Read(x)", wantLines: 1},
		{name: "empty text glyph only", glyph: styles.LitDot, text: "", depth: 0, width: 40, wantFirst: "●", wantLines: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RailNode(tt.glyph, tt.text, tt.depth, tt.width)
			if len(got) == 0 {
				t.Fatalf("RailNode returned no lines")
			}
			if first := ansi.Strip(got[0]); first != tt.wantFirst {
				t.Errorf("RailNode first line = %q, want %q", first, tt.wantFirst)
			}
			if tt.wantLines > 0 && len(got) != tt.wantLines {
				t.Errorf("RailNode line count = %d, want %d", len(got), tt.wantLines)
			}
		})
	}
}

// TestRailNodeWrap verifies that long node text wraps onto continuation lines that hang
// under the text (spine + RailWidth blank), never under the glyph.
func TestRailNodeWrap(t *testing.T) {
	t.Parallel()
	// Narrow width forces a wrap: budget = width - RailWidth*(depth+1) = 10-2 = 8 cols.
	got := RailNode(styles.LitDot, "hello world here", 0, 10)
	if len(got) < 2 {
		t.Fatalf("RailNode did not wrap: got %d line(s): %q", len(got), got)
	}
	cont := ansi.Strip(got[1])
	// Continuation aligns under the first line's TEXT: depth 0 spine is empty, so it is
	// exactly RailWidth (2) leading blanks, then the wrapped word — never the glyph.
	if !strings.HasPrefix(cont, "  ") {
		t.Errorf("continuation line = %q, want it to start with a RailWidth blank", cont)
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
		want  []string // ansi.Strip of each row
	}{
		{name: "depth 0 detail", text: "42 lines", depth: 0, width: 40, want: []string{"│ 42 lines"}},
		{name: "depth 1 detail", text: "12 lines", depth: 1, width: 40, want: []string{"│ │ 12 lines"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := RailDetail(tt.text, tt.depth, tt.width)
			if len(got) != len(tt.want) {
				t.Fatalf("RailDetail line count = %d, want %d (%q)", len(got), len(tt.want), got)
			}
			for i := range got {
				if s := ansi.Strip(got[i]); s != tt.want[i] {
					t.Errorf("RailDetail line %d = %q, want %q", i, s, tt.want[i])
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
		want  string // ansi.Strip
	}{
		{name: "depth 0 connector", depth: 0, want: "│"},
		{name: "depth 1 connector", depth: 1, want: "│ │"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ansi.Strip(RailConnector(tt.depth)); got != tt.want {
				t.Errorf("RailConnector(%d) = %q, want %q", tt.depth, got, tt.want)
			}
		})
	}
}
