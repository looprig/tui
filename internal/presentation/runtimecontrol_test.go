package presentation

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/looprig/core/uuid"
)

// TestSessionPresentationFooterShowsFixedProfileAndWorkspace covers the synchronous,
// consumer-supplied session metadata in the footer: the workspace root followed by the
// fixed access profile badge. Product branding belongs in the startup banner, not the
// persistent footer.
func TestSessionPresentationFooterShowsFixedProfileAndWorkspace(t *testing.T) {
	t.Parallel()

	base := &fakeAgent{activeLoopID: callID(1)}
	m := New(context.Background(), base, func(context.Context) (Agent, error) { return base, nil },
		AgentBanner{Name: "Carbon"},
		WithSessionPresentation(SessionPresentation{ProfileName: "Writable", WorkspaceRoot: "/workspace"}))
	m.width = 80

	footer := stripANSI(m.footerView())
	if got, want := strings.Split(footer, "\n")[0], "/workspace [WRITABLE]"; got != want {
		t.Errorf("footer header = %q, want %q", got, want)
	}
}

// TestSessionPresentationOmitsEmptyMetadata covers the zero presentation: with no
// profile or workspace the footer shows only the agent name (no stray separators).
func TestSessionPresentationOmitsEmptyMetadata(t *testing.T) {
	t.Parallel()

	base := &fakeAgent{activeLoopID: callID(1)}
	m := New(context.Background(), base, func(context.Context) (Agent, error) { return base, nil }, AgentBanner{Name: "Carbon"})
	m.width = 80

	footer := stripANSI(m.footerView())
	if strings.Contains(footer, "·") {
		t.Errorf("footer = %q, want no metadata separators when no presentation is supplied", footer)
	}
}

// TestSessionPresentationUsesAgentSessionPresenterAtConstruction pins the fix for the
// blank-footer-on-first-launch bug: New's ONLY route to a presentation used to be the
// (never-supplied-by-any-real-caller) WithSessionPresentation option, so the footer's
// profile/workspace metadata was blank until the first /clear reopen, which is the only
// path that ever type-asserted the agent as a SessionPresenter. New must perform the SAME
// type assertion at construction, with NO reopen involved, so the very first frame already
// carries the constructed agent's own session presentation.
func TestSessionPresentationUsesAgentSessionPresenterAtConstruction(t *testing.T) {
	t.Parallel()

	agent := presenterAgent{
		fakeAgent: &fakeAgent{activeLoopID: callID(1)},
		pres: SessionPresentation{
			ProfileName:           "ReadOnly",
			WorkspaceRoot:         "/workspace",
			PermissionDiagnostics: []string{"initial diag"},
		},
	}
	m := New(context.Background(), agent, func(context.Context) (Agent, error) { return agent, nil }, AgentBanner{Name: "Carbon"})
	m.width = 80

	if !reflect.DeepEqual(m.presentation, agent.pres) {
		t.Fatalf("presentation = %+v, want the constructed agent's SessionPresentation() %+v", m.presentation, agent.pres)
	}
	footer := stripANSI(m.footerView())
	if !strings.Contains(footer, "/workspace [READONLY]") {
		t.Errorf("footer = %q, want the agent's own fixed profile + workspace at construction, no reopen involved", footer)
	}
}

// TestSessionPresentationOptionOverridesAgentSessionPresenterAtConstruction covers the
// explicit-option precedence: WithSessionPresentation is the consumer-supplied
// construction-time override (per its doc comment); when a caller supplies it, it wins over
// the agent's own SessionPresenter capability even if the agent implements one.
func TestSessionPresentationOptionOverridesAgentSessionPresenterAtConstruction(t *testing.T) {
	t.Parallel()

	agent := presenterAgent{
		fakeAgent: &fakeAgent{activeLoopID: callID(1)},
		pres:      SessionPresentation{ProfileName: "FromAgent", WorkspaceRoot: "/from-agent"},
	}
	option := SessionPresentation{ProfileName: "FromOption", WorkspaceRoot: "/from-option"}
	m := New(context.Background(), agent, func(context.Context) (Agent, error) { return agent, nil },
		AgentBanner{Name: "Carbon"}, WithSessionPresentation(option))
	m.width = 80

	if !reflect.DeepEqual(m.presentation, option) {
		t.Fatalf("presentation = %+v, want the explicit WithSessionPresentation option %+v", m.presentation, option)
	}
	footer := stripANSI(m.footerView())
	if !strings.Contains(footer, "[FROMOPTION]") || strings.Contains(footer, "[FROMAGENT]") {
		t.Errorf("footer = %q, want the explicit option to win over the agent's SessionPresenter", footer)
	}
}

