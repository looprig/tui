package presentation

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/gate"
	"github.com/looprig/harness/pkg/identity"
)

// openURLGateID is the deterministic gate id the open-url fixtures carry.
var openURLGateID = gate.ID{0x0b, 0xad, 0xca, 0xfe, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0x02}

// openURLLoopID is the deterministic producing-loop id the open-url fixtures
// carry. It is NON-ZERO on purpose: a zero id would let a router that ignored the
// loop entirely still pass.
var openURLLoopID = uuid.UUID{0x21, 0x31, 0x41, 0x51, 0x61, 0x71, 0x81, 0x91, 0xa1, 0xb1, 0xc1, 0xd1, 0xe1, 0xf1, 0x01, 0x03}

// fixtureOrigin is the validated bare origin the fixtures display.
const fixtureOrigin = "https://github.com"

// canaryURL is the ephemeral action target an opener would hold PRIVATELY. It
// appears in NO fixture envelope — it exists so the leak assertions have a
// concrete secret to hunt for, and so this file states plainly what must never
// reach the screen.
const canaryURL = "https://github.com/login/oauth/authorize?state=CANARYSTATE&code_challenge=CANARYPKCE"

// openURLGateOpened builds the GateOpened envelope the TUI folds. origin and
// controls vary per test; everything else is the fixture's.
func openURLGateOpened(origin string, controls ...string) event.GateOpened {
	prompt := gate.Prompt{
		Title:  "Authorize Example",
		Body:   "Finish signing in, then confirm below.",
		Origin: origin,
	}
	for _, c := range controls {
		prompt.Controls = append(prompt.Controls, gate.Control{Action: c, Label: c})
	}
	return event.GateOpened{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: openURLLoopID}},
		Gate: gate.Gate{
			ID:       openURLGateID,
			Kind:     gate.KindOpenURL,
			Resolver: gate.ResolverSession,
			Prompt:   prompt,
		},
	}
}

// openURLModel folds an open-url GateOpened into a fresh interaction model.
func openURLModel(origin string, controls ...string) interactionModel {
	return newInteractionModel().ApplyEvent(openURLGateOpened(origin, controls...))
}

// renderOpenURL renders the active open-url card the way the surface does.
func renderOpenURL(t *testing.T, m interactionModel) string {
	t.Helper()
	p := m.ActivePrompt()
	if p == nil {
		t.Fatal("no active prompt to render")
	}
	return renderOpenURLBox(*p, 60, m.PendingCount())
}

// TestOpenURLGateRendersTheValidatedOrigin is the headline proof: the TUI folds an
// open-url gate out of the PUBLIC envelope and shows the origin — the one thing
// the human's trust decision rests on.
func TestOpenURLGateRendersTheValidatedOrigin(t *testing.T) {
	t.Parallel()
	m := openURLModel(fixtureOrigin, gate.FormActionAccept, gate.FormActionDecline)
	if m.mode != modeOpenURLPrompt {
		t.Fatalf("mode = %v, want modeOpenURLPrompt", m.mode)
	}
	if m.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", m.PendingCount())
	}
	got := stripANSI(renderOpenURL(t, m))
	for _, want := range []string{
		"Authorize Example",               // the title
		"Finish signing in",               // the opener's body
		"origin: " + fixtureOrigin,        // the origin, LABELLED as one
		"Continue only if you trust this", // the TUI's own trust caution
		openURLCompleteHint,               // the completion action
		"[esc] decline",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("open-url card missing %q in:\n%s", want, got)
		}
	}
	assertPanelFramed(t, renderOpenURL(t, m))
}

// TestOpenURLGateNeverRendersAURL is the leak canary.
//
// It is a structural claim, not a hopeful one: the view-model has no url field, so
// there is nothing to render. This asserts the consequence end-to-end — an
// envelope is all the TUI is given, and even when an opener has stuffed a full
// action URL into the prose the TUI does render (Body is opener-supplied text),
// nothing the TUI ADDS reintroduces a target. The origin row shows the bare origin
// and only the bare origin.
func TestOpenURLGateNeverRendersAURL(t *testing.T) {
	t.Parallel()
	ev := openURLGateOpened(fixtureOrigin, gate.FormActionAccept, gate.FormActionDecline)
	m := newInteractionModel().ApplyEvent(ev)

	p := m.ActivePrompt()
	if p == nil {
		t.Fatal("no active prompt")
	}
	// The view-model carries the origin and nothing else that could be a target.
	if p.Origin != fixtureOrigin {
		t.Fatalf("Origin = %q, want %q", p.Origin, fixtureOrigin)
	}
	got := stripANSI(renderOpenURL(t, m))
	for _, secret := range []string{canaryURL, "CANARYSTATE", "CANARYPKCE", "code_challenge", "state=", "/login/oauth"} {
		if strings.Contains(got, secret) {
			t.Errorf("open-url card leaked %q:\n%s", secret, got)
		}
	}
}

