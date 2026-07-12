package tui

import (
	"context"
	"errors"
	"testing"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/tool"
)

// newTestCore builds a sessionCore over agent (with a fresh /clear thunk yielding the
// same agent and a blank banner), the direct seam the sessionCore transport tests
// drive without a Screen wrapper.
func newTestCore(agent Agent) sessionCore {
	return newSessionCore(context.Background(), agent, fakeOpen(agent), AgentBanner{})
}

// TestSessionCoreHandleEventRoutesToBothReducers pins the shared transport's core
// invariant: one subscription event reaches BOTH the transcript reducer (accumulating
// the live segment / remembering a gate) AND the interaction reducer (enqueuing a
// prompt), and the continuous reader re-arms only while a live subscription is
// installed (the nil-sub guard).
func TestSessionCoreHandleEventRoutesToBothReducers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		withSub         bool
		ev              event.Event
		wantLiveText    string
		wantPending     int
		wantRearm       bool
		wantGatePending bool
	}{
		{
			name:         "token delta routes to transcript, rearms while subscribed",
			withSub:      true,
			ev:           event.TokenDelta{Chunk: &content.TextChunk{Text: "hello"}},
			wantLiveText: "hello",
			wantRearm:    true,
		},
		{
			name:            "permission request enqueues prompt and remembers the gate",
			withSub:         true,
			ev:              event.PermissionRequested{ToolExecutionID: callID(1), Request: tool.BashRequest{Command: "ls"}},
			wantPending:     1,
			wantRearm:       true,
			wantGatePending: true,
		},
		{
			name:         "nil subscription does not re-arm (the /clear-window guard)",
			withSub:      false,
			ev:           event.TokenDelta{Chunk: &content.TextChunk{Text: "hi"}},
			wantLiveText: "hi",
			wantRearm:    false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := newTestCore(&fakeAgent{})
			if tt.withSub {
				c.sub = newFakeSubscription()
			}

			rearm, phase := c.handleEvent(tt.ev)

			if c.transcript.live.Text != tt.wantLiveText {
				t.Errorf("transcript live.Text = %q, want %q", c.transcript.live.Text, tt.wantLiveText)
			}
			if c.interaction.PendingCount() != tt.wantPending {
				t.Errorf("interaction PendingCount = %d, want %d", c.interaction.PendingCount(), tt.wantPending)
			}
			if tt.wantGatePending && c.transcript.live.gateDecisions[callID(1)] != gatePending {
				t.Errorf("gateDecisions[callID(1)] = %v, want gatePending", c.transcript.live.gateDecisions[callID(1)])
			}
			if (rearm != nil) != tt.wantRearm {
				t.Errorf("rearm != nil = %v, want %v", rearm != nil, tt.wantRearm)
			}
			if phase != turnUnchanged {
				t.Errorf("phase = %d, want turnUnchanged (no turn-lifecycle event)", phase)
			}
		})
	}
}

// TestSessionCoreApplyTurnStatus pins the shared turn-status derivation: a PRIMARY
// loop's turn-lifecycle event drives status and reports the phase transition (so a
// presentation shell can react — blink/tip in scrollback, a status pulse in modern);
// a SUBAGENT loop's turn event and a non-turn event leave status untouched.
func TestSessionCoreApplyTurnStatus(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)
	subagent := callID(0xBB)

	tests := []struct {
		name        string
		ev          event.Event
		startStatus Status
		wantStatus  Status
		wantPhase   turnPhase
	}{
		{name: "primary TurnStarted goes running", ev: event.TurnStarted{Header: hdr(primary)}, startStatus: StatusIdle, wantStatus: StatusRunning, wantPhase: turnStarted},
		{name: "primary TurnDone goes idle", ev: event.TurnDone{Header: hdr(primary)}, startStatus: StatusRunning, wantStatus: StatusIdle, wantPhase: turnEnded},
		{name: "primary TurnFailed goes idle", ev: event.TurnFailed{Header: hdr(primary), Err: errors.New("x")}, startStatus: StatusRunning, wantStatus: StatusIdle, wantPhase: turnEnded},
		{name: "primary TurnInterrupted goes idle", ev: event.TurnInterrupted{Header: hdr(primary)}, startStatus: StatusInterrupting, wantStatus: StatusIdle, wantPhase: turnEnded},
		{name: "subagent TurnStarted leaves status", ev: event.TurnStarted{Header: hdr(subagent)}, startStatus: StatusRunning, wantStatus: StatusRunning, wantPhase: turnUnchanged},
		{name: "token delta leaves status", ev: event.TokenDelta{Header: hdr(primary), Chunk: &content.TextChunk{Text: "x"}}, startStatus: StatusRunning, wantStatus: StatusRunning, wantPhase: turnUnchanged},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := newTestCore(&fakeAgent{rootLoopID: primary})
			c.status = tt.startStatus

			phase := c.applyTurnStatus(tt.ev)
			if c.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", c.status, tt.wantStatus)
			}
			if phase != tt.wantPhase {
				t.Errorf("phase = %d, want %d", phase, tt.wantPhase)
			}
		})
	}
}

