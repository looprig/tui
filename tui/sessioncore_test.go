package tui

import (
	"context"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

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
// (proving the swap happened before the re-subscribe command was built). On error the already
// closed old agent is dropped, an error entry commits, and the shell is told to present and exit.
func TestSessionCoreReopenOrdering(t *testing.T) {
	t.Parallel()

	t.Run("success closes old sub before swapping and re-subscribes the fresh agent", func(t *testing.T) {
		t.Parallel()

		old := &fakeAgent{rootLoopID: callID(0x01)}
		fresh := &fakeAgent{rootLoopID: callID(0x02)}
		c := newTestCore(old)
		c.openAgent = fakeOpen(fresh)
		oldSub := newFakeSubscription()
		c.sub = oldSub
		c.transcript = c.transcript.CommitUser([]content.Block{&content.TextBlock{Text: "x"}})
		c = feedPrompt(c)

		reopen, present := c.runSlash("/clear")
		if present || reopen == nil {
			t.Fatal("/clear did not begin reopen")
		}
		msg := reopen().(reopenResultMsg)
		cmd, present := c.applyReopenResult(msg)
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
		if c.transcript.rootLoopID != fresh.rootLoopID {
			t.Errorf("transcript rootLoopID = %v, want fresh %v (read after the swap)", c.transcript.rootLoopID, fresh.rootLoopID)
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
		// Draining the command re-subscribes against the FRESH agent; the old agent was
		// already closed before the opener returned it.
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

	t.Run("error drops the closed old agent, presents, and exits", func(t *testing.T) {
		t.Parallel()

		old := &fakeAgent{}
		c := newTestCore(old)
		c.openAgent = func(context.Context) (Agent, error) { return nil, errors.New("no agent") }

		reopen, present := c.runSlash("/clear")
		if present || reopen == nil {
			t.Fatal("/clear did not begin reopen")
		}
		msg := reopen().(reopenResultMsg)
		cmd, present := c.applyReopenResult(msg)
		if !present {
			t.Error("present = false on error, want true (the committed error must be shown)")
		}
		if c.Agent() != nil {
			t.Error("agent retained on error after close, want nil (no live session remains)")
		}
		if old.closeCalls != 1 {
			t.Errorf("old Close calls = %d, want exactly 1", old.closeCalls)
		}
		if cmd == nil {
			t.Fatal("cmd = nil, want terminal quit after failed replacement open")
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

// TestSessionCoreSubscribeBaseline pins the active-selection HANDSHAKE: the authoritative
// active baseline is read from ActiveLoopID ONLY after the subscription exists
// (applySubscribed), and each queued ActiveLoopChanged is reconciled causally against that
// post-subscription baseline — a matching Previous advances it, a selection the baseline
// already reflects is a no-op, and a stale selection can never regress it.
func TestSessionCoreSubscribeBaseline(t *testing.T) {
	t.Parallel()

	root := loopID(0xF0)
	planner, builder, reviewer := loopID(0xA1), loopID(0xB2), loopID(0xC3)
	session := loopID(0x99)

	tests := []struct {
		name       string
		baseline   uuid.UUID
		queued     []event.ActiveLoopChanged
		wantActive uuid.UUID
		wantStatus Status
	}{
		{name: "transition after baseline", baseline: planner, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}}, wantActive: builder, wantStatus: StatusIdle},
		{name: "baseline already includes transition", baseline: builder, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}}, wantActive: builder, wantStatus: StatusIdle},
		{name: "stale event cannot regress baseline", baseline: reviewer, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}}, wantActive: reviewer, wantStatus: StatusIdle},
		{name: "queued chain catches up", baseline: planner, queued: []event.ActiveLoopChanged{{PreviousLoopID: planner, ActiveLoopID: builder}, {PreviousLoopID: builder, ActiveLoopID: reviewer}}, wantActive: reviewer, wantStatus: StatusIdle},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub := newFakeSubscription()
			agent := &fakeAgent{rootLoopID: root, activeLoopID: tt.baseline, subStream: sub}
			c := newTestCore(agent)

			// Drive the real order: run the subscription command, then apply the
			// subscribedMsg (which reads the baseline only after the stream exists).
			subscribeCore(t, &c)
			if c.activeLoopID != tt.baseline {
				t.Fatalf("post-subscription baseline = %v, want %v", c.activeLoopID, tt.baseline)
			}

			// Drain the queued selections through handleEvent, exactly as the reader would.
			for _, ev := range tt.queued {
				c.handleEvent(selectionEvent(session, ev))
			}

			if c.activeLoopID != tt.wantActive {
				t.Errorf("activeLoopID = %v, want %v", c.activeLoopID, tt.wantActive)
			}
			if c.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", c.status, tt.wantStatus)
			}
		})
	}
}