// TestOpenURLGateHonorsControls is the fail-closed router proof. The session
// refuses an action a gate never advertised, so a key for an unoffered action must
// be a no-op HERE — and the legend must not print a key that does nothing.
//
// The completion action is how RequiresCompletion reaches the TUI: an opener that
// wants an explicit "I finished" offers accept, and one that does not, does not.
func TestOpenURLGateHonorsControls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		controls   []string
		enterKind  uiActionKind
		escKind    uiActionKind
		wantLegend []string
		denyLegend []string
	}{
		{
			name:       "both actions offered",
			controls:   []string{gate.FormActionAccept, gate.FormActionDecline},
			enterKind:  uiGateRespond,
			escKind:    uiGateRespond,
			wantLegend: []string{"[enter]", "[esc]"},
		},
		{
			// RequiresCompletion false: no accept control, so no completion key.
			name:       "completion not required",
			controls:   []string{gate.FormActionDecline},
			enterKind:  uiNoop,
			escKind:    uiGateRespond,
			wantLegend: []string{"[esc]"},
			denyLegend: []string{"[enter]"},
		},
		{
			name:       "decline not offered",
			controls:   []string{gate.FormActionAccept},
			enterKind:  uiGateRespond,
			escKind:    uiNoop,
			wantLegend: []string{"[enter]"},
			denyLegend: []string{"[esc]"},
		},
		{
			name:       "no actions offered",
			controls:   nil,
			enterKind:  uiNoop,
			escKind:    uiNoop,
			wantLegend: []string{"no actions offered"},
			denyLegend: []string{"[enter]", "[esc]"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := openURLModel(fixtureOrigin, tt.controls...)

			_, action := m.openURLKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			if action.Kind != tt.enterKind {
				t.Errorf("enter = %v, want %v", action.Kind, tt.enterKind)
			}
			if tt.enterKind == uiGateRespond && action.GateAction != gate.FormActionAccept {
				t.Errorf("enter action = %q, want an accept", action.GateAction)
			}
			_, action = m.openURLKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			if action.Kind != tt.escKind {
				t.Errorf("esc = %v, want %v", action.Kind, tt.escKind)
			}
			if tt.escKind == uiGateRespond && action.GateAction != gate.FormActionDecline {
				t.Errorf("esc action = %q, want a decline", action.GateAction)
			}

			got := stripANSI(renderOpenURL(t, m))
			for _, want := range tt.wantLegend {
				if !strings.Contains(got, want) {
					t.Errorf("legend missing %q in:\n%s", want, got)
				}
			}
			for _, unwanted := range tt.denyLegend {
				if strings.Contains(got, unwanted) {
					t.Errorf("legend offers %q, which the gate never advertised:\n%s", unwanted, got)
				}
			}
		})
	}
}

// TestOpenURLGateRespondsWithTheGateAndLoop proves the response names the right
// gate and loop, and carries no values — an open-url gate has no fields, and a
// values map would be the TUI inventing an answer.
func TestOpenURLGateRespondsWithTheGateAndLoop(t *testing.T) {
	t.Parallel()
	m := openURLModel(fixtureOrigin, gate.FormActionAccept, gate.FormActionDecline)

	next, action := m.openURLKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action.GateID != openURLGateID {
		t.Errorf("GateID = %v, want %v", action.GateID, openURLGateID)
	}
	if action.LoopID != openURLLoopID {
		t.Errorf("LoopID = %v, want %v", action.LoopID, openURLLoopID)
	}
	if action.Values != nil {
		t.Errorf("Values = %v, want nil (an open-url gate has no fields)", action.Values)
	}
	// The head is popped optimistically, returning the surface to compose.
	if next.PendingCount() != 0 {
		t.Errorf("pending after answering = %d, want 0", next.PendingCount())
	}
	if next.mode != modeCompose {
		t.Errorf("mode after answering = %v, want modeCompose", next.mode)
	}
}

