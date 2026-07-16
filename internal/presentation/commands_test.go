package presentation

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/tool"
)

// TestSubNext covers the continuous reader: each staged event is delivered as an
// eventMsg in FIFO order, and a closed channel yields a subClosedMsg carrying the
// subscription's typed termination cause.
func TestSubNext(t *testing.T) {
	t.Parallel()

	sub := newFakeSubscription()
	sub.push(event.TurnStarted{})
	sub.push(event.TurnDone{Message: &content.AIMessage{}})

	// First receive yields the TurnStarted event.
	msg := subNext(sub)()
	ev, ok := msg.(eventMsg)
	if !ok {
		t.Fatalf("first msg = %T, want eventMsg", msg)
	}
	if _, ok := ev.ev.(event.TurnStarted); !ok {
		t.Errorf("first event = %T, want event.TurnStarted", ev.ev)
	}

	// Second receive yields the TurnDone event.
	msg = subNext(sub)()
	ev, ok = msg.(eventMsg)
	if !ok {
		t.Fatalf("second msg = %T, want eventMsg", msg)
	}
	if _, ok := ev.ev.(event.TurnDone); !ok {
		t.Errorf("second event = %T, want event.TurnDone", ev.ev)
	}
}

// TestSubNextClosed covers the reader's terminal: a closed channel yields a
// subClosedMsg carrying Err() — nil for an intentional Close, the typed loss error
// for a hub-forced drop.
func TestSubNextClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		closeErr error
		wantErr  bool
	}{
		{name: "intentional close yields nil err", closeErr: nil},
		{name: "loss yields the typed err", closeErr: errors.New("lost"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub := newFakeSubscription()
			sub.closeErr = tt.closeErr
			_ = sub.Close()

			msg := subNext(sub)()
			cm, ok := msg.(subClosedMsg)
			if !ok {
				t.Fatalf("msg = %T, want subClosedMsg", msg)
			}
			if (cm.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", cm.err != nil, tt.wantErr)
			}
		})
	}
}

// TestSubNextNilIsNoop guards the /clear re-subscribe window: a re-arm built from a
// transiently-nil m.sub must be a no-op (nil msg), never a nil-deref panic.
func TestSubNextNilIsNoop(t *testing.T) {
	t.Parallel()
	if msg := subNext(nil)(); msg != nil {
		t.Fatalf("subNext(nil)() = %v, want nil (no-op, no panic)", msg)
	}
}

// TestSubscribeWith covers the startup subscribe: it forwards the GIVEN filter (the
// all-loop AllLoopsEventFilter) and reports the outcome. On success the stream is carried
// on the msg; on error the err is carried and the stream is nil.
func TestSubscribeWith(t *testing.T) {
	t.Parallel()

	t.Run("success carries the stream and the all-loops filter", func(t *testing.T) {
		t.Parallel()
		sub := newFakeSubscription()
		agent := &fakeAgent{activeLoopID: callID(0x5A), subStream: sub}

		msg := subscribeWith(agent, AllLoopsEventFilter())()
		res, ok := msg.(subscribedMsg)
		if !ok {
			t.Fatalf("msg = %T, want subscribedMsg", msg)
		}
		if res.err != nil {
			t.Errorf("err = %v, want nil", res.err)
		}
		if res.sub != sub {
			t.Errorf("sub = %p, want %p", res.sub, sub)
		}
		// The forwarded filter is the all-loop scope: both classes deliver from every loop.
		if !agent.subFilter.Ephemeral.All {
			t.Error("subscribe filter Ephemeral.All = false, want true (every loop's live stream)")
		}
		if !agent.subFilter.Enduring.All {
			t.Error("subscribe filter Enduring.All = false, want true (every loop's enduring events)")
		}
	})

	t.Run("error carries the err and a nil stream", func(t *testing.T) {
		t.Parallel()
		agent := &fakeAgent{subErr: errors.New("no hub")}

		msg := subscribeWith(agent, AllLoopsEventFilter())()
		res, ok := msg.(subscribedMsg)
		if !ok {
			t.Fatalf("msg = %T, want subscribedMsg", msg)
		}
		if res.err == nil {
			t.Error("err = nil, want non-nil")
		}
		if res.sub != nil {
			t.Errorf("sub = %p, want nil on error", res.sub)
		}
	})
}

