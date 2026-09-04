package presentation

import (
	"sync"

	"github.com/looprig/tui/styles"
)

// Markdown rendering is the dominant cost of a transcript re-render. Glamour parses the
// document with goldmark and rebuilds its ANSI style tree on every call — measured at
// ~0.6ms and ~290KB per committed entry, against ~16us to construct the renderer itself,
// so the expense is the render, not the setup. renderFocused re-renders EVERY committed
// entry, and handleEvent calls it on every subscription event including each streamed
// token delta. On a long session that made one delta cost hundreds of milliseconds
// (measured: 456ms / 234MB at 800 entries), which saturated Bubble Tea's single,
// unbuffered event loop and left wheel events queued seconds behind the pointer.
//
// The cache is CONTENT-ADDRESSED on (markdown, width) rather than keyed on an entry's
// displayID, and that is deliberate: a committed entry is NOT immutable — a subagent
// card is reconciled in place after commit (transcript.updateReconciledSubagent), so an
// ID-keyed cache would serve a stale row. Keying on the markdown itself cannot go stale,
// because the key IS the input.
//
// Memoizing is sound only because styles.NewMarkdownRenderer is deterministic in
// (markdown, width): it uses the static DarkStyleConfig, never glamour.WithAutoStyle,
// and performs no terminal I/O, so it consults no ambient state that could change
// between two calls. Color downsampling to the terminal's profile happens later, in
// Bubble Tea's writer, never here. If NewMarkdownRenderer ever gains a dependency on
// ambient state, this cache must gain that state in its key.

// mdKey is the full set of inputs a rendered markdown document depends on.
type mdKey struct {
	md    string
	width int
}

// mdCache bounds are per generation, so live memory is at most twice these. The entry
// cap keeps a long transcript's committed documents resident (a session with more
// distinct documents than this simply re-renders the coldest ones), while the byte cap
// keeps a handful of very large documents from pinning that many entries' worth of memory.
const (
	mdCacheMaxEntries = 2048
	mdCacheMaxBytes   = 4 << 20
)

// mdCache is a bounded, content-addressed memo of rendered markdown documents.
//
// Eviction is two-generation rather than LRU: on overflow the current generation is
// demoted to old and a fresh one starts, and a hit in old is promoted back into current.
// That keeps the hot set (the committed entries redrawn on every event) resident with
// O(1) bookkeeping and no per-entry recency list. It also absorbs the two churn sources
// that would defeat a plain unbounded map — a streaming live segment, whose partial text
// is a distinct key per delta, and a terminal resize, which retires every key at the old
// width — without either being special-cased.
//
// It is guarded by a mutex. The Bubble Tea update loop renders on a single goroutine, so
// this is uncontended in production; the lock is what makes the shared cache safe for
// parallel tests and for any future off-loop rendering.
type mdCache struct {
	mu       sync.Mutex
	cur      map[mdKey][]string
	old      map[mdKey][]string
	curBytes int

	// hits and misses are observability for tests, which assert that a repeated render
	// is served from the memo rather than re-entering glamour.
	hits, misses uint64
}

// sharedMDCache is process-wide rather than per-Screen because Screen is copied by value
// on every Bubble Tea update; a cache field would be copied (and its map shared) on every
// message anyway, so the map may as well be owned in one place with one lock.
var sharedMDCache mdCache

// lookup returns the memoized lines for k, promoting a hit found in the old generation
// back into the current one. The returned slice is the cache's own and MUST NOT be
// mutated or handed to a caller directly; renderMarkdownDoc copies it.
func (c *mdCache) lookup(k mdKey) ([]string, bool) {
	if lines, ok := c.cur[k]; ok {
		c.hits++
		return lines, true
	}
	if lines, ok := c.old[k]; ok {
		// Promote: the document is still in the hot set, so it must survive the next
		// generation swap.
		c.store(k, lines)
		c.hits++
		return lines, true
	}
	c.misses++
	return nil, false
}

// store inserts lines under k, rotating generations first when the current one is full.
func (c *mdCache) store(k mdKey, lines []string) {
	if c.cur == nil {
		c.cur = make(map[mdKey][]string)
	}
	if len(c.cur) >= mdCacheMaxEntries || c.curBytes >= mdCacheMaxBytes {
		c.old = c.cur
		c.cur = make(map[mdKey][]string)
		c.curBytes = 0
	}
	c.cur[k] = lines
	c.curBytes += mdEntryBytes(k, lines)
}

// mdEntryBytes approximates one entry's retained size: the key's markdown plus every
// rendered line. It is an accounting estimate for the byte cap, not an exact heap
// measurement — slice headers and map overhead are ignored.
func mdEntryBytes(k mdKey, lines []string) int {
	n := len(k.md)
	for _, ln := range lines {
		n += len(ln)
	}
	return n
}

// renderMarkdownDoc renders md to its dedented ANSI lines at width, memoized on
// (md, width). ok is false when glamour fails to construct or render, leaving the
// caller to apply its own raw-text fallback (failures are NOT cached — they are
// construction/parse faults, not a property of the document worth remembering).
//
// The returned slice is a fresh copy on every call, including a cache hit: callers
// prefix each line in place (a bullet, a rail, an accent bar), which would otherwise
// corrupt the memoized document for every later reader.
func renderMarkdownDoc(md string, width int) ([]string, bool) {
	k := mdKey{md: md, width: width}

	sharedMDCache.mu.Lock()
	cached, ok := sharedMDCache.lookup(k)
	sharedMDCache.mu.Unlock()
	if ok {
		return append([]string(nil), cached...), true
	}

	r, err := styles.NewMarkdownRenderer(width)
	if err != nil {
		return nil, false
	}
	out, err := styles.RenderMarkdown(r, md, width)
	if err != nil {
		return nil, false
	}
	lines := dedentDocument(out)

	sharedMDCache.mu.Lock()
	sharedMDCache.store(k, lines)
	sharedMDCache.mu.Unlock()

	return append([]string(nil), lines...), true
}

// mdCacheStats reports the shared cache's hit/miss counters for tests.
func mdCacheStats() (hits, misses uint64) {
	sharedMDCache.mu.Lock()
	defer sharedMDCache.mu.Unlock()
	return sharedMDCache.hits, sharedMDCache.misses
}

// resetMDCache clears the shared cache and its counters so a test can measure a render
// from a known-cold state.
func resetMDCache() {
	sharedMDCache.mu.Lock()
	defer sharedMDCache.mu.Unlock()
	sharedMDCache.cur = nil
	sharedMDCache.old = nil
	sharedMDCache.curBytes = 0
	sharedMDCache.hits = 0
	sharedMDCache.misses = 0
}
