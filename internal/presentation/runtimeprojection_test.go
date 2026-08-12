package presentation

import (
	"testing"
	"time"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/loop"
	contextcount "github.com/looprig/inference/contextcount"
	model "github.com/looprig/inference/model"
)

func TestRuntimeProjectionFoldsAuthoritativeLoopEvents(t *testing.T) {
	t.Parallel()

	loopID := callID(1)
	parentID := callID(2)
	initial := event.ModelRuntime{
		Key:    model.ModelKey{Provider: "provider", Model: "initial"},
		Limits: model.ContextLimits{WindowTokens: 128_000},
		Effort: model.EffortLow,
	}
	changed := event.ModelRuntime{
		Key:    model.ModelKey{Provider: "provider", Model: "changed"},
		Limits: model.ContextLimits{WindowTokens: 256_000},
		Effort: model.EffortHigh,
	}
	measurement := event.ContextMeasurement{
		InputTokens: content.TokenCount(42_000),
		InputLimit:  content.TokenCount(100_000),
		Quality:     contextcount.CountQualityHeuristicEstimate,
	}

	projection := newRuntimeProjection()
	projection = projection.ApplyEvent(event.LoopStarted{
		Header: event.Header{
			Coordinates: identity.Coordinates{LoopID: loopID},
			AgentName:   "operator",
			Cause:       identity.Cause{Coordinates: identity.Coordinates{LoopID: parentID}},
		},
		DisplayName:     "Operator",
		Description:     "Primary coding loop",
		ParentToolUseID: "toolu_1",
		InitialMode:     "plan",
		Runtime:         initial,
	})

	state, ok := projection.loop(loopID)
	if !ok {
		t.Fatal("LoopStarted did not create runtime state")
	}
	if state.agentName != "operator" || state.displayName != "Operator" || state.description != "Primary coding loop" {
		t.Fatalf("loop identity = %+v", state)
	}
	if state.parentLoopID != parentID || state.parentToolUseID != "toolu_1" || state.primer {
		t.Fatalf("loop provenance = %+v, want delegate of %v", state, parentID)
	}
	if state.mode != loop.ModeName("plan") || state.runtime != initial || !state.live {
		t.Fatalf("initial runtime = %+v, want mode plan runtime %+v live", state, initial)
	}

	projection = projection.ApplyEvent(event.LoopModeChanged{
		Header:  event.Header{Coordinates: identity.Coordinates{LoopID: loopID}},
		Mode:    "build",
		Runtime: changed,
	})
	projection = projection.ApplyEvent(event.ContextMeasured{
		Header:      event.Header{Coordinates: identity.Coordinates{LoopID: loopID}},
		Measurement: measurement,
	})
	projection = projection.ApplyEvent(event.LoopIdle{Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}})

	state, _ = projection.loop(loopID)
	if state.mode != loop.ModeName("build") || state.runtime != changed {
		t.Fatalf("changed runtime = %+v, want build %+v", state, changed)
	}
	if !state.hasContext || state.context != measurement {
		t.Fatalf("context = (%+v, %v), want (%+v, true)", state.context, state.hasContext, measurement)
	}
	if state.live {
		t.Fatal("LoopIdle left loop live")
	}

	projection = projection.ApplyEvent(event.LoopInferenceChanged{
		Header:  event.Header{Coordinates: identity.Coordinates{LoopID: loopID}},
		Runtime: initial,
	})
	state, _ = projection.loop(loopID)
	if state.mode != loop.ModeName("build") || state.runtime != initial {
		t.Fatalf("inference-only change = mode %q runtime %+v, want build and %+v", state.mode, state.runtime, initial)
	}
}

