package model

import (
	"reflect"
	"testing"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
)

func callID(b byte) uuid.UUID {
	var id uuid.UUID
	id[0] = b
	return id
}

func hdr(loopID uuid.UUID) event.Header {
	return event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}
}

func TestCompactionProjectionApplyEvent(t *testing.T) {
	t.Parallel()

	loopA := callID(0xA1)
	loopB := callID(0xB1)
	attemptA := event.CompactAttemptID(callID(0xA2))
	attemptB := event.CompactAttemptID(callID(0xB2))
	attemptOther := event.CompactAttemptID(callID(0xC2))

	start := func(loopID uuid.UUID, attemptID event.CompactAttemptID) event.Event {
		return event.CompactionStarted{Header: hdr(loopID), AttemptID: attemptID}
	}
	commit := func(loopID uuid.UUID, attemptID event.CompactAttemptID) event.Event {
		return event.CompactionCommitted{Header: hdr(loopID), AttemptID: attemptID}
	}
	reject := func(loopID uuid.UUID, attemptID event.CompactAttemptID) event.Event {
		return event.CompactionRejected{Header: hdr(loopID), AttemptID: attemptID}
	}
	fold := func(events ...event.Event) CompactionProjection {
		projection := CompactionProjection{}
		for _, ev := range events {
			projection = projection.ApplyEvent(ev)
		}
		return projection
	}

	tests := []struct {
		name        string
		events      []event.Event
		wantActiveA bool
		wantActiveB bool
		wantTermA   bool
		wantTermB   bool
	}{
		{name: "empty projection is inactive", events: nil},
		{
			name:        "independent loops remain active",
			events:      []event.Event{start(loopA, attemptA), start(loopB, attemptB)},
			wantActiveA: true,
			wantActiveB: true,
		},
		{
			name:        "mismatched terminal does not clear active attempt",
			events:      []event.Event{start(loopA, attemptA), commit(loopA, attemptOther)},
			wantActiveA: true,
		},
		{
			name:        "matching commit clears only its loop",
			events:      []event.Event{start(loopA, attemptA), start(loopB, attemptB), commit(loopA, attemptA)},
			wantTermA:   true,
			wantActiveB: true,
		},
		{
			name:      "matching rejection clears active attempt",
			events:    []event.Event{start(loopA, attemptA), reject(loopA, attemptA)},
			wantTermA: true,
		},
		{
			name:      "terminal before start tombstones stale start",
			events:    []event.Event{commit(loopA, attemptA), start(loopA, attemptA)},
			wantTermA: true,
		},
		{
			name:      "duplicate start and terminal are idempotent",
			events:    []event.Event{start(loopA, attemptA), start(loopA, attemptA), commit(loopA, attemptA), commit(loopA, attemptA)},
			wantTermA: true,
		},
		{
			name:        "terminal tombstones are scoped by loop",
			events:      []event.Event{commit(loopA, attemptA), start(loopB, attemptA)},
			wantActiveB: true,
			wantTermA:   true,
		},
		{
			name:      "rejected terminal tombstones stale start",
			events:    []event.Event{reject(loopB, attemptB), start(loopB, attemptB)},
			wantTermB: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := fold(tt.events...)
			if active := got.IsActive(loopA); active != tt.wantActiveA {
				t.Errorf("IsActive(loop A) = %v, want %v", active, tt.wantActiveA)
			}
			if active := got.IsActive(loopB); active != tt.wantActiveB {
				t.Errorf("IsActive(loop B) = %v, want %v", active, tt.wantActiveB)
			}
			if terminal := got.IsTerminal(loopA, attemptA); terminal != tt.wantTermA {
				t.Errorf("isTerminal(loop A, attempt A) = %v, want %v", terminal, tt.wantTermA)
			}
			if terminal := got.IsTerminal(loopB, attemptB); terminal != tt.wantTermB {
				t.Errorf("isTerminal(loop B, attempt B) = %v, want %v", terminal, tt.wantTermB)
			}
		})
	}
}

func TestCompactionProjectionDoesNotMutatePriorValue(t *testing.T) {
	t.Parallel()

	loopID := callID(0xD1)
	attemptID := event.CompactAttemptID(callID(0xD2))
	started := CompactionProjection{}.ApplyEvent(event.CompactionStarted{Header: hdr(loopID), AttemptID: attemptID})
	before := started

	tests := []struct {
		name string
		ev   event.Event
	}{
		{name: "later terminal clones active map", ev: event.CompactionCommitted{Header: hdr(loopID), AttemptID: attemptID}},
		{name: "later loop start clones active map", ev: event.CompactionStarted{Header: hdr(callID(0xE1)), AttemptID: event.CompactAttemptID(callID(0xE2))}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_ = started.ApplyEvent(tt.ev)
			if !reflect.DeepEqual(started, before) {
				t.Fatalf("ApplyEvent mutated prior projection: got %+v, want %+v", started, before)
			}
			if !started.IsActive(loopID) {
				t.Fatal("prior projection lost its active attempt")
			}
		})
	}
}