// TestSessionCoreReopenOrdering pins the /clear reopen ordering the two shells share:
// on success the OLD subscription is CLOSED, the agent is swapped to the fresh one, the
// transcript/interaction/status reset, and the re-subscribe targets the FRESH agent
// (proving the swap happened before the re-subscribe command was built). On error the old agent is
// kept, an error entry commits, and the shell is told to present it.
func TestSessionCoreReopenOrdering(t *testing.T) {
	t.Parallel()

	t.Run("success closes old sub before swapping and re-subscribes the fresh agent", func(t *testing.T) {
		t.Parallel()

		old := &fakeAgent{rootLoopID: callID(0x01)}
		fresh := &fakeAgent{rootLoopID: callID(0x02)}
		c := newTestCore(old)
		oldSub := newFakeSubscription()
		c.sub = oldSub
		c.status = StatusResetting
		c.transcript = c.transcript.CommitUser([]content.Block{&content.TextBlock{Text: "x"}})
		c = feedPrompt(c)

		cmd, present := c.applyReopenResult(reopenResultMsg{agent: fresh})
		if present {
			t.Fatal("present = true on success, want false (nothing to flush; the shell resets)")
		}
		if !oldSub.closed {
			t.Error("old subscription not closed on /clear swap")
		}
		if c.sub != nil {
			t.Errorf("sub = %p, want nil (old dropped; the re-subscribe installs the new one)", c.sub)
		}
		if c.Agent() != fresh {
			t.Errorf("agent = %p, want fresh %p", c.Agent(), fresh)
		}
		if c.transcript.primaryLoopID != fresh.rootLoopID {
			t.Errorf("transcript primaryLoopID = %v, want fresh %v (read after the swap)", c.transcript.primaryLoopID, fresh.rootLoopID)
		}
		if len(c.transcript.committed) != 0 {
			t.Errorf("committed = %d, want 0 (reset)", len(c.transcript.committed))
		}
		if c.interaction.PendingCount() != 0 {
			t.Errorf("PendingCount = %d, want 0 (prompts cleared)", c.interaction.PendingCount())
		}
		if c.status != StatusIdle {
			t.Errorf("status = %d, want StatusIdle", c.status)
		}
		// Draining the batch must close the OLD agent and re-subscribe against the FRESH one.
		drainCmd(t, cmd)
		if !old.closeCalled {
			t.Error("old agent Close() not called")
		}
		if fresh.subscribeCount != 1 {
			t.Errorf("fresh Subscribe count = %d, want 1 (swap happened before the re-subscribe command was built)", fresh.subscribeCount)
		}
		if old.subscribeCount != 0 {
			t.Errorf("old Subscribe count = %d, want 0 (re-subscribe must target the fresh agent)", old.subscribeCount)
		}
	})

	t.Run("error keeps the old agent and asks the shell to present", func(t *testing.T) {
		t.Parallel()

		old := &fakeAgent{}
		c := newTestCore(old)
		c.status = StatusResetting

		cmd, present := c.applyReopenResult(reopenResultMsg{err: errors.New("no agent")})
		if !present {
			t.Error("present = false on error, want true (the committed error must be shown)")
		}
		if cmd != nil {
			t.Errorf("cmd = non-nil, want nil (the shell flushes; no transport cmd)")
		}
		if c.Agent() != old {
			t.Error("agent swapped on error, want unchanged")
		}
		if c.status != StatusIdle {
			t.Errorf("status = %d, want StatusIdle", c.status)
		}
		if rec := c.transcript.committed[len(c.transcript.committed)-1]; rec.Kind != kindNotice || rec.Level != noticeError {
			t.Errorf("last committed = (kind %d, level %d), want (kindNotice, noticeError)", rec.Kind, rec.Level)
		}
	})
}

