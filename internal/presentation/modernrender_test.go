package presentation

import (
	"strings"
	"testing"

	"github.com/looprig/tui/styles"
)

// TestPadUserCard pins the modern user-card vertical padding: padUserCard brackets a user
// entry's rendered lines with one rail pad row above and below so the gray panel reads as a
// padded card rather than text flush to the panel edge. The pad rows carry the accent bar in
// styled (the rail runs unbroken top-to-bottom) but NO plain text (they are vertical
// whitespace — nothing extra reaches the clipboard), belong to the same entry as the content,
// and every line's sub is reassigned 0-based across the padded block so provenance stays
// unique and ordered. Empty input is returned unchanged.
func TestPadUserCard(t *testing.T) {
	t.Parallel()

	line := func(id displayID, sub int, s string) renderedLine {
		return renderedLine{styled: s, plain: s, entry: id, sub: sub}
	}
	tests := []struct {
		name    string
		in      []renderedLine
		wantLen int
	}{
		{name: "single line gains a pad row above and below", in: []renderedLine{line(3, 0, "▌ Hello")}, wantLen: 3},
		{name: "multi-line entry is bracketed once, not per line", in: []renderedLine{line(3, 0, "▌ one"), line(3, 1, "▌ two")}, wantLen: 4},
		{name: "empty input is returned unchanged", in: nil, wantLen: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := padUserCard(tt.in)
			if len(got) != tt.wantLen {
				t.Fatalf("padUserCard len = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantLen == 0 {
				return
			}
			// sub indices are contiguous 0..len-1, in order.
			for i, ln := range got {
				if ln.sub != i {
					t.Errorf("line %d sub = %d, want %d (0-based across the padded block)", i, ln.sub, i)
				}
			}
			id := tt.in[0].entry
			for _, pad := range []struct {
				where string
				ln    renderedLine
			}{{"top", got[0]}, {"bottom", got[len(got)-1]}} {
				if pad.ln.entry != id {
					t.Errorf("%s pad entry = %d, want %d (owned by the user entry)", pad.where, pad.ln.entry, id)
				}
				if pad.ln.plain != "" {
					t.Errorf("%s pad plain = %q, want empty (a pad row copies as nothing)", pad.where, pad.ln.plain)
				}
				if !strings.Contains(pad.ln.styled, styles.AccentBar) {
					t.Errorf("%s pad styled = %q, want the %q accent rail", pad.where, pad.ln.styled, styles.AccentBar)
				}
			}
			// The original content lines survive verbatim (styled/plain) between the pad rows.
			for i, orig := range tt.in {
				if got[i+1].styled != orig.styled || got[i+1].plain != orig.plain {
					t.Errorf("content line %d = {%q,%q}, want {%q,%q}", i, got[i+1].styled, got[i+1].plain, orig.styled, orig.plain)
				}
			}
		})
	}
}