// TestSubmitCmd covers the fire-and-forget submit: blocks are forwarded to Submit
// and the result msg carries only the error (a nil err is a silent success).
func TestSubmitCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		submitErr error
		wantErr   bool
	}{
		{name: "success is silent", submitErr: nil},
		{name: "error surfaced", submitErr: errors.New("send failed"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := &fakeAgent{submitErr: tt.submitErr}
			blocks := []content.Block{&content.TextBlock{Text: "hi"}}
			msg := submitCmd(context.Background(), agent, blocks)()

			res, ok := msg.(submitResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want submitResultMsg", msg)
			}
			if (res.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", res.err != nil, tt.wantErr)
			}
			if !agent.submitCalled {
				t.Error("Submit not called")
			}
		})
	}
}

// TestSubmitToLoopCmd covers the loop-targeted fire-and-forget submit: blocks AND the
// target loopID are forwarded to SubmitToLoop, and the result msg carries only the error
// (a nil err is a silent success) in the SAME submitResultMsg shape as submitCmd.
func TestSubmitToLoopCmd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		submitErr error
		wantErr   bool
	}{
		{name: "success is silent", submitErr: nil},
		{name: "error surfaced", submitErr: errors.New("send failed"), wantErr: true},
	}

	loopID := callID(0x5A)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := &fakeAgent{submitErr: tt.submitErr}
			blocks := []content.Block{&content.TextBlock{Text: "hi"}}
			msg := submitToLoopCmd(context.Background(), agent, loopID, blocks)()

			res, ok := msg.(submitResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want submitResultMsg", msg)
			}
			if (res.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", res.err != nil, tt.wantErr)
			}
			if !agent.submitToLoopCalled {
				t.Error("SubmitToLoop not called")
			}
			if agent.lastSubmitToLoopID != loopID {
				t.Errorf("SubmitToLoop loopID = %v, want %v", agent.lastSubmitToLoopID, loopID)
			}
		})
	}
}

func TestCompactToLoopCmd(t *testing.T) {
	t.Parallel()

	wantID := callID(0x61)
	wantLoopID := callID(0x62)
	wantErr := errors.New("compact failed")
	tests := []struct {
		name      string
		agent     *fakeAgent
		wantID    uuid.UUID
		wantError error
	}{
		{name: "success preserves correlation id", agent: &fakeAgent{compactID: wantID}, wantID: wantID},
		{name: "failure preserves exact error", agent: &fakeAgent{compactErr: wantErr}, wantError: wantErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := compactToLoopCmd(context.Background(), tt.agent, wantLoopID)()
			res, ok := msg.(compactResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want compactResultMsg", msg)
			}
			if res.commandID != tt.wantID {
				t.Errorf("commandID = %v, want %v", res.commandID, tt.wantID)
			}
			if res.err != tt.wantError {
				t.Errorf("err = %v, want exact error %v", res.err, tt.wantError)
			}
			if !tt.agent.compactCalled {
				t.Fatal("CompactToLoop was not called")
			}
			if tt.agent.lastCompactLoopID != wantLoopID {
				t.Errorf("CompactToLoop loopID = %v, want %v", tt.agent.lastCompactLoopID, wantLoopID)
			}
			if !tt.agent.compactHasDeadline {
				t.Fatal("CompactToLoop context has no deadline")
			}
			if tt.agent.compactTimeLeft > 2*time.Second {
				t.Errorf("deadline remaining = %v, want at most %v", tt.agent.compactTimeLeft, 2*time.Second)
			}
		})
	}
}

func TestInterruptTurn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		agent         *fakeAgent
		wantCancelled bool
		wantErr       bool
	}{
		{
			name:          "cancelled true no error",
			agent:         &fakeAgent{interruptCancelled: true},
			wantCancelled: true,
			wantErr:       false,
		},
		{
			name:    "error surfaced",
			agent:   &fakeAgent{interruptErr: errors.New("interrupt failed")},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := interruptTurn(context.Background(), tt.agent)()
			res, ok := msg.(interruptResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want interruptResultMsg", msg)
			}
			if res.cancelled != tt.wantCancelled {
				t.Errorf("cancelled = %v, want %v", res.cancelled, tt.wantCancelled)
			}
			if (res.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", res.err != nil, tt.wantErr)
			}
		})
	}
}

func TestReopenAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		open      OpenAgent
		wantAgent bool
		wantErr   bool
	}{
		{
			name:      "success returns agent",
			open:      fakeOpen(&fakeAgent{}),
			wantAgent: true,
		},
		{
			name:    "error surfaced, nil agent",
			open:    func(context.Context) (Agent, error) { return nil, errors.New("open failed") },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			old := &fakeAgent{}
			msg := reopenAgent(context.Background(), old, tt.open, newReopenHandoff())()
			res, ok := msg.(reopenResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want reopenResultMsg", msg)
			}
			if (res.agent != nil) != tt.wantAgent {
				t.Errorf("agent != nil = %v, want %v", res.agent != nil, tt.wantAgent)
			}
			if (res.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", res.err != nil, tt.wantErr)
			}
			if !old.closeCalled {
				t.Error("old agent was not closed before replacement open")
			}
		})
	}
}

// TestReopenAgentClosesOldBeforeOpeningReplacement reproduces the exclusive-workspace
// handoff: the replacement opener rejects the call unless the old session has already
// released its lease via Close.
func TestReopenAgentClosesOldBeforeOpeningReplacement(t *testing.T) {
	t.Parallel()

	old := &fakeAgent{}
	fresh := &fakeAgent{}
	open := func(context.Context) (Agent, error) {
		if !old.closeCalled {
			return nil, errors.New("workspace lease still held")
		}
		return fresh, nil
	}

	msg := reopenAgent(context.Background(), old, open, newReopenHandoff())()
	res, ok := msg.(reopenResultMsg)
	if !ok {
		t.Fatalf("msg = %T, want reopenResultMsg", msg)
	}
	if res.err != nil {
		t.Fatalf("reopen error = %v", res.err)
	}
	if res.agent != fresh {
		t.Fatalf("agent = %p, want fresh %p", res.agent, fresh)
	}
	if old.closeCalls != 1 {
		t.Fatalf("old Close calls = %d, want exactly 1", old.closeCalls)
	}
}

// TestReopenAgentKeepsSuccessfulOpenerContextAlive reproduces a /clear failure where
// the product derives the replacement session's Loop lifetime from the OpenAgent context.
// A successful reopen must therefore keep that context live until the application context
// itself is cancelled; cancelling a construction-only child on return kills the fresh loop
// and makes the next submit fail with "session: loop exited".
func TestReopenAgentKeepsSuccessfulOpenerContextAlive(t *testing.T) {
	t.Parallel()

	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	old := &fakeAgent{}
	fresh := &fakeAgent{}
	var openerCtx context.Context

	res := reopenAgent(appCtx, old, func(ctx context.Context) (Agent, error) {
		openerCtx = ctx
		return fresh, nil
	}, newReopenHandoff())().(reopenResultMsg)
	if res.err != nil || res.agent != fresh {
		t.Fatalf("reopen = (agent %p, err %v), want fresh %p with nil error", res.agent, res.err, fresh)
	}
	select {
	case <-openerCtx.Done():
		t.Fatalf("successful opener context already cancelled: %v", openerCtx.Err())
	default:
	}

	cancelApp()
	select {
	case <-openerCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("opener context did not follow application cancellation")
	}
}

// TestReopenAgentClosesPartialReplacementOnError guards the opener contract defensively:
// if construction returns both an agent and an error, the unusable partial replacement must
// be closed before the terminal error is delivered.
func TestReopenAgentClosesPartialReplacementOnError(t *testing.T) {
	t.Parallel()

	old := &fakeAgent{}
	partialCloseErr := errors.New("partial close failed")
	partial := &fakeAgent{closeErr: partialCloseErr}
	openErr := errors.New("partial open failed")
	open := func(context.Context) (Agent, error) { return partial, openErr }

	res := reopenAgent(context.Background(), old, open, newReopenHandoff())().(reopenResultMsg)
	if !errors.Is(res.err, openErr) {
		t.Fatalf("reopen error = %v, want %v", res.err, openErr)
	}
	if !errors.Is(res.err, partialCloseErr) {
		t.Fatalf("reopen error = %v, want partial cleanup error %v", res.err, partialCloseErr)
	}
	if partial.closeCalls != 1 {
		t.Fatalf("partial replacement Close calls = %d, want exactly 1", partial.closeCalls)
	}
}