// TestSessionCoreMapAction pins the shared submit/interrupt/gate command wiring: each
// typed uiAction produces the right bounded command (and, for the gate actions, records
// the transcript decision), and reports whether it committed an out-of-band entry the
// shell must present.
func TestSessionCoreMapAction(t *testing.T) {
	t.Parallel()

	t.Run("uiSubmit fires submit fire-and-forget", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{}
		c := newTestCore(agent)
		cmd, present := c.mapAction(uiAction{Kind: uiSubmit, Text: "hi there"})
		if present {
			t.Error("present = true, want false (a valid submit commits no out-of-band row)")
		}
		if cmd == nil {
			t.Fatal("submit cmd = nil, want submitCmd")
		}
		drainCmd(t, cmd)
		if !agent.submitCalled {
			t.Error("Submit not called")
		}
		if got := firstBlockText(agent.lastSubmitBlocks); got != "hi there" {
			t.Errorf("Submit blocks = %q, want %q", got, "hi there")
		}
	})

	t.Run("uiSubmit with a bad attachment commits a user row then an error", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{}
		c := newTestCore(agent)
		cmd, present := c.mapAction(uiAction{Kind: uiSubmit, Text: "@nope.pem"})
		if !present {
			t.Error("present = false, want true (the build error must be shown)")
		}
		if cmd != nil {
			t.Errorf("cmd = non-nil, want nil (nothing sent; the shell flushes)")
		}
		if agent.submitCalled {
			t.Error("Submit called on a build error, want not called")
		}
		if got := len(c.transcript.committed); got != 2 {
			t.Fatalf("committed = %d, want 2 (user row + error notice)", got)
		}
		if rec := c.transcript.committed[1]; rec.Kind != kindNotice || rec.Level != noticeError {
			t.Errorf("second committed = (kind %d, level %d), want (kindNotice, noticeError)", rec.Kind, rec.Level)
		}
	})

	t.Run("uiApprove records the decision and dispatches Approve", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{}
		c := newTestCore(agent)
		// Remember the gate first so ResolveGate has a pending decision to flip.
		c.transcript.live.gateDecisions = map[uuid.UUID]gateDecision{callID(7): gatePending}
		cmd, present := c.mapAction(uiAction{Kind: uiApprove, LoopID: callID(9), ToolExecutionID: callID(7), Scope: tool.ScopeOnce})
		if present {
			t.Error("present = true, want false (a gate reply commits no scrollback entry)")
		}
		if got := c.transcript.live.gateDecisions[callID(7)]; got != gateApproved {
			t.Errorf("gateDecisions[callID(7)] = %v, want gateApproved", got)
		}
		if cmd == nil {
			t.Fatal("approve cmd = nil, want approveCmd")
		}
		cmd()
		if !agent.approveCalled || agent.lastLoopID != callID(9) || agent.lastCallID != callID(7) || agent.lastScope != tool.ScopeOnce {
			t.Errorf("Approve dispatch = (called %v, loop %v, id %v, scope %v), want (true, %v, %v, ScopeOnce)",
				agent.approveCalled, agent.lastLoopID, agent.lastCallID, agent.lastScope, callID(9), callID(7))
		}
	})

	t.Run("uiInterrupt interrupts a running turn", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{}
		c := newTestCore(agent)
		c.status = StatusRunning
		cmd, present := c.mapAction(uiAction{Kind: uiInterrupt})
		if present {
			t.Error("present = true, want false")
		}
		if cmd == nil {
			t.Fatal("interrupt cmd = nil, want interruptTurn")
		}
		if c.status != StatusInterrupting {
			t.Errorf("status = %d, want StatusInterrupting", c.status)
		}
	})

	t.Run("uiNoop is inert", func(t *testing.T) {
		t.Parallel()
		c := newTestCore(&fakeAgent{})
		cmd, present := c.mapAction(uiAction{Kind: uiNoop})
		if present || cmd != nil {
			t.Errorf("uiNoop = (cmd %v, present %v), want (nil, false)", cmd != nil, present)
		}
	})
}

// hdr builds an event Header carrying only a loop id, the field the transport routing
// (turn status, primary vs subagent) keys on.
func hdr(loopID uuid.UUID) event.Header {
	return event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}
}

// feedPrompt enqueues one pending prompt into the core's interaction surface so a test
// can assert /clear clears it. It routes a PermissionRequested through the core exactly
// as the live path would.
func feedPrompt(c sessionCore) sessionCore {
	c.interaction = c.interaction.ApplyEvent(event.PermissionRequested{ToolExecutionID: callID(5), Request: tool.BashRequest{Command: "x"}})
	return c
}
