package presentation

import (
	"testing"

	"github.com/looprig/core/content"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/loop"
	"github.com/looprig/harness/pkg/security"
	contextcount "github.com/looprig/inference/contextcount"
	model "github.com/looprig/inference/model"
)

func TestRuntimeProjectionFoldsAuthoritativeLoopAndAccessEvents(t *testing.T) {
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
	projection = projection.ApplyEvent(event.SecurityLimitChanged{Level: security.Level(3)})
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
	if !projection.hasAccess || projection.access != security.Level(3) {
		t.Fatalf("access = (%v, %v), want (3, true)", projection.access, projection.hasAccess)
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