// TestReopenAgentPreservesErrorFromTypedNilReplacement reproduces a Go interface edge
// case at the session-browser boundary: a concrete nil *Agent converted to Agent is not
// itself nil. Failed construction must not call Close on that unusable value and mask the
// original restore error with a panic.
func TestReopenAgentPreservesErrorFromTypedNilReplacement(t *testing.T) {
	t.Parallel()

	old := &fakeAgent{}
	restoreErr := errors.New("session: restore config mismatch")
	var absent *fakeAgent
	open := func(context.Context) (Agent, error) { return absent, restoreErr }

	res := reopenAgent(context.Background(), old, open, newReopenHandoff())().(reopenResultMsg)
	if !errors.Is(res.err, restoreErr) {
		t.Fatalf("reopen error = %v, want original restore error %v", res.err, restoreErr)
	}
	if res.agent != nil {
		t.Fatalf("replacement agent = %T, want nil", res.agent)
	}
	if old.closeCalls != 1 {
		t.Errorf("old Close calls = %d, want exactly 1", old.closeCalls)
	}
}

// TestReopenAgentCloseFailureStopsBeforeOpen pins the first half of the ownership handoff:
// one failed Close attempt is wrapped and returned, and the replacement opener is never called.
func TestReopenAgentCloseFailureStopsBeforeOpen(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("shutdown failed")
	old := &fakeAgent{closeErr: closeErr}
	var openerCalled bool
	open := func(context.Context) (Agent, error) {
		openerCalled = true
		return &fakeAgent{}, nil
	}

	res := reopenAgent(context.Background(), old, open, newReopenHandoff())().(reopenResultMsg)
	if !errors.Is(res.err, closeErr) {
		t.Fatalf("reopen error = %v, want wrapped %v", res.err, closeErr)
	}
	if res.err == closeErr {
		t.Error("reopen returned bare close error, want ownership-handoff context")
	}
	if res.agent != nil {
		t.Fatalf("replacement agent = %T, want nil", res.agent)
	}
	if openerCalled {
		t.Error("replacement opener called after old Close failure")
	}
	if old.closeCalls != 1 {
		t.Errorf("old Close calls = %d, want exactly 1", old.closeCalls)
	}
}

func TestCloseAgent(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{}
	msg := closeAgent(agent)()
	if msg != nil {
		t.Errorf("closeAgent msg = %v, want nil", msg)
	}
	if !agent.closeCalled {
		t.Error("closeAgent did not call agent.Close")
	}
}

// NOTE: TestPrintPayload / TestPrintPayloadReadOnly / TestPrintToScrollback were
// removed with the scrollback Screen — printPayload/printToScrollback and the
// printAction type were exclusive to that shell's native-scrollback flush and were
// archived alongside it (see ../archive/cli-oldscreen).

// TestApproveCmd covers the bounded approve dispatch: every allowed scope is
// forwarded verbatim with the call ID, and a configured error surfaces on the
// result msg (non-fatal). errScope is a sentinel for the error path.
func TestApproveCmd(t *testing.T) {
	t.Parallel()

	errApprove := errors.New("approve failed")

	tests := []struct {
		name       string
		loopID     uuid.UUID
		callID     uuid.UUID
		scope      tool.ApprovalScope
		approveErr error
		wantErr    bool
	}{
		{name: "once succeeds", loopID: callID(10), callID: callID(1), scope: tool.ScopeOnce},
		{name: "session succeeds", loopID: callID(20), callID: callID(2), scope: tool.ScopeSession},
		{name: "workspace succeeds", loopID: callID(30), callID: callID(3), scope: tool.ScopeWorkspace},
		{name: "error surfaced", loopID: callID(40), callID: callID(4), scope: tool.ScopeOnce, approveErr: errApprove, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := &fakeAgent{approveErr: tt.approveErr}
			msg := approveCmd(context.Background(), agent, tt.loopID, tt.callID, tt.scope)()

			res, ok := msg.(promptResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want promptResultMsg", msg)
			}
			if (res.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", res.err != nil, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(res.err, errApprove) {
				t.Errorf("err = %v, want %v", res.err, errApprove)
			}
			if agent.lastLoopID != tt.loopID {
				t.Errorf("recorded loopID = %v, want %v", agent.lastLoopID, tt.loopID)
			}
			if agent.lastCallID != tt.callID {
				t.Errorf("recorded callID = %v, want %v", agent.lastCallID, tt.callID)
			}
			if agent.lastScope != tt.scope {
				t.Errorf("recorded scope = %v, want %v", agent.lastScope, tt.scope)
			}
		})
	}
}

