package tui

import (
	"testing"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
)

// TestAllLoopsEventFilter locks the repository's all-loop delivery contract: the single
// session subscription delivers EVERY loop's live Ephemeral stream (active AND any other
// loop), every loop's Enduring events, AND the session-scoped selection transition
// (ActiveLoopChanged). It drives a table through the shared event.ShouldDeliver predicate
// so the assertion is the real fan-out decision, not a re-derivation of it. There is no
// active-only path any more — modern mode renders any focused loop's whole live output,
// so it must actually RECEIVE every loop's firehose.
func TestAllLoopsEventFilter(t *testing.T) {
	t.Parallel()
	active, other, sessionID := loopID(1), loopID(2), loopID(9)
	filter := AllLoopsEventFilter()
	// Pin the struct shape at the source: BOTH classes deliver from every loop. This is the
	// load-bearing invariant the behavioral rows below rely on, asserted directly so a
	// regression that narrows either scope is caught even if the rows drift.
	if !filter.Ephemeral.All {
		t.Error("Ephemeral.All = false, want true (every loop's live stream)")
	}
	if !filter.Enduring.All {
		t.Error("Enduring.All = false, want true (every loop's enduring events)")
	}
	tests := []struct {
		name string
		ev   event.Event
		want bool
	}{
		{name: "active ephemeral", ev: event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: active}}}, want: true},
		{name: "other ephemeral", ev: event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: other}}}, want: true},
		{name: "other enduring", ev: event.StepDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: other}}}, want: true},
		{name: "selection", ev: event.ActiveLoopChanged{Header: event.Header{Coordinates: identity.Coordinates{SessionID: sessionID}}, ActiveLoopID: other}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := event.ShouldDeliver(filter, tt.ev); got != tt.want {
				t.Errorf("ShouldDeliver(%T) = %v, want %v", tt.ev, got, tt.want)
			}
		})
	}
}

// loopID builds a deterministic non-zero loop uuid from one byte (callID is defined
// in screen_test.go with the same shape; this name documents the loop-id intent).
func loopID(b byte) uuid.UUID {
	var u uuid.UUID
	u[0] = b
	return u
}
