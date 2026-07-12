package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/tool"
)

// This file holds the package-wide test fixtures — the scriptable Agent/Subscription
// doubles and the small pure helpers — shared by every *_test.go in package tui. They
// live here (not co-located with any one shell) because they are shell-independent: a
// fakeAgent drives sessionCore, Screen, and the reducer/render unit tests alike.

// compile-time assertion that the test double satisfies the (widened) Agent
// interface; if a method is added or its signature drifts, this fails to build.
var _ Agent = (*fakeAgent)(nil)

// fakeAgent is a scriptable Agent test double. It records calls and returns the
// configured reader/error/bool so shell behavior can be exercised without a real
// session.
type fakeAgent struct {
	// submit recorder: a configured id/error is returned, and the last call's
	// blocks are captured so a test can assert the wrapper forwarded them. When
	// submitID is zero it defaults to a fixed deterministic id so callers always get
	// a usable correlation id.
	submitID         uuid.UUID
	submitErr        error
	submitCalled     bool
	lastSubmitBlocks []content.Block

	// submitToLoop recorder: parallels the submit recorder for the loop-targeted
	// SubmitToLoop (the modern composer's focused-loop submit). It captures the target
	// loopID and blocks so a test can assert the composer routed to the FOCUSED loop, and
	// shares submitID/submitErr with Submit (same returned id/error contract).
	submitToLoopCalled     bool
	lastSubmitToLoopID     uuid.UUID
	lastSubmitToLoopBlocks []content.Block

	// rootLoopID is returned by RootLoopID — the stable root used for transcript
	// attribution; zero is a valid fixed id for the single-loop default. activeLoopID
	// is returned by ActiveLoopID (the current default input target) and DEFAULTS to
	// rootLoopID when left zero, so a test that sets only rootLoopID gets active == root
	// (the Task 2 compile-preserving default; Task 3-4 diverge them).
	rootLoopID   uuid.UUID
	activeLoopID uuid.UUID

	interruptCancelled bool
	interruptErr       error

	closeCalled  bool
	closeErr     error
	acceptsImage bool

	// subscribe recorder: the configured stream/error is returned, the last filter
	// is captured so a test can assert the wrapper forwarded it, and subscribeCount
	// counts Subscribe calls so a test can assert the TUI subscribes exactly ONCE per
	// session (one session-lifetime subscription, never re-subscribed per turn).
	subStream      event.Subscription
	subErr         error
	subFilter      event.EventFilter
	subscribeCount int

	// gate-trio recorders: the configured error is returned, and the last call's
	// arguments are captured so a test can assert the wrapper forwarded them.
	approveErr    error
	denyErr       error
	answerErr     error
	approveCalled bool
	denyCalled    bool
	answerCalled  bool
	lastLoopID    uuid.UUID
	lastCallID    uuid.UUID
	lastScope     tool.ApprovalScope
	lastAnswer    string

	// replay-backlog recorder: backlog is returned verbatim (a restored session's
	// historical Enduring events for repaint; nil for a NEW session), replayErr is the
	// configured failure, and replayCalled records that the restore seam was exercised.
	backlog      []event.Event
	replayErr    error
	replayCalled bool
}

// fixedFakeSubmitID is the deterministic InputID a fakeAgent returns when no
// submitID is configured, so a test always gets a non-zero correlation id.
var fixedFakeSubmitID = uuid.UUID{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00}

func (f *fakeAgent) Submit(_ context.Context, blocks []content.Block) (uuid.UUID, error) {
	f.submitCalled = true
	f.lastSubmitBlocks = blocks
	if f.submitErr != nil {
		return uuid.UUID{}, f.submitErr
	}
	if f.submitID.IsZero() {
		return fixedFakeSubmitID, nil
	}
	return f.submitID, nil
}

func (f *fakeAgent) SubmitToLoop(_ context.Context, loopID uuid.UUID, blocks []content.Block) (uuid.UUID, error) {
	f.submitToLoopCalled = true
	f.lastSubmitToLoopID = loopID
	f.lastSubmitToLoopBlocks = blocks
	if f.submitErr != nil {
		return uuid.UUID{}, f.submitErr
	}
	if f.submitID.IsZero() {
		return fixedFakeSubmitID, nil
	}
	return f.submitID, nil
}

func (f *fakeAgent) RootLoopID() uuid.UUID { return f.rootLoopID }