// TestSessionCoreSelectionFailsClosed pins the fail-closed guard: an ActiveLoopChanged that
// arrives BEFORE the subscription handshake establishes a baseline (activeLoopID still zero)
// is IGNORED — without an authority the selection cannot be causally ordered — leaving both
// the baseline and the displayed status untouched.
func TestSessionCoreSelectionFailsClosed(t *testing.T) {
	t.Parallel()

	root := loopID(0xF0)
	planner, builder := loopID(0xA1), loopID(0xB2)
	session := loopID(0x99)

	tests := []struct {
		name        string
		startStatus Status
	}{
		{name: "idle: selection before baseline is ignored", startStatus: StatusIdle},
		{name: "running: status is left untouched", startStatus: StatusRunning},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Stream installed, but applySubscribed has NOT run — so activeLoopID is still
			// zero (no authoritative baseline).
			c := newTestCore(&fakeAgent{rootLoopID: root})
			c.sub = newFakeSubscription()
			c.status = tt.startStatus

			c.handleEvent(selectionEvent(session, event.ActiveLoopChanged{PreviousLoopID: planner, ActiveLoopID: builder}))

			if !c.activeLoopID.IsZero() {
				t.Errorf("activeLoopID = %v, want zero (fail closed: no baseline)", c.activeLoopID)
			}
			if c.status != tt.startStatus {
				t.Errorf("status = %d, want %d (untouched)", c.status, tt.startStatus)
			}
		})
	}
}

// TestSessionCoreReopenSetupWindow pins the /clear reopen setup-window race: the replacement
// subscription queues a selection DURING Subscribe (before applySubscribed reads the
// baseline), and the reconciliation converges on the causally newest active id whether or
// not the baseline read already caught the selection.
func TestSessionCoreReopenSetupWindow(t *testing.T) {
	t.Parallel()

	root := loopID(0xF0)
	planner, builder := loopID(0xA1), loopID(0xB2)
	session := loopID(0x99)

	tests := []struct {
		name       string
		baseline   uuid.UUID // fresh agent's ActiveLoopID at the moment applySubscribed reads it
		wantActive uuid.UUID
	}{
		{name: "baseline read missed the setup-window selection", baseline: planner, wantActive: builder},
		{name: "baseline read already caught the setup-window selection", baseline: builder, wantActive: builder},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			old := &fakeAgent{rootLoopID: root, subStream: newFakeSubscription()}
			c := newTestCore(old)
			c.sub = old.subStream
			c.status = StatusResetting

			freshSub := newFakeSubscription()
			fresh := &fakeAgent{
				rootLoopID:   root,
				activeLoopID: tt.baseline,
				subStream:    freshSub,
				subEnqueue:   []event.Event{selectionEvent(session, event.ActiveLoopChanged{PreviousLoopID: planner, ActiveLoopID: builder})},
			}

			// Reopen swaps in the fresh agent and clears the baseline; the batch it returns
			// re-subscribes. Drive the fresh subscribe leg directly so the setup-window
			// enqueue (pushed inside Subscribe) precedes applySubscribed's baseline read.
			if _, present := c.applyReopenResult(reopenResultMsg{agent: fresh}); present {
				t.Fatal("reopen reported an error, want success")
			}
			if !c.activeLoopID.IsZero() {
				t.Fatalf("activeLoopID = %v after reopen, want zero (baseline re-read on the fresh subscribe)", c.activeLoopID)
			}

			subscribeCore(t, &c)
			drainStream(t, &c, freshSub)

			if c.activeLoopID != tt.wantActive {
				t.Errorf("activeLoopID = %v, want %v (causally newest)", c.activeLoopID, tt.wantActive)
			}
		})
	}
}