// TestPermissionDiagnosticsRenderBeforeFirstGate covers the diagnostics-surfacing
// requirement: manual out-of-catalog allow-family diagnostics are committed in the
// startup metadata area at the opening banner (systemReadyMsg), so they are visible
// BEFORE any permission gate could arrive. The banner commits FIRST, then the
// diagnostics, in order.
func TestPermissionDiagnosticsRenderBeforeFirstGate(t *testing.T) {
	t.Parallel()

	diagnostics := []string{"allow family 'net.*' is not in the catalogue", "allow family 'exec.*' is manual"}
	base := &fakeAgent{activeLoopID: callID(1)}
	m := New(context.Background(), base, func(context.Context) (Agent, error) { return base, nil },
		AgentBanner{Name: "Carbon"},
		WithSessionPresentation(SessionPresentation{PermissionDiagnostics: diagnostics}))
	m.width, m.height, m.ready = 80, 24, true
	m, _ = updateScreen(t, m, systemReadyMsg{}) // commit the opening metadata block

	committed := m.transcript.testCommitted()
	if len(committed) < 3 {
		t.Fatalf("committed startup entries = %d, want banner + 2 diagnostics", len(committed))
	}
	if banner := committedText(committed[0]); !strings.Contains(banner, "Carbon") {
		t.Errorf("committed[0] = %q, want the identity/session banner first", banner)
	}
	for i, want := range diagnostics {
		if got := committedText(committed[1+i]); !strings.Contains(got, want) {
			t.Errorf("committed[%d] = %q, want diagnostic %q after the banner", 1+i, got, want)
		}
	}
}

type runtimeCatalogFake struct{ *fakeAgent }

func (*runtimeCatalogFake) LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error) {
	return LoopRuntimeOptions{}, nil
}

type runtimeControllerFake struct{ *fakeAgent }

func (*runtimeControllerFake) SetMode(context.Context, uuid.UUID, ModeID) error     { return nil }
func (*runtimeControllerFake) SetModel(context.Context, uuid.UUID, ModelID) error   { return nil }
func (*runtimeControllerFake) SetEffort(context.Context, uuid.UUID, EffortID) error { return nil }

type runtimeFullFake struct {
	*fakeAgent
	modeID ModeID
	loopID uuid.UUID
}

func (*runtimeFullFake) LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error) {
	return LoopRuntimeOptions{Modes: []ModeOption{{ID: "review", Label: "Review"}}}, nil
}
func (f *runtimeFullFake) SetMode(_ context.Context, loopID uuid.UUID, id ModeID) error {
	f.loopID, f.modeID = loopID, id
	return nil
}
func (*runtimeFullFake) SetModel(context.Context, uuid.UUID, ModelID) error   { return nil }
func (*runtimeFullFake) SetEffort(context.Context, uuid.UUID, EffortID) error { return nil }

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

func TestRuntimeModelChoicesKeepProviderForGrouping(t *testing.T) {
	t.Parallel()

	catalog := runtimeModelCatalogFake{runtimeCatalogFake: &runtimeCatalogFake{fakeAgent: &fakeAgent{activeLoopID: callID(1)}}}
	msg := queryRuntimeChoices(context.Background(), &catalog, runtimeTrayModel, callID(1))().(runtimeChoicesMsg)
	if got, want := msg.items[0].Provider, "OpenAI"; got != want {
		t.Errorf("model provider = %q, want %q for the grouped model tray", got, want)
	}
}

type runtimeModelCatalogFake struct{ *runtimeCatalogFake }

func (*runtimeModelCatalogFake) LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error) {
	return LoopRuntimeOptions{Models: []ModelOption{{ID: "gpt-5.4", Provider: "OpenAI", Label: "GPT-5.4"}}}, nil
}

var _ Agent = (*fakeAgent)(nil)
var _ RuntimeCatalog = (*runtimeCatalogFake)(nil)
var _ RuntimeController = (*runtimeControllerFake)(nil)
var _ RuntimeCatalog = (*runtimeFullFake)(nil)
var _ RuntimeController = (*runtimeFullFake)(nil)
