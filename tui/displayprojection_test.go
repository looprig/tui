package tui

import (
	"testing"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/tool"
)

// TestFoldDisplay covers the exported displayed-projection fold: it folds a primary
// loop's Enduring events through the SAME reducers the live/restore paths use, and
// the resulting projection equals the one built by the internal foldBacklog helper
// (so the public seam and the production fold are the SAME fold). It also covers the
// headline-property comparators EqualTranscript / PendingPrompts that the persistence
// integration tests assert through.
func TestFoldDisplay(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)
	hdr := event.Header{Coordinates: identity.Coordinates{LoopID: primary}}
	user := func(text string) *content.UserMessage {
		return &content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: []content.Block{&content.TextBlock{Text: text}}}}
	}

	cleanTurn := []event.Event{
		event.TurnStarted{Header: hdr, Message: user("first question")},
		event.StepDone{Header: hdr, Messages: content.AgenticMessages{aiMessage("", "first answer")}},
		event.TurnDone{Header: hdr, Message: aiMessage("", "first answer")},
	}

	tests := []struct {
		name             string
		events           []event.Event
		wantCommittedLen int
		wantPending      int
	}{
		{name: "empty fold has no committed entries", events: nil, wantCommittedLen: 0, wantPending: 0},
		{
			name:             "clean single turn commits user + assistant",
			events:           cleanTurn,
			wantCommittedLen: 2,
			wantPending:      0,
		},
		{
			name: "pending permission gate counts as a pending prompt",
			events: []event.Event{
				event.TurnStarted{Header: hdr, Message: user("q")},
				event.PermissionRequested{Header: hdr, ToolExecutionID: callID(7), Request: tool.BashRequest{Command: "ls"}},
			},
			wantCommittedLen: 1, // the user row; the gate surfaces on the (uncommitted) tool card, not a record
			wantPending:      1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := FoldDisplay(tt.events)
			if got.CommittedLen() != tt.wantCommittedLen {
				t.Errorf("CommittedLen() = %d, want %d", got.CommittedLen(), tt.wantCommittedLen)
			}
			if got.PendingPrompts() != tt.wantPending {
				t.Errorf("PendingPrompts() = %d, want %d", got.PendingPrompts(), tt.wantPending)
			}

			// The exported fold IS the production fold: it must equal the internal
			// foldBacklog helper's transcript exactly.
			wantTr, _ := foldBacklog(primary, tt.events)
			if !got.equalTranscriptModel(wantTr) {
				t.Errorf("FoldDisplay transcript != foldBacklog transcript")
			}
		})
	}

	// EqualTranscript is reflexive across two independent folds of the SAME events
	// from the SAME zero state — the deep-equality the displayed==restored property
	// relies on.
	a := FoldDisplay(cleanTurn)
	b := FoldDisplay(cleanTurn)
	if !a.EqualTranscript(b) {
		t.Error("EqualTranscript on two folds of identical events = false, want true")
	}

}

func TestDisplayProjectionCommittedLenSurvivesSessionEventTail(t *testing.T) {
	t.Parallel()
	loopID := callID(0xA1)
	events := []event.Event{
		event.TurnStarted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}, Message: &content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: []content.Block{&content.TextBlock{Text: "question"}}}}},
		event.StepDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}, Messages: content.AgenticMessages{aiMessage("", "answer")}},
		event.SessionIdle{},
	}
	if got := FoldDisplay(events).CommittedLen(); got != 2 {
		t.Fatalf("CommittedLen after session-scoped tail = %d, want 2", got)
	}
}

func TestDisplayProjectionTracksLifecycleOnlyHistoryPresence(t *testing.T) {
	loopID := callID(0x44)
	events := []event.Event{
		event.LoopStarted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}, AgentName: "operator"}, DisplayName: "Operator Primer"},
		event.LoopIdle{Header: hdr(loopID)},
	}
	projection := FoldDisplay(events)
	if got := projection.EventCount(); got != 2 {
		t.Fatalf("EventCount() = %d, want 2", got)
	}
	if got := projection.CommittedLen(); got != 0 {
		t.Fatalf("CommittedLen() = %d, want 0", got)
	}
	if got := FoldDisplay(nil).EventCount(); got != 0 {
		t.Fatalf("empty EventCount() = %d, want 0", got)
	}
}

func TestFoldDisplayRetainsCompactionTerminalTombstones(t *testing.T) {
	t.Parallel()

	loopID := callID(0xC1)
	attemptID := event.CompactAttemptID(callID(0xC2))
	tests := []struct {
		name   string
		events []event.Event
	}{
		{name: "committed terminal", events: []event.Event{event.CompactionCommitted{Header: hdr(loopID), AttemptID: attemptID}}},
		{name: "rejected terminal", events: []event.Event{event.CompactionRejected{Header: hdr(loopID), AttemptID: attemptID}}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			projection := FoldDisplay(tt.events)
			if !projection.compaction.isTerminal(loopID, attemptID) {
				t.Fatal("FoldDisplay did not retain terminal attempt tombstone")
			}
		})
	}
}

// equalTranscriptModel is a same-package test bridge: it deep-compares a
// DisplayProjection's (unexported) transcript model against an internally-built
// transcriptModel, so TestFoldDisplay can assert the exported fold equals the
// internal foldBacklog fold without leaking the unexported type across packages.
func (p DisplayProjection) equalTranscriptModel(tr transcriptModel) bool {
	return p.EqualTranscript(DisplayProjection{transcript: tr})
}

var _ = uuid.UUID{}