// TestOpenURLGateRefusesAnInvalidEnvelope is the fail-closed render path. The
// origin is the entire security content of this card; an envelope whose origin is
// missing, malformed, or is really a full action URL cannot be shown as a trust
// decision — the TUI re-runs the session's own gate.ValidateGate rather than
// trusting the event, and degrades to a decline-only notice.
func TestOpenURLGateRefusesAnInvalidEnvelope(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		origin string
	}{
		{name: "no origin", origin: ""},
		{name: "origin is really an action URL", origin: canaryURL},
		{name: "origin carries a path", origin: "https://github.com/login/oauth"},
		{name: "origin carries userinfo", origin: "https://user:pass@github.com"},
		{name: "unsupported scheme", origin: "javascript:alert(1)"},
		{name: "not a url at all", origin: "github, probably"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := openURLModel(tt.origin, gate.FormActionAccept, gate.FormActionDecline)
			p := m.ActivePrompt()
			if p == nil {
				t.Fatal("an unrenderable gate must still be shown so it can be declined")
			}
			if !p.unsupported {
				t.Fatal("an invalid envelope was accepted as renderable")
			}
			if p.Origin != "" {
				t.Errorf("Origin = %q; an unvalidated origin must not be retained", p.Origin)
			}

			got := stripANSI(renderOpenURL(t, m))
			if !strings.Contains(got, "did not name a valid origin") {
				t.Errorf("no unsupported notice in:\n%s", got)
			}
			// The rejected origin — which may BE the action URL — is not on screen.
			if tt.origin != "" && strings.Contains(got, tt.origin) {
				t.Errorf("card rendered the rejected origin %q:\n%s", tt.origin, got)
			}
			// Accepting would be the user vouching for what the TUI could not.
			_, action := m.openURLKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			if action.Kind != uiNoop {
				t.Errorf("enter on an invalid gate produced %v, want uiNoop", action.Kind)
			}
			// Declining stays available: it is the only honest action left.
			_, action = m.openURLKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			if action.Kind != uiGateRespond || action.GateAction != gate.FormActionDecline {
				t.Errorf("esc = %v/%q, want a decline", action.Kind, action.GateAction)
			}
		})
	}
}

// TestOpenURLGateIgnoresOtherKeys proves the card takes no other input: there is
// no editor here, and — critically — no "open" key, because the TUI has no URL to
// open. The host owns the browser.
func TestOpenURLGateIgnoresOtherKeys(t *testing.T) {
	t.Parallel()
	m := openURLModel(fixtureOrigin, gate.FormActionAccept, gate.FormActionDecline)
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'o', Text: "o"},
		{Code: 'y', Text: "y"},
		{Code: tea.KeyUp},
		{Code: tea.KeyRight},
		{Code: tea.KeySpace},
		{Code: tea.KeyBackspace},
	} {
		next, action := m.openURLKey(msg)
		if action.Kind != uiNoop {
			t.Errorf("key %v produced %v, want uiNoop", msg.Code, action.Kind)
		}
		if next.PendingCount() != 1 {
			t.Errorf("key %v resolved the gate; it must stay open", msg.Code)
		}
	}
}

// TestOpenURLGateClearedWhenResolvedElsewhere proves a gate answered somewhere
// else (a policy timeout, another client) does not leave a stale card asking for a
// decision already made.
func TestOpenURLGateClearedWhenResolvedElsewhere(t *testing.T) {
	t.Parallel()
	m := openURLModel(fixtureOrigin, gate.FormActionAccept, gate.FormActionDecline)
	if m.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", m.PendingCount())
	}
	m = m.ApplyEvent(event.GateResolved{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: openURLLoopID}},
		GateID: openURLGateID,
	})
	if m.PendingCount() != 0 {
		t.Fatalf("pending = %d, want 0 (the gate was resolved elsewhere)", m.PendingCount())
	}
	if m.mode != modeCompose {
		t.Errorf("mode = %v, want modeCompose", m.mode)
	}
}
