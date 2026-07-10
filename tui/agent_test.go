package tui

import (
	"testing"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
)

// TestDefaultEventFilter locks the single-loop TUI default filter shape: live
// Ephemeral tokens from the primary loop ONLY, and Enduring events from EVERY loop,
// with session-scoped events always passing. It drives a table through the shared
// event.ShouldDeliver predicate so the assertion is the real fan-out decision, not a
// re-derivation of it.
func TestDefaultEventFilter(t *testing.T) {
	t.Parallel()

	primary := loopID(1)
	subagent := loopID(2)
	filter := DefaultEventFilter(primary)

	tests := []struct {
		name string
		ev   event.Event
		want bool
	}{
		{
			name: "primary loop TokenDelta delivers (live tokens from the watched loop)",
			ev:   event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}}},
			want: true,
		},
		{
			name: "subagent TokenDelta is filtered out (its firehose never enters egress)",
			ev:   event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subagent}}},
			want: false,
		},
		{
			name: "primary loop StepDone delivers (finalized group)",
			ev:   event.StepDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}}},
			want: true,
		},
		{
			name: "subagent StepDone delivers (all-loop Enduring: collapsed-but-present)",
			ev:   event.StepDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subagent}}},
			want: true,
		},
		{
			name: "subagent TurnDone terminal delivers (all-loop Enduring)",
			ev:   event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subagent}}},
			want: true,
		},
		{
			name: "session-scoped SessionIdle always delivers (bypasses the loop filter)",
			ev:   event.SessionIdle{Header: event.Header{Coordinates: identity.Coordinates{SessionID: loopID(9)}}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := event.ShouldDeliver(filter, tt.ev); got != tt.want {
				t.Errorf("ShouldDeliver(default, %T) = %v, want %v", tt.ev, got, tt.want)
			}
		})
	}
}

// TestDefaultEventFilterShape locks the literal STRUCT shape returned by
// DefaultEventFilter (distinct from TestDefaultEventFilter, which asserts the
// resulting delivery decisions through ShouldDeliver): Ephemeral scopes to the
// primary loop ONLY (not All, exactly one member — the primary id), and Enduring is
// All (every loop). It inspects the returned struct directly so a regression that,
// say, flips Ephemeral.All on or widens the Ephemeral.Loops set is caught at the
// source, not only through behavior.
func TestDefaultEventFilterShape(t *testing.T) {
	t.Parallel()

	primary := loopID(1)
	filter := DefaultEventFilter(primary)

	if filter.Ephemeral.All {
		t.Error("Ephemeral.All = true, want false (Ephemeral must be primary-only, not every loop)")
	}
	if len(filter.Ephemeral.Loops) != 1 {
		t.Errorf("Ephemeral.Loops size = %d, want 1 (only the primary loop)", len(filter.Ephemeral.Loops))
	}
	if _, ok := filter.Ephemeral.Loops[primary]; !ok {
		t.Errorf("Ephemeral.Loops missing the primary loop %v", primary)
	}
	if !filter.Enduring.All {
		t.Error("Enduring.All = false, want true (Enduring must deliver from every loop)")
	}
}

// TestAllLoopsEventFilter locks the MODERN TUI's filter shape: unlike DefaultEventFilter
// (primary-only Ephemeral), AllLoopsEventFilter's Ephemeral scope matches EVERY loop, so
// a subagent loop's live token/tool firehose reaches the subscriber and its per-loop
// projection is not starved; Enduring stays all-loops as in the default. It asserts the
// Ephemeral scope directly (All plus Matches over a random NON-primary loop id) and
// cross-checks that DefaultEventFilter does NOT match that id — pinning the load-bearing
// difference at the source, not only through behavior.
func TestAllLoopsEventFilter(t *testing.T) {
	t.Parallel()

	primary := loopID(1)
	other := loopID(7) // an arbitrary NON-primary (subagent) loop id

	all := AllLoopsEventFilter()
	def := DefaultEventFilter(primary)

	if !all.Ephemeral.All {
		t.Error("AllLoopsEventFilter Ephemeral.All = false, want true (every loop's live stream)")
	}
	if !all.Ephemeral.Matches(other) {
		t.Error("AllLoopsEventFilter Ephemeral.Matches(non-primary) = false, want true (modern mode receives every loop's live stream)")
	}
	if !all.Ephemeral.Matches(primary) {
		t.Error("AllLoopsEventFilter Ephemeral.Matches(primary) = false, want true")
	}
	if !all.Enduring.All {
		t.Error("AllLoopsEventFilter Enduring.All = false, want true (Enduring stays all-loops)")
	}
	// The load-bearing contrast (design §Subscription): the primary-only default does NOT
	// match a non-primary loop's Ephemeral events — the very starvation modern mode fixes.
	if def.Ephemeral.Matches(other) {
		t.Error("DefaultEventFilter Ephemeral.Matches(non-primary) = true, want false (default is primary-only)")
	}
}

// loopID builds a deterministic non-zero loop uuid from one byte (callID is defined
// in screen_test.go with the same shape; this name documents the loop-id intent).
func loopID(b byte) uuid.UUID {
	var u uuid.UUID
	u[0] = b
	return u
}
