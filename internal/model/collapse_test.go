package model

import "testing"

// TestCollapseStateDefault locks the initial contract: a FRESH state renders every
// entry COLLAPSED (globalCollapsed true), independent of the entry id. The zero id, the
// first real id, and the numeric extremes all fold by default — thinking starts folded.
func TestCollapseStateDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   DisplayID
	}{
		{name: "zero id", id: 0},
		{name: "first id", id: 1},
		{name: "large id", id: DisplayID(1 << 40)},
		{name: "max id", id: DisplayID(^uint64(0))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCollapseState()
			if !c.Effective(tt.id) {
				t.Errorf("Effective(%d) = false, want true (fresh state is collapsed)", tt.id)
			}
		})
	}
}

// TestCollapseStateToggleAll locks the global toggle: each ToggleAll flips the default
// applied to an un-overridden entry, and two calls return to the collapsed start.
func TestCollapseStateToggleAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		toggles int  // number of ToggleAll calls from a fresh (collapsed) state
		want    bool // Effective for an entry with no per-entry override
	}{
		{name: "zero toggles collapsed", toggles: 0, want: true},
		{name: "one toggle expands", toggles: 1, want: false},
		{name: "two toggles restore collapsed", toggles: 2, want: true},
		{name: "three toggles expand", toggles: 3, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCollapseState()
			for i := 0; i < tt.toggles; i++ {
				c.ToggleAll()
			}
			if got := c.Effective(DisplayID(7)); got != tt.want {
				t.Errorf("Effective after %d ToggleAll = %v, want %v", tt.toggles, got, tt.want)
			}
		})
	}
}

// TestCollapseStateToggleAllClearsOverrides locks the RETROACTIVE contract: ToggleAll
// discards every per-entry override, so an entry previously toggled open re-follows the
// new global. The distinguishing witness: after one Toggle (open) an entry differs from
// global; TWO subsequent ToggleAlls (global true->false->true) must leave the entry at
// the global's value (true) — which is only possible if the override was cleared. A
// retained override would still read the toggled-open value (false).
func TestCollapseStateToggleAllClearsOverrides(t *testing.T) {
	t.Parallel()

	c := NewCollapseState()
	const id = DisplayID(3)

	c.Toggle(id) // override[id] = false (expanded), differing from global (true)
	if c.Effective(id) {
		t.Fatalf("Effective(id) after Toggle = true, want false (toggled open)")
	}

	c.ToggleAll() // global -> false, overrides cleared
	if len(c.overrides) != 0 {
		t.Fatalf("overrides not cleared by ToggleAll: len = %d, want 0", len(c.overrides))
	}
	c.ToggleAll() // global -> true, overrides still cleared

	if !c.Effective(id) {
		t.Errorf("Effective(id) after two ToggleAll = false, want true (override cleared, follows global)")
	}
	if !c.Effective(DisplayID(99)) {
		t.Errorf("Effective(other) after two ToggleAll = false, want true (follows global)")
	}
}

// TestCollapseStateToggle locks the per-entry toggle: from the collapsed default one
// Toggle expands the entry, a second re-collapses it, a third expands again — each
// Toggle flips the entry relative to its current effective state.
func TestCollapseStateToggle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		toggles int
		want    bool
	}{
		{name: "one toggle expands", toggles: 1, want: false},
		{name: "two toggles re-collapse", toggles: 2, want: true},
		{name: "three toggles expand", toggles: 3, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCollapseState()
			const id = DisplayID(5)
			for i := 0; i < tt.toggles; i++ {
				c.Toggle(id)
			}
			if got := c.Effective(id); got != tt.want {
				t.Errorf("Effective(id) after %d Toggle = %v, want %v", tt.toggles, got, tt.want)
			}
		})
	}
}

// TestCollapseStateToggleIsolatesEntry locks that Toggle touches ONLY its entry: the
// toggled entry flips while a sibling with no override keeps following the global.
func TestCollapseStateToggleIsolatesEntry(t *testing.T) {
	t.Parallel()

	c := NewCollapseState()
	const target = DisplayID(2)
	const other = DisplayID(8)

	c.Toggle(target)
	if c.Effective(target) {
		t.Errorf("Effective(target) = true, want false (toggled open)")
	}
	if !c.Effective(other) {
		t.Errorf("Effective(other) = false, want true (unaffected, follows global)")
	}
}

// TestCollapseStateEffectivePrecedence locks override > global in BOTH directions: with
// the global collapsed, a toggled entry reads expanded; with the global expanded (after
// ToggleAll clears overrides), a freshly toggled entry reads collapsed. An
// un-overridden entry always reads the global.
func TestCollapseStateEffectivePrecedence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		expandGlobal   bool // ToggleAll first, flipping the global to expanded
		wantGlobalOnly bool // Effective for an un-overridden entry
		wantOverridden bool // Effective for the toggled entry (opposite of global)
	}{
		{name: "global collapsed, override expands", expandGlobal: false, wantGlobalOnly: true, wantOverridden: false},
		{name: "global expanded, override collapses", expandGlobal: true, wantGlobalOnly: false, wantOverridden: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewCollapseState()
			if tt.expandGlobal {
				c.ToggleAll() // global -> false (expanded); clears overrides
			}
			const overridden = DisplayID(4)
			const plain = DisplayID(9)
			c.Toggle(overridden) // override = !global
			if got := c.Effective(plain); got != tt.wantGlobalOnly {
				t.Errorf("Effective(plain) = %v, want %v (global)", got, tt.wantGlobalOnly)
			}
			if got := c.Effective(overridden); got != tt.wantOverridden {
				t.Errorf("Effective(overridden) = %v, want %v (override)", got, tt.wantOverridden)
			}
		})
	}
}

// TestCollapseStateCloneOnWrite is the value-semantics guard: two CollapseState VALUE
// copies share the same overrides backing map, yet mutating one (Toggle or ToggleAll)
// must never be observable through the other. This mirrors the recordLoopLive/loopLive
// clone-on-write contract — a state copied into a model copy owns its own map.
func TestCollapseStateCloneOnWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*CollapseState)
	}{
		{name: "Toggle does not alias sibling", mutate: func(c *CollapseState) { c.Toggle(DisplayID(1)) }},
		{name: "ToggleAll does not alias sibling", mutate: func(c *CollapseState) { c.ToggleAll() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Seed an override so both copies share a non-empty overrides map.
			base := NewCollapseState()
			base.Toggle(DisplayID(7)) // overrides = {7: false}

			a := base // two value copies aliasing the same overrides map
			b := base
			wantGlobalB := b.globalCollapsed
			want7B := b.Effective(DisplayID(7))

			tt.mutate(&a)

			if b.globalCollapsed != wantGlobalB {
				t.Errorf("b.globalCollapsed mutated by a's mutation: got %v, want %v", b.globalCollapsed, wantGlobalB)
			}
			if got := b.Effective(DisplayID(7)); got != want7B {
				t.Errorf("b.Effective(7) mutated by a's mutation: got %v, want %v", got, want7B)
			}
			// Entry 1 is toggled only in a (Toggle case); b must never see that override.
			if got := b.Effective(DisplayID(1)); got != b.globalCollapsed {
				t.Errorf("b.Effective(1) leaked a's Toggle: got %v, want %v (global)", got, b.globalCollapsed)
			}
		})
	}
}