// TestSessionCoreActiveRunningStatus pins the per-loop running fold: EVERY loop's turn
// events fold its running bit, but the DISPLAYED status follows the active loop — so a
// selection or the active loop's own terminal moves the status, while a background loop's
// turn events fold silently.
func TestSessionCoreActiveRunningStatus(t *testing.T) {
	t.Parallel()

	root := loopID(0xF0)
	planner, builder := loopID(0xA1), loopID(0xB2)
	session := loopID(0x99)

	sel := func(prev, next uuid.UUID) event.Event {
		return selectionEvent(session, event.ActiveLoopChanged{PreviousLoopID: prev, ActiveLoopID: next})
	}

	tests := []struct {
		name        string
		baseline    uuid.UUID
		events      []event.Event
		wantActive  uuid.UUID
		wantStatus  Status
		wantRunning map[uuid.UUID]bool
	}{
		{
			name:        "active loop start then select an idle loop yields idle",
			baseline:    planner,
			events:      []event.Event{event.TurnStarted{Header: hdr(planner)}, sel(planner, builder)},
			wantActive:  builder,
			wantStatus:  StatusIdle,
			wantRunning: map[uuid.UUID]bool{planner: true, builder: false},
		},
		{
			name:        "background loop already running then select it yields running",
			baseline:    planner,
			events:      []event.Event{event.TurnStarted{Header: hdr(builder)}, sel(planner, builder)},
			wantActive:  builder,
			wantStatus:  StatusRunning,
			wantRunning: map[uuid.UUID]bool{planner: false, builder: true},
		},
		{
			name:        "late terminal on a background loop leaves the active loop running",
			baseline:    planner,
			events:      []event.Event{event.TurnStarted{Header: hdr(builder)}, sel(planner, builder), event.TurnDone{Header: hdr(planner)}},
			wantActive:  builder,
			wantStatus:  StatusRunning,
			wantRunning: map[uuid.UUID]bool{planner: false, builder: true},
		},
		{
			name:        "active loop terminal yields idle",
			baseline:    builder,
			events:      []event.Event{event.TurnStarted{Header: hdr(builder)}, event.TurnDone{Header: hdr(builder)}},
			wantActive:  builder,
			wantStatus:  StatusIdle,
			wantRunning: map[uuid.UUID]bool{builder: false},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub := newFakeSubscription()
			agent := &fakeAgent{rootLoopID: root, activeLoopID: tt.baseline, subStream: sub}
			c := newTestCore(agent)
			subscribeCore(t, &c)

			for _, ev := range tt.events {
				c.handleEvent(ev)
			}

			if c.activeLoopID != tt.wantActive {
				t.Errorf("activeLoopID = %v, want %v", c.activeLoopID, tt.wantActive)
			}
			if c.status != tt.wantStatus {
				t.Errorf("status = %d, want %d", c.status, tt.wantStatus)
			}
			for id, want := range tt.wantRunning {
				if got := c.loopRunning[id]; got != want {
					t.Errorf("loopRunning[%v] = %v, want %v", id, got, want)
				}
			}
		})
	}
}

