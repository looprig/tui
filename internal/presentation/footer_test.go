package presentation

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestLoopFooterWrapsAtSegmentsAndHitTestsRenderedRows(t *testing.T) {
	first, second := callID(1), callID(2)
	footer := loopFooter{
		header: "CodeRig · Writable · /workspace",
		bar: loopBar{entries: []loopBarEntry{
			{id: first, name: "operator-primary", live: true},
			{id: second, name: "operator", live: true},
		}, focused: first},
	}
	const width = 34
	view := footer.View(width)
	lines := strings.Split(view, "\n")
	if len(lines) < 2 {
		t.Fatalf("footer did not wrap at segment boundary: %q", view)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > width {
			t.Fatalf("footer row width = %d, want <= %d: %q", lipgloss.Width(line), width, line)
		}
	}
	_, hits := footer.layout(width)
	for _, hit := range hits {
		if got, ok := footer.HitTest(hit.start, hit.row, width); !ok || got != hit.id {
			t.Fatalf("HitTest(%d,%d) = %s,%v, want %s,true", hit.start, hit.row, got, ok, hit.id)
		}
	}
}