func TestRuntimeProjectionUsesCommittedPostCompactionContext(t *testing.T) {
	t.Parallel()

	compactedLoopID := callID(0x21)
	otherLoopID := callID(0x22)
	preContext := validContextMeasurement(content.TokenCount(80), content.TokenCount(100), 1, callID(0x23), 0x80)
	postContext := validContextMeasurement(content.TokenCount(20), content.TokenCount(100), 2, callID(0x24), 0x20)
	otherContext := validContextMeasurement(content.TokenCount(60), content.TokenCount(100), 1, callID(0x25), 0x60)

	projection := newRuntimeProjection()
	for _, loopID := range []uuid.UUID{compactedLoopID, otherLoopID} {
		projection = projection.ApplyEvent(event.LoopStarted{
			Header:  hdr(loopID),
			Runtime: event.ModelRuntime{Key: preContext.Model},
		})
	}
	projection = projection.ApplyEvent(event.ContextMeasured{
		Header:      hdr(compactedLoopID),
		Measurement: preContext,
	})
	projection = projection.ApplyEvent(event.ContextMeasured{
		Header:      hdr(otherLoopID),
		Measurement: otherContext,
	})
	projection = projection.ApplyEvent(event.CompactionCommitted{
		Header:           hdr(compactedLoopID),
		AttemptID:        event.CompactAttemptID(callID(0x26)),
		WaiterCommandIDs: []uuid.UUID{callID(0x27)},
		Reason:           event.CompactionReasonManual,
		Basis:            preContext.Basis,
		Summary:          userMsg("compacted"),
		PostContext:      postContext,
	})

	compacted, ok := projection.loop(compactedLoopID)
	if !ok {
		t.Fatal("compacted loop missing from runtime projection")
	}
	if !compacted.hasContext || compacted.context != postContext {
		t.Fatalf("compacted context = (%+v, %v), want (%+v, true)", compacted.context, compacted.hasContext, postContext)
	}

	other, ok := projection.loop(otherLoopID)
	if !ok {
		t.Fatal("other loop missing from runtime projection")
	}
	if !other.hasContext || other.context != otherContext {
		t.Fatalf("other loop context = (%+v, %v), want (%+v, true)", other.context, other.hasContext, otherContext)
	}
}

func TestRuntimeProjectionUsesCommittedPostCompactionContextWithoutPriorMeasurement(t *testing.T) {
	t.Parallel()

	loopID := callID(0x28)
	preBasis := event.ContextBasis{Revision: 1, ThroughEventID: callID(0x29)}
	postContext := validContextMeasurement(content.TokenCount(20), content.TokenCount(100), 2, callID(0x2a), 0x20)

	projection := newRuntimeProjection().ApplyEvent(event.LoopStarted{Header: hdr(loopID)})
	projection = projection.ApplyEvent(event.CompactionCommitted{
		Header:           hdr(loopID),
		AttemptID:        event.CompactAttemptID(callID(0x2b)),
		WaiterCommandIDs: []uuid.UUID{callID(0x2c)},
		Reason:           event.CompactionReasonManual,
		Basis:            preBasis,
		Summary:          userMsg("compacted"),
		PostContext:      postContext,
	})

	state, ok := projection.loop(loopID)
	if !ok {
		t.Fatal("loop missing from runtime projection")
	}
	if !state.hasContext || state.context != postContext {
		t.Fatalf("first context-bearing event produced (%+v, %v), want (%+v, true)", state.context, state.hasContext, postContext)
	}
}