// TestSessionCoreTargetImageCapability pins that the image-capability query is keyed on the
// SUBMISSION TARGET, not the session as a whole: submit queries the reconciled active loop
// (effectiveActiveLoopID) and submitToLoop queries its explicit focused target. A capable
// target accepts an image @path; a text-only or unknown target rejects it at the build
// boundary (fail closed) — committing the message plus a faint error and sending nothing.
func TestSessionCoreTargetImageCapability(t *testing.T) {
	t.Parallel()

	root := loopID(0xF0)
	planner, builder, unknown := loopID(0xA1), loopID(0xB2), loopID(0xC3)

	// A PNG fixture routed through the real attachment helpers, so the capability gate is
	// exercised on a genuine image classification rather than a synthetic block. The byte
	// contents are irrelevant: classification is by the .png extension, decided BEFORE any
	// file read, so the capability gate never inspects them.
	dir := t.TempDir()
	png := writeFile(t, dir, "image.png", []byte{0x89, 'P', 'N', 'G'})
	text := "look @" + png

	tests := []struct {
		name         string
		caps         map[uuid.UUID]bool
		activeLoopID uuid.UUID // the core's reconciled active loop (submit's effective target)
		useToLoop    bool      // true → submitToLoop(target); false → generic submit
		target       uuid.UUID // submitToLoop's explicit focused target (ignored for submit)
		wantAccepted bool
		wantQueried  uuid.UUID // the loop the capability query must have asked about
	}{
		{
			name:         "focused image-capable builder accepts the image",
			caps:         map[uuid.UUID]bool{builder: true},
			useToLoop:    true,
			target:       builder,
			wantAccepted: true,
			wantQueried:  builder,
		},
		{
			name:         "focused text-only planner rejects the same image",
			caps:         map[uuid.UUID]bool{planner: false, builder: true},
			useToLoop:    true,
			target:       planner,
			wantAccepted: false,
			wantQueried:  planner,
		},
		{
			name:         "unknown focused loop fails closed",
			caps:         map[uuid.UUID]bool{builder: true},
			useToLoop:    true,
			target:       unknown,
			wantAccepted: false,
			wantQueried:  unknown,
		},
		{
			name:         "generic submit queries the reconciled active loop",
			caps:         map[uuid.UUID]bool{builder: true},
			activeLoopID: builder,
			useToLoop:    false,
			wantAccepted: true,
			wantQueried:  builder,
		},
		{
			// Zero-baseline boundary: activeLoopID unestablished (pre-first-subscribe),
			// so effectiveActiveLoopID falls back to the ROOT — the exact window the
			// effectiveActiveLoopID deviation exists to serve. A root that supports images
			// must accept, and the query must target the root, not the zero loop.
			name:         "generic submit pre-baseline falls back to the root loop",
			caps:         map[uuid.UUID]bool{root: true},
			activeLoopID: uuid.UUID{}, // zero: no authoritative baseline yet
			useToLoop:    false,
			wantAccepted: true,
			wantQueried:  root,
		},
		{
			name:         "generic submit fails closed on a text-only active loop",
			caps:         map[uuid.UUID]bool{planner: false},
			activeLoopID: planner,
			useToLoop:    false,
			wantAccepted: false,
			wantQueried:  planner,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := &fakeAgent{rootLoopID: root, acceptsImages: tt.caps}
			c := newTestCore(agent)
			c.activeLoopID = tt.activeLoopID

			var (
				cmd     tea.Cmd
				present bool
			)
			if tt.useToLoop {
				cmd, present = c.submitToLoop(tt.target, text)
			} else {
				cmd, present = c.submit(text)
			}

			if agent.lastAcceptsImagesLoopID != tt.wantQueried {
				t.Errorf("queried loop = %v, want %v", agent.lastAcceptsImagesLoopID, tt.wantQueried)
			}

			if tt.wantAccepted {
				if present {
					t.Errorf("present = true, want false (accepted image commits nothing out-of-band)")
				}
				drainCmd(t, cmd)
				if tt.useToLoop && !agent.submitToLoopCalled {
					t.Error("SubmitToLoop not called for an accepted image")
				}
				if !tt.useToLoop && !agent.submitCalled {
					t.Error("Submit not called for an accepted image")
				}
				return
			}

			if !present {
				t.Errorf("present = false, want true (a rejected image commits a faint error)")
			}
			if cmd != nil {
				t.Errorf("cmd = non-nil, want nil (a rejected image sends nothing)")
			}
			if agent.submitCalled || agent.submitToLoopCalled {
				t.Error("Submit/SubmitToLoop called on a rejected image, want no send")
			}
		})
	}
}

