package presentation

import (
	"strconv"
	"strings"
	"testing"

	"github.com/looprig/core/content"
	"github.com/looprig/tui/styles"
)

const mdCacheSample = "A paragraph with `inline code`, a [link](https://example.com) and a list:\n\n- one\n- two\n\n```go\nfunc main() {}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n"

// renderMarkdownDoc must return byte-identical output to calling glamour directly, on a
// cold cache and on a hit. A memo that changes what the user sees is not a memo.
func TestRenderMarkdownDocMatchesDirectGlamourRender(t *testing.T) {
	resetMDCache()
	for _, width := range []int{40, 98} {
		r, err := styles.NewMarkdownRenderer(width)
		if err != nil {
			t.Fatalf("NewMarkdownRenderer(%d): %v", width, err)
		}
		out, err := styles.RenderMarkdown(r, mdCacheSample, width)
		if err != nil {
			t.Fatalf("RenderMarkdown(%d): %v", width, err)
		}
		want := dedentDocument(out)

		for attempt := range 2 {
			got, ok := renderMarkdownDoc(mdCacheSample, width)
			if !ok {
				t.Fatalf("width %d attempt %d: renderMarkdownDoc reported failure", width, attempt)
			}
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("width %d attempt %d: cached render differs from direct render\n got: %q\nwant: %q", width, attempt, got, want)
			}
		}
	}
}

// A repeated render of the same document at the same width must not re-enter glamour.
func TestRenderMarkdownDocServesRepeatFromCache(t *testing.T) {
	resetMDCache()
	if _, ok := renderMarkdownDoc(mdCacheSample, 98); !ok {
		t.Fatal("cold render failed")
	}
	_, missesAfterCold := mdCacheStats()
	if missesAfterCold != 1 {
		t.Fatalf("cold render: got %d misses, want 1", missesAfterCold)
	}

	if _, ok := renderMarkdownDoc(mdCacheSample, 98); !ok {
		t.Fatal("warm render failed")
	}
	hits, misses := mdCacheStats()
	if hits != 1 || misses != 1 {
		t.Errorf("warm render: got %d hits / %d misses, want 1 / 1", hits, misses)
	}
}

// Width is part of the key: the same document at a different width is a different render.
func TestRenderMarkdownDocKeysOnWidth(t *testing.T) {
	resetMDCache()
	narrow, ok := renderMarkdownDoc(mdCacheSample, 40)
	if !ok {
		t.Fatal("narrow render failed")
	}
	wide, ok := renderMarkdownDoc(mdCacheSample, 98)
	if !ok {
		t.Fatal("wide render failed")
	}
	if _, misses := mdCacheStats(); misses != 2 {
		t.Errorf("got %d misses, want 2 (width must be part of the key)", misses)
	}
	if strings.Join(narrow, "\n") == strings.Join(wide, "\n") {
		t.Error("narrow and wide renders are identical; width is not reaching glamour")
	}
}

// Callers prefix returned lines in place (bullet, rail, accent bar). The cache must hand
// out a copy, or the first caller's prefixes leak into every later reader's document.
func TestRenderMarkdownDocIsolatesCallerMutation(t *testing.T) {
	resetMDCache()
	first, ok := renderMarkdownDoc(mdCacheSample, 98)
	if !ok {
		t.Fatal("first render failed")
	}
	want := strings.Join(append([]string(nil), first...), "\n")
	for i := range first {
		first[i] = "MUTATED" + first[i]
	}

	second, ok := renderMarkdownDoc(mdCacheSample, 98)
	if !ok {
		t.Fatal("second render failed")
	}
	if got := strings.Join(second, "\n"); got != want {
		t.Errorf("caller mutation leaked into the cache\n got: %q\nwant: %q", got, want)
	}
}

// The cache is bounded: churn must not retain every document ever rendered.
func TestMDCacheEvictsOnOverflow(t *testing.T) {
	var c mdCache
	for i := range mdCacheMaxEntries * 2 {
		c.store(mdKey{md: strings.Repeat("x", i%7+1) + string(rune('a'+i%26)) + strconv.Itoa(i), width: 98}, []string{"line"})
	}
	if len(c.cur) > mdCacheMaxEntries {
		t.Errorf("current generation holds %d entries, want <= %d", len(c.cur), mdCacheMaxEntries)
	}
	if len(c.old) > mdCacheMaxEntries {
		t.Errorf("old generation holds %d entries, want <= %d", len(c.old), mdCacheMaxEntries)
	}
}

// A document evicted from the current generation but still in the old one is promoted
// back on the next hit, so the hot set survives repeated generation rotations.
func TestMDCachePromotesFromOldGeneration(t *testing.T) {
	var c mdCache
	hot := mdKey{md: "hot", width: 98}
	c.store(hot, []string{"hot lines"})
	for i := range mdCacheMaxEntries {
		c.store(mdKey{md: "filler" + strconv.Itoa(i), width: 98}, []string{"x"})
	}
	if _, ok := c.cur[hot]; ok {
		t.Fatal("test precondition: hot key was expected to rotate out of the current generation")
	}
	if _, ok := c.lookup(hot); !ok {
		t.Fatal("hot key lost; a document in the old generation must still be a hit")
	}
	if _, ok := c.cur[hot]; !ok {
		t.Error("hot key was not promoted back into the current generation")
	}
}

// The end-to-end guarantee this whole change exists for: re-rendering an unchanged
// transcript (what every streamed token delta does) must not re-enter glamour at all.
func TestRenderFocusedReusesCachedEntriesAcrossRerenders(t *testing.T) {
	resetMDCache()
	var s Screen
	s.width = 100
	s.collapse = newCollapseState()

	body := "Narration with `inline code`.\n\n- one\n- two\n"
	entries := make([]entry, 0, 30)
	for i := range 30 {
		id := displayID(i + 1)
		if i%2 == 0 {
			entries = append(entries, entry{ID: id, Kind: kindUser, Blocks: []content.Block{&content.TextBlock{Text: "request " + strconv.Itoa(i)}}})
			continue
		}
		entries = append(entries, entry{ID: id, Kind: kindAssistant, Blocks: []content.Block{&content.TextBlock{Text: body}}})
	}
	s.transcript = transcriptModel{global: entries, nextID: displayID(len(entries) + 1)}

	first := s.renderFocused()
	_, missesAfterFirst := mdCacheStats()
	if missesAfterFirst == 0 {
		t.Fatal("first render recorded no misses; the render path is not using the cache")
	}

	second := s.renderFocused()
	_, missesAfterSecond := mdCacheStats()
	if missesAfterSecond != missesAfterFirst {
		t.Errorf("re-render of an unchanged transcript re-entered glamour %d times, want 0", missesAfterSecond-missesAfterFirst)
	}

	if len(first) != len(second) {
		t.Fatalf("re-render changed line count: %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].styled != second[i].styled || first[i].plain != second[i].plain {
			t.Fatalf("re-render changed line %d:\n first: %q\nsecond: %q", i, first[i].styled, second[i].styled)
		}
	}
}