func TestRuntimeProjectionAndStatusIgnoreCompactionRetained(t *testing.T) {
	t.Parallel()

	loopID := callID(0x2d)
	sessionID := callID(0x2e)
	preContext := validContextMeasurement(content.TokenCount(82), content.TokenCount(100), 1, callID(0x2f), 0x82)
	postContext := validContextMeasurement(content.TokenCount(17), content.TokenCount(100), 2, callID(0x30), 0x17)

	compactionHeader := hdr(loopID)
	compactionHeader.Coordinates.SessionID = sessionID
	compactionHeader.EventID = callID(0x31)
	compaction := event.CompactionCommitted{
		Header:           compactionHeader,
		AttemptID:        event.CompactAttemptID(callID(0x32)),
		WaiterCommandIDs: []uuid.UUID{callID(0x33)},
		Reason:           event.CompactionReasonManual,
		Basis:            preContext.Basis,
		Summary:          userMsg("retained summary"),
		Retained: content.AgenticMessages{
			userMsg("retained user turn"),
			aiMessage("thinking", "retained assistant", toolUse("retained-tool", "Bash", `{"command":"true"}`)),
			toolResult("retained-tool", "retained tool result"),
		},
		PostContext: postContext,
		Duration:    7 * time.Second,
	}
	if err := event.ValidateEvent(compaction); err != nil {
		t.Fatalf("retained compaction fixture is invalid: %v", err)
	}

	projection := newRuntimeProjection().ApplyEvent(event.LoopStarted{
		Header: event.Header{
			Coordinates: identity.Coordinates{SessionID: sessionID, LoopID: loopID},
			EventID:     callID(0x34),
		},
		Runtime: event.ModelRuntime{Key: postContext.Model},
	})
	projection = projection.ApplyEvent(compaction)
	state, ok := projection.loop(loopID)
	if !ok {
		t.Fatal("retained compaction omitted loop runtime state")
	}
	if !state.hasContext || state.context != postContext {
		t.Fatalf("runtime context = (%+v, %v), want exact PostContext %+v", state.context, state.hasContext, postContext)
	}

	// The status metadata is another consumer of the same runtime projection. Its
	// percentage must come from PostContext (17/100), not from the three retained
	// messages or their content.
	screen := newScreenSized(t, &fakeAgent{activeLoopID: loopID}, 100, 30)
	screen = feed(t, screen, event.LoopStarted{
		Header: event.Header{
			Coordinates: identity.Coordinates{SessionID: sessionID, LoopID: loopID},
			EventID:     callID(0x35),
		},
		Runtime: event.ModelRuntime{Key: postContext.Model},
	})
	screen = feed(t, screen, compaction)
	if got, want := screen.focusedRuntimeStatus(), "model · none · ~17% context"; got != want {
		t.Fatalf("focused runtime status = %q, want %q", got, want)
	}
}

func TestRuntimeProjectionIdentifiesPrimersAndIgnoresActiveSelection(t *testing.T) {
	t.Parallel()

	loopID := callID(3)
	projection := newRuntimeProjection().ApplyEvent(event.LoopStarted{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}, AgentName: "planner"},
	})
	before, ok := projection.loop(loopID)
	if !ok || !before.primer {
		t.Fatalf("root LoopStarted state = %+v, want primer", before)
	}

	projection = projection.ApplyEvent(event.ActiveLoopChanged{PreviousLoopID: loopID, ActiveLoopID: callID(4)})
	after, ok := projection.loop(loopID)
	if !ok || after != before {
		t.Fatalf("ActiveLoopChanged mutated runtime projection: before=%+v after=%+v", before, after)
	}
}

func TestFoldDisplayAndLiveScreenUseTheSameRuntimeProjection(t *testing.T) {
	t.Parallel()

	loopID := callID(5)
	started := event.LoopStarted{
		Header:      event.Header{Coordinates: identity.Coordinates{LoopID: loopID}, AgentName: "operator"},
		InitialMode: "plan",
		Runtime: event.ModelRuntime{
			Key:    model.ModelKey{Provider: "provider", Model: "model"},
			Effort: model.EffortMedium,
		},
	}

	folded := FoldDisplay([]event.Event{started})
	foldedState, ok := folded.runtime.loop(loopID)
	if !ok {
		t.Fatal("FoldDisplay omitted runtime projection")
	}

	agent := &fakeAgent{activeLoopID: loopID}
	screen := newScreenSized(t, agent, 100, 30)
	screen = feed(t, screen, started)
	liveState, ok := screen.runtime.loop(loopID)
	if !ok {
		t.Fatal("live Screen omitted runtime projection")
	}
	if liveState != foldedState {
		t.Fatalf("live runtime = %+v, folded runtime = %+v", liveState, foldedState)
	}
}