// TestSessionCoreTargetImageCapabilityDynamic pins that the capability is re-queried on EACH
// submission against the LIVE agent: flipping the focused loop from image-capable to text-only
// between two submissions flips the second result, with no Agent rebuild in between.
func TestSessionCoreTargetImageCapabilityDynamic(t *testing.T) {
	t.Parallel()

	root, builder := loopID(0xF0), loopID(0xB2)
	dir := t.TempDir()
	png := writeFile(t, dir, "image.png", []byte{0x89, 'P', 'N', 'G'})
	text := "look @" + png

	agent := &fakeAgent{rootLoopID: root, acceptsImages: map[uuid.UUID]bool{builder: true}}
	c := newTestCore(agent)

	// First submission: builder is image-capable → accepted and sent.
	cmd, present := c.submitToLoop(builder, text)
	if present {
		t.Fatal("first submit rejected the image, want accepted")
	}
	drainCmd(t, cmd)
	if !agent.submitToLoopCalled {
		t.Fatal("first submit did not call SubmitToLoop")
	}

	// Flip the SAME agent to text-only and reset the send recorder.
	agent.acceptsImages[builder] = false
	agent.submitToLoopCalled = false

	// Second submission on the SAME core+agent must now reject at the boundary.
	cmd, present = c.submitToLoop(builder, text)
	if !present {
		t.Error("second submit accepted the image after builder went text-only, want rejected")
	}
	if cmd != nil {
		t.Error("second submit produced a command, want nil (nothing sent)")
	}
	if agent.submitToLoopCalled {
		t.Error("SubmitToLoop called on the rejected second submit")
	}
}

// hdr builds an event Header carrying only a loop id, the field the transport routing
// (turn status, primary vs subagent) keys on.
func hdr(loopID uuid.UUID) event.Header {
	return event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}
}

// selectionEvent stamps an ActiveLoopChanged with a valid session header (the field the
// session-scoped selection transition carries), leaving its Previous/Active loop ids intact.
func selectionEvent(sessionID uuid.UUID, ev event.ActiveLoopChanged) event.ActiveLoopChanged {
	ev.Header = event.Header{Coordinates: identity.Coordinates{SessionID: sessionID}}
	return ev
}

// subscribeCore drives the subscription handshake on c: it runs the subscribe command,
// asserts the yielded subscribedMsg, and applies it (installing the stream and reading the
// authoritative active baseline). It is the shared setup the handshake tests reuse.
func subscribeCore(t *testing.T, c *sessionCore) {
	t.Helper()
	msg, ok := c.subscribe()().(subscribedMsg)
	if !ok {
		t.Fatalf("subscribe cmd yielded %T, want subscribedMsg", msg)
	}
	c.applySubscribed(msg)
}

// drainStream feeds every buffered delivery on s through the core's handleEvent, exactly as
// the continuous reader would drain the setup-window backlog after the stream is installed.
func drainStream(t *testing.T, c *sessionCore, s *fakeSubscription) {
	t.Helper()
	for {
		select {
		case d := <-s.Events():
			c.handleEvent(d.Event)
		default:
			return
		}
	}
}

// feedPrompt enqueues one pending prompt into the core's interaction surface so a test
// can assert /clear clears it. It routes a PermissionRequested through the core exactly
// as the live path would.
func feedPrompt(c sessionCore) sessionCore {
	c.interaction = c.interaction.ApplyEvent(event.PermissionRequested{ToolExecutionID: callID(5), Request: tool.BashRequest{Command: "x"}})
	return c
}
