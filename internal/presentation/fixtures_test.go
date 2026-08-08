package presentation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/gate"
	"github.com/looprig/harness/pkg/tool"
	contextcount "github.com/looprig/inference/contextcount"
	model "github.com/looprig/inference/model"
)

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// This file holds the package-wide test fixtures — the scriptable Agent/Subscription
// doubles and the small pure helpers — shared by every *_test.go in package tui. They
// live here (not co-located with any one shell) because they are shell-independent: a
// fakeAgent drives sessionCore, Screen, and the reducer/render unit tests alike.

func validContextMeasurement(input, limit content.TokenCount, revision event.ContextRevision, throughEventID uuid.UUID, fingerprint byte) event.ContextMeasurement {
	return event.ContextMeasurement{
		Basis: event.ContextBasis{
			Revision:       revision,
			ThroughEventID: throughEventID,
		},
		Model:              model.ModelKey{Provider: "provider", Model: "model"},
		RequestFingerprint: [32]byte{fingerprint},
		InputTokens:        input,
		InputLimit:         limit,
		Quality:            contextcount.CountQualityHeuristicEstimate,
	}
}

// compile-time assertion that the test double satisfies the (widened) Agent
// interface; if a method is added or its signature drifts, this fails to build.
var _ Agent = (*fakeAgent)(nil)

