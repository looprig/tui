package tui

// testCommitted exposes the single-loop backing slice used by most reducer unit tests.
// Multi-loop tests receive an aggregate snapshot and should use projectionFor when they
// need to assert loop ownership.
func (m transcriptModel) testCommitted() []entry {
	if len(m.global) == 0 && len(m.projections) == 1 {
		for _, p := range m.projections {
			return p.committed
		}
	}
	if len(m.projections) == 0 {
		return m.global
	}
	out := append([]entry(nil), m.global...)
	seen := make(map[string]bool, len(m.loopOrder))
	for _, id := range m.loopOrder {
		if p := m.projections[id]; p != nil {
			out = append(out, p.committed...)
			seen[id.String()] = true
		}
	}
	for id, p := range m.projections {
		if p != nil && !seen[id.String()] {
			out = append(out, p.committed...)
		}
	}
	return out
}

// testLive returns the sole loop's live segment for legacy-shaped reducer assertions.
func (m transcriptModel) testLive() *liveSeg {
	if len(m.projections) == 1 {
		for _, p := range m.projections {
			return &p.live
		}
	}
	panic("testLive requires a single loop; use projectionFor in multi-loop tests")
}
