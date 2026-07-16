package presentation

import (
	"context"
	"testing"

	"github.com/looprig/core/uuid"
)

type runtimeCatalogFake struct{ *fakeAgent }

func (*runtimeCatalogFake) LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error) {
	return LoopRuntimeOptions{}, nil
}

func (*runtimeCatalogFake) AccessOptions(context.Context) (AccessOptions, error) {
	return AccessOptions{}, nil
}

type runtimeControllerFake struct{ *fakeAgent }

func (*runtimeControllerFake) SetMode(context.Context, uuid.UUID, ModeID) error     { return nil }
func (*runtimeControllerFake) SetModel(context.Context, uuid.UUID, ModelID) error   { return nil }
func (*runtimeControllerFake) SetEffort(context.Context, uuid.UUID, EffortID) error { return nil }
func (*runtimeControllerFake) SetAccess(context.Context, AccessID) error            { return nil }

type runtimeFullFake struct {
	*fakeAgent
	modeID ModeID
	loopID uuid.UUID
}

func (*runtimeFullFake) LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error) {
	return LoopRuntimeOptions{Modes: []ModeOption{{ID: "review", Label: "Review"}}}, nil
}
func (*runtimeFullFake) AccessOptions(context.Context) (AccessOptions, error) {
	return AccessOptions{Root: "/workspace", Choices: []AccessOption{{ID: "1", Label: "Read Only"}}}, nil
}
func (f *runtimeFullFake) SetMode(_ context.Context, loopID uuid.UUID, id ModeID) error {
	f.loopID, f.modeID = loopID, id
	return nil
}
func (*runtimeFullFake) SetModel(context.Context, uuid.UUID, ModelID) error   { return nil }
func (*runtimeFullFake) SetEffort(context.Context, uuid.UUID, EffortID) error { return nil }
func (*runtimeFullFake) SetAccess(context.Context, AccessID) error            { return nil }

func TestScreenDetectsOptionalRuntimeCapabilitiesWithoutExpandingAgent(t *testing.T) {
	t.Parallel()

	base := &fakeAgent{activeLoopID: callID(1)}
	plain := New(context.Background(), base, func(context.Context) (Agent, error) { return base, nil }, AgentBanner{})
	if plain.runtimeCatalog != nil || plain.runtimeController != nil {
		t.Fatalf("plain Agent capabilities = (%T, %T), want nil", plain.runtimeCatalog, plain.runtimeController)
	}

	catalogAgent := &runtimeCatalogFake{fakeAgent: base}
	withCatalog := New(context.Background(), catalogAgent, func(context.Context) (Agent, error) { return catalogAgent, nil }, AgentBanner{})
	if withCatalog.runtimeCatalog == nil {
		t.Fatal("RuntimeCatalog capability was not detected")
	}
	if withCatalog.runtimeController != nil {
		t.Fatalf("discovery-only Agent controller = %T, want nil", withCatalog.runtimeController)
	}

	controllerAgent := &runtimeControllerFake{fakeAgent: base}
	withController := New(context.Background(), controllerAgent, func(context.Context) (Agent, error) { return controllerAgent, nil }, AgentBanner{})
	if withController.runtimeCatalog != nil {
		t.Fatalf("mutation-only Agent catalog = %T, want nil", withController.runtimeCatalog)
	}
	if withController.runtimeController == nil {
		t.Fatal("RuntimeController capability was not detected")
	}
}

func TestRuntimeCommandsCaptureFocusedLoopAndReturnTypedChoice(t *testing.T) {
	loopID := callID(9)
	agent := &runtimeFullFake{fakeAgent: &fakeAgent{activeLoopID: loopID}}
	msg := queryRuntimeChoices(context.Background(), agent, runtimeTrayMode, loopID)().(runtimeChoicesMsg)
	if msg.loopID != loopID || len(msg.items) != 1 || msg.items[0].ID != "review" {
		t.Fatalf("runtime choices = %#v, want captured loop and opaque review id", msg)
	}
	result := mutateRuntime(context.Background(), agent, runtimeTrayMode, loopID, msg.items[0].ID)().(runtimeMutationMsg)
	if result.err != nil || agent.loopID != loopID || agent.modeID != "review" {
		t.Fatalf("mutation result=%#v target=%s id=%q", result, agent.loopID, agent.modeID)
	}
}

var _ Agent = (*fakeAgent)(nil)
var _ RuntimeCatalog = (*runtimeCatalogFake)(nil)
var _ RuntimeController = (*runtimeControllerFake)(nil)
var _ RuntimeCatalog = (*runtimeFullFake)(nil)
var _ RuntimeController = (*runtimeFullFake)(nil)
