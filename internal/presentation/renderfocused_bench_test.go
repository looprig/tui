package presentation

import (
	"fmt"
	"testing"

	"github.com/looprig/core/content"
)

// benchScreen builds a Screen holding n committed entries of mixed kind.
func benchScreen(n int) Screen {
	var s Screen
	s.width = 100
	s.collapse = newCollapseState()
	body := "Here is a paragraph of assistant narration with `inline code` and a list:\n\n- one\n- two\n- three\n\nAnd a closing sentence that is long enough to wrap across the available width of the terminal frame.\n"
	entries := make([]entry, 0, n)
	for i := 0; i < n; i++ {
		id := displayID(i + 1)
		switch i % 3 {
		case 0:
			entries = append(entries, entry{ID: id, Kind: kindUser, Blocks: []content.Block{&content.TextBlock{Text: "please do the thing number " + fmt.Sprint(i)}}})
		case 1:
			entries = append(entries, entry{ID: id, Kind: kindAssistant, Blocks: []content.Block{
				&content.ThinkingBlock{Thinking: "considering the options carefully"},
				&content.TextBlock{Text: body},
			}})
		default:
			entries = append(entries, entry{ID: id, Kind: kindAssistant, Blocks: []content.Block{&content.TextBlock{Text: body}}})
		}
	}
	s.transcript = transcriptModel{global: entries, nextID: displayID(n + 1)}
	return s
}

// BenchmarkRenderFocusedScaling guards the cost of a warm re-render — what every
// subscription event, including each streamed token delta, pays. renderFocused is O(the
// whole transcript), so this stays linear in entry count; what matters is the constant.
// It was ~570us per entry before the markdown memo (mdcache.go) and the hand-scanned CSI
// strip (plainFromStyled), which together left the loop unable to keep up with a
// streaming turn on a long session and pushed mouse-wheel input seconds behind the
// pointer. It is ~23us per entry now. A large regression here means scrolling has gone
// sluggish mid-turn again.
func BenchmarkRenderFocusedScaling(b *testing.B) {
	for _, n := range []int{100, 400, 800} {
		s := benchScreen(n)
		b.Run(fmt.Sprintf("entries=%d", n), func(b *testing.B) {
			resetMDCache()
			s.renderFocused() // warm, as a live session's steady state is
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = s.renderFocused()
			}
		})
	}
}