// TestDenyCmd covers the bounded deny dispatch: the call ID is forwarded and a
// configured error surfaces on the result msg (non-fatal).
func TestDenyCmd(t *testing.T) {
	t.Parallel()

	errDeny := errors.New("deny failed")

	tests := []struct {
		name    string
		loopID  uuid.UUID
		callID  uuid.UUID
		denyErr error
		wantErr bool
	}{
		{name: "deny succeeds", loopID: callID(10), callID: callID(1)},
		{name: "error surfaced", loopID: callID(20), callID: callID(2), denyErr: errDeny, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := &fakeAgent{denyErr: tt.denyErr}
			msg := denyCmd(context.Background(), agent, tt.loopID, tt.callID)()

			res, ok := msg.(promptResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want promptResultMsg", msg)
			}
			if (res.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", res.err != nil, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(res.err, errDeny) {
				t.Errorf("err = %v, want %v", res.err, errDeny)
			}
			if agent.lastLoopID != tt.loopID {
				t.Errorf("recorded loopID = %v, want %v", agent.lastLoopID, tt.loopID)
			}
			if agent.lastCallID != tt.callID {
				t.Errorf("recorded callID = %v, want %v", agent.lastCallID, tt.callID)
			}
		})
	}
}

// TestProvideAnswerCmd covers the bounded answer dispatch: the call ID and answer
// are forwarded verbatim and a configured error surfaces on the result msg.
func TestProvideAnswerCmd(t *testing.T) {
	t.Parallel()

	errAnswer := errors.New("answer failed")

	tests := []struct {
		name      string
		loopID    uuid.UUID
		callID    uuid.UUID
		answer    string
		answerErr error
		wantErr   bool
	}{
		{name: "answer succeeds", loopID: callID(10), callID: callID(1), answer: "yes, proceed"},
		{name: "empty answer forwarded", loopID: callID(20), callID: callID(2), answer: ""},
		{name: "error surfaced", loopID: callID(30), callID: callID(3), answer: "x", answerErr: errAnswer, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := &fakeAgent{answerErr: tt.answerErr}
			msg := provideAnswerCmd(context.Background(), agent, tt.loopID, tt.callID, tt.answer)()

			res, ok := msg.(promptResultMsg)
			if !ok {
				t.Fatalf("msg = %T, want promptResultMsg", msg)
			}
			if (res.err != nil) != tt.wantErr {
				t.Errorf("err != nil = %v, want %v", res.err != nil, tt.wantErr)
			}
			if tt.wantErr && !errors.Is(res.err, errAnswer) {
				t.Errorf("err = %v, want %v", res.err, errAnswer)
			}
			if agent.lastLoopID != tt.loopID {
				t.Errorf("recorded loopID = %v, want %v", agent.lastLoopID, tt.loopID)
			}
			if agent.lastCallID != tt.callID {
				t.Errorf("recorded callID = %v, want %v", agent.lastCallID, tt.callID)
			}
			if agent.lastAnswer != tt.answer {
				t.Errorf("recorded answer = %q, want %q", agent.lastAnswer, tt.answer)
			}
		})
	}
}

// TestPromptDispatchBounded asserts the dispatch cmds honor a cancelled parent
// context by returning promptly rather than blocking — the bounded-ctx guarantee
// that keeps the Update loop from wedging on a stuck send. Each cmd still returns
// a promptResultMsg (never panics).
func TestPromptDispatchBounded(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already-cancelled parent: the bounded child inherits cancellation

	// Each goroutine gets its own agent: this test asserts only that the cmds
	// return promptly, and a shared fake's recorder fields would race.
	done := make(chan tea.Msg, 3)
	go func() { done <- approveCmd(ctx, &fakeAgent{}, callID(10), callID(1), tool.ScopeOnce)() }()
	go func() { done <- denyCmd(ctx, &fakeAgent{}, callID(20), callID(2))() }()
	go func() { done <- provideAnswerCmd(ctx, &fakeAgent{}, callID(30), callID(3), "a")() }()

	for i := 0; i < 3; i++ {
		select {
		case msg := <-done:
			if _, ok := msg.(promptResultMsg); !ok {
				t.Errorf("msg = %T, want promptResultMsg", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("dispatch cmd did not return promptly under a cancelled context")
		}
	}
}