// fakeAgent is a scriptable Agent test double. It records calls and returns the
// configured reader/error/bool so shell behavior can be exercised without a real
// session.
type fakeAgent struct {
	// sessionID is the durable session identity rendered in the startup banner.
	// A zero value falls back to fixedFakeSessionID for tests unrelated to identity.
	sessionID uuid.UUID

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

	// compact recorder: CompactToLoop returns the configured id/error and captures
	// the exact loop plus command deadline so the bounded slash dispatch can be
	// tested without waiting for a real timeout.
	compactID          uuid.UUID
	compactErr         error
	compactCalled      bool
	lastCompactLoopID  uuid.UUID
	compactDeadline    time.Time
	compactTimeLeft    time.Duration
	compactHasDeadline bool

	// activeLoopID is the current default input target returned by ActiveLoopID.
	activeLoopID uuid.UUID

	interruptCancelled bool
	interruptErr       error

	closeCalled  bool
	closeCalls   int
	closeErr     error
	closeEntered chan struct{}
	closeRelease <-chan struct{}

	// acceptsImages is the per-loop image capability the widened AcceptsImages(loopID)
	// query reads: a loop absent from the map (or a nil map) reports false, modeling the
	// fail-closed contract for an unknown target. lastAcceptsImagesLoopID records the loop
	// the last query asked about, so a test can assert WHICH loop the submit path targeted
	// (the reconciled active loop for submit, the explicit focused loop for submitToLoop).
	acceptsImages           map[uuid.UUID]bool
	lastAcceptsImagesLoopID uuid.UUID

	// subscribe recorder: the configured stream/error is returned, the last filter
	// is captured so a test can assert the wrapper forwarded it, and subscribeCount
	// counts Subscribe calls so a test can assert the TUI subscribes exactly ONCE per
	// session (one session-lifetime subscription, never re-subscribed per turn).
	subStream      event.Subscription
	subErr         error
	subFilter      event.EventFilter
	subscribeCount int

	// subEnqueue is pushed onto subStream at the moment Subscribe is called — modeling
	// selections/turns that arrive during the subscription SETUP WINDOW (after the stream
	// exists but before applySubscribed reads the ActiveLoopID baseline), so a test can
	// prove the baseline/event reconciliation converges on the causally newest active id.
	subEnqueue []event.Event

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
	lastApproval  gate.ApprovalAction
	lastAnswer    string

	// form-gate recorder: the same capture-and-report shape for RespondGate.
	gateErr        error
	gateCalled     bool
	lastGateID     gate.ID
	lastGateAction string
	lastGateValues map[string]json.RawMessage

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
var fixedFakeSessionID = uuid.UUID{0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x47, 0x28, 0x89, 0x2a, 0x2b, 0x2c, 0x2d, 0x2e, 0x2f, 0x30}

func (f *fakeAgent) SessionID() uuid.UUID {
	if f.sessionID.IsZero() {
		return fixedFakeSessionID
	}
	return f.sessionID
}

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

func (f *fakeAgent) CompactToLoop(ctx context.Context, loopID uuid.UUID) (uuid.UUID, error) {
	f.compactCalled = true
	f.lastCompactLoopID = loopID
	f.compactDeadline, f.compactHasDeadline = ctx.Deadline()
	f.compactTimeLeft = time.Until(f.compactDeadline)
	if f.compactErr != nil {
		return uuid.UUID{}, f.compactErr
	}
	return f.compactID, nil
}

func (f *fakeAgent) ActiveLoopID() uuid.UUID {
	return f.activeLoopID
}

func (f *fakeAgent) Interrupt(_ context.Context) (bool, error) {
	return f.interruptCancelled, f.interruptErr
}

func (f *fakeAgent) Close(_ context.Context) error {
	f.closeCalled = true
	f.closeCalls++
	if f.closeEntered != nil {
		close(f.closeEntered)
		f.closeEntered = nil
	}
	if f.closeRelease != nil {
		<-f.closeRelease
	}
	return f.closeErr
}

// AcceptsImages records the queried loop and reports its configured capability, failing
// closed (false) for a loop absent from the map or a nil map — the unknown-target contract.
func (f *fakeAgent) AcceptsImages(loopID uuid.UUID) bool {
	f.lastAcceptsImagesLoopID = loopID
	return f.acceptsImages[loopID]
}

func (f *fakeAgent) Subscribe(filter event.EventFilter) (EventStream, error) {
	f.subscribeCount++
	f.subFilter = filter
	if f.subErr != nil {
		return nil, f.subErr
	}
	// Deliver the setup-window backlog onto the stream as it is created, so the events
	// are readable immediately after applySubscribed installs it (the race the baseline
	// reconciliation must tolerate).
	if fs, ok := f.subStream.(*fakeSubscription); ok {
		for _, ev := range f.subEnqueue {
			fs.push(ev)
		}
	}
	return f.subStream, nil
}

func (f *fakeAgent) Approve(_ context.Context, loopID, callID uuid.UUID, action gate.ApprovalAction) error {
	f.approveCalled = true
	f.lastLoopID = loopID
	f.lastCallID = callID
	f.lastApproval = action
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

func (f *fakeAgent) RespondGate(_ context.Context, gateID gate.ID, action string, values map[string]json.RawMessage) error {
	f.gateCalled = true
	f.lastGateID = gateID
	f.lastGateAction = action
	f.lastGateValues = values
	return f.gateErr
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

// toolRequest builds a typed prepared tool.Request fixture with the given tool name,
// one-line summary, and unmet requirements — the exact shape the per-turn stream
// delivers on event.PermissionRequested.Request. The TUI reads these fields verbatim
// (it never validates or reconstructs), so a test can stage any combination of
// requirements and candidates.
func toolRequest(toolName, summary string, requirements ...tool.Requirement) tool.Request {
	return tool.Request{ToolName: toolName, Summary: summary, Requirements: requirements}
}

// requirement builds one unmet requirement with a display description and the exact
// persisted rule-candidate descriptions an "always for this workspace" approval would
// save. Every string is display-ready — the values double as Match so the fixture is
// self-consistent.
func requirement(description string, candidates ...string) tool.Requirement {
	r := tool.Requirement{Kind: "capability", Match: description, Description: description}
	for _, c := range candidates {
		r.Candidates = append(r.Candidates, tool.RuleCandidate{Kind: "capability", Match: c, Description: c})
	}
	return r
}

// bashPermission is the common single-requirement Bash approval fixture used across
// the permission tests: tool "Bash", the command as summary, one command.execute
// requirement, and one reusable candidate.
func bashPermission(command string) tool.Request {
	return toolRequest("Bash", command, requirement("run "+command, "always allow "+command))
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