// ActiveLoopID returns the configured active loop, defaulting to the root when unset so
// common test constructors (which set only rootLoopID) get active == root.
func (f *fakeAgent) ActiveLoopID() uuid.UUID {
	if f.activeLoopID.IsZero() {
		return f.rootLoopID
	}
	return f.activeLoopID
}

func (f *fakeAgent) Interrupt(_ context.Context) (bool, error) {
	return f.interruptCancelled, f.interruptErr
}

func (f *fakeAgent) Close(_ context.Context) error {
	f.closeCalled = true
	return f.closeErr
}

func (f *fakeAgent) AcceptsImages() bool { return f.acceptsImage }

func (f *fakeAgent) Subscribe(filter event.EventFilter) (EventStream, error) {
	f.subscribeCount++
	f.subFilter = filter
	if f.subErr != nil {
		return nil, f.subErr
	}
	return f.subStream, nil
}

func (f *fakeAgent) Approve(_ context.Context, loopID, callID uuid.UUID, scope tool.ApprovalScope) error {
	f.approveCalled = true
	f.lastLoopID = loopID
	f.lastCallID = callID
	f.lastScope = scope
	return f.approveErr
}

func (f *fakeAgent) Deny(_ context.Context, loopID, callID uuid.UUID) error {
	f.denyCalled = true
	f.lastLoopID = loopID
	f.lastCallID = callID
	return f.denyErr
}

func (f *fakeAgent) ProvideAnswer(_ context.Context, loopID, callID uuid.UUID, answer string) error {
	f.answerCalled = true
	f.lastLoopID = loopID
	f.lastCallID = callID
	f.lastAnswer = answer
	return f.answerErr
}

func (f *fakeAgent) ReplayBacklog(_ context.Context) ([]event.Event, error) {
	f.replayCalled = true
	if f.replayErr != nil {
		return nil, f.replayErr
	}
	return f.backlog, nil
}

// fakeSubscription is a test-controlled event.Subscription: a buffered channel a
// test pushes events onto (push) plus an idempotent Close and a configurable Err.
// It models the session-lifetime stream a shell reads via subNext. The channel
// is buffered so push never blocks the test goroutine; closeErr is what Err reports
// after a hub-forced loss (nil mimics an intentional Close).
type fakeSubscription struct {
	ch       chan event.Delivery
	closeErr error
	closed   bool
}

// newFakeSubscription builds a fakeSubscription with a generously buffered channel
// so a test can stage several events without a reader draining them.
func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{ch: make(chan event.Delivery, 64)}
}

func (s *fakeSubscription) Events() <-chan event.Delivery { return s.ch }

// Close is the consumer's idempotent teardown: it closes the channel once so a
// subsequent subNext receives !ok. It records no error (Err stays whatever was set).
func (s *fakeSubscription) Close() error {
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
	return nil
}

func (s *fakeSubscription) Err() error { return s.closeErr }

// push stages an event on the subscription channel (non-blocking — the buffer is
// large enough for the tests). It panics if the channel is full so a test bug
// surfaces loudly rather than hanging.
func (s *fakeSubscription) push(ev event.Event) {
	select {
	case s.ch <- event.Delivery{Event: ev}:
	default:
		panic("fakeSubscription buffer full")
	}
}

// compile-time assertion that fakeSubscription satisfies the consumer contract.
var _ event.Subscription = (*fakeSubscription)(nil)

// fakeOpen returns an OpenAgent thunk that yields the given agent.
func fakeOpen(a Agent) OpenAgent {
	return func(context.Context) (Agent, error) { return a, nil }
}

// callID returns a deterministic non-zero UUID for a test, distinguishing gates by
// a single byte so ToolExecutionID correlation can be asserted.
func callID(b byte) uuid.UUID {
	var u uuid.UUID
	u[0] = b
	return u
}

// drainCmd executes cmd, recursively running any BatchMsg/sequenceMsg leaves it
// produces so the underlying I/O closures (e.g. submitCmd's agent.Submit call) all
// run. A nil cmd is a no-op. It is the test-side analogue of the Bubble Tea runtime
// fanning out a batched command.
func drainCmd(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drainCmd(t, c)
		}
	}
}

// firstBlockText returns the first text-block text in blocks, or "".
func firstBlockText(blocks []content.Block) string {
	for _, b := range blocks {
		if tb, ok := b.(*content.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}

// committedText returns the first text-block text of e, or "".
func committedText(e entry) string {
	for _, b := range e.Blocks {
		if tb, ok := b.(*content.TextBlock); ok {
			return tb.Text
		}
	}
	return ""
}
