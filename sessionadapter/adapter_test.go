package sessionadapter

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/gate"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/journal"
	"github.com/looprig/harness/pkg/loop"
	"github.com/looprig/harness/pkg/sessionstore"
	"github.com/looprig/harness/pkg/workspacestore"
	model "github.com/looprig/inference/model"
	"github.com/looprig/tui"
)

func TestSessionAdapterHasNoRootLoopContract(t *testing.T) {
	if _, ok := reflect.TypeOf((*sessionAdapter)(nil)).MethodByName("RootLoopID"); ok {
		t.Fatal("sessionAdapter still exposes RootLoopID")
	}
}

// Compile-time proof that *sessionAdapter satisfies the CLI's tui.Agent surface.
var _ tui.Agent = (*sessionAdapter)(nil)

// errNoReplay is the sentinel a new-session fake replay opener returns; newSessionAdapter
// ignores it for a NEW session (only a restore requires the replayer).
var errNoReplay = errors.New("no replay for a new session")

func testModel() model.Model {
	return model.Model{
		Provider:  model.ProviderName("test"),
		APIFormat: model.APIFormat("test"),
		Name:      "fake-model",
		Limits:    model.ContextLimits{WindowTokens: 128_000},
	}
}

// fakeReplayOpener returns no replayer, exercising the new-session path (ReplayBacklog nil).
type fakeReplayOpener struct{}

func (fakeReplayOpener) OpenEventReplayer(uuid.UUID, sessionstore.ReplayRequest) (journal.EventReplayer, error) {
	return nil, errNoReplay
}

type scriptedReplayOpener struct {
	replayer journal.EventReplayer
	err      error
}

type replayInitError struct{ message string }

func (e *replayInitError) Error() string { return e.message }

func (s scriptedReplayOpener) OpenEventReplayer(uuid.UUID, sessionstore.ReplayRequest) (journal.EventReplayer, error) {
	return s.replayer, s.err
}

type scriptedEventReplayer struct {
	cursor  journal.EventCursor
	openErr error
}

func (s scriptedEventReplayer) Open(context.Context, journal.ReplayRequest) (journal.EventCursor, error) {
	return s.cursor, s.openErr
}

type scriptedEventCursor struct {
	events  []event.Event
	index   int
	nextErr error
}

func (s *scriptedEventCursor) Next(context.Context) (event.Event, uint64, error) {
	if s.index < len(s.events) {
		ev := s.events[s.index]
		s.index++
		return ev, uint64(s.index), nil
	}
	if s.nextErr == nil {
		return nil, 0, io.EOF
	}
	return nil, 0, s.nextErr
}
func (*scriptedEventCursor) Close() error { return nil }

// fakeHandle is a minimal loop.Handle: an id + a model whose caps drive AcceptsImages.
type fakeHandle struct {
	id    uuid.UUID
	model model.Model
}

func (h *fakeHandle) ID() uuid.UUID       { return h.id }
func (h *fakeHandle) Mode() loop.ModeName { return "" }
func (h *fakeHandle) Model() model.Model  { return h.model }

// fakeSub is a controllable event.Subscription: the test feeds deliveries on ch.
type fakeSub struct {
	ch    chan event.Delivery
	once  sync.Once
	errMu sync.Mutex
	err   error
}

func newFakeSub() *fakeSub { return &fakeSub{ch: make(chan event.Delivery, 8)} }

func (s *fakeSub) Events() <-chan event.Delivery { return s.ch }
func (s *fakeSub) Close() error {
	s.once.Do(func() { close(s.ch) })
	return nil
}
func (s *fakeSub) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}
func (s *fakeSub) fail(err error) {
	s.errMu.Lock()
	s.err = err
	s.errMu.Unlock()
	s.once.Do(func() { close(s.ch) })
}

// fakeController is a fake session.SessionController for the adapter tests: it records gate
// responses + shutdowns and returns configured loops. The methods the adapter never exercises
// return benign zero values.
type fakeController struct {
	sessionID uuid.UUID
	active    *fakeHandle
	loops     map[uuid.UUID]*fakeHandle
	sub       *fakeSub

	mu                  sync.Mutex
	compactTargets      []uuid.UUID
	compactResult       uuid.UUID
	compactErr          error
	gotGate             []gate.GateResponse
	shutdowns           int
	shutdownErr         error
	shutdownCtxErr      error
	shutdownDeadline    time.Time
	shutdownHasDeadline bool
	subscribeQueue      []event.Subscription
	subscribeErrors     []error
	subscribeCalls      int
}

func (f *fakeController) SessionID() uuid.UUID    { return f.sessionID }
func (f *fakeController) ActiveLoop() loop.Handle { return f.active }
func (f *fakeController) Loop(id uuid.UUID) (loop.Handle, bool) {
	h, ok := f.loops[id]
	if !ok {
		return nil, false
	}
	return h, true
}
func (f *fakeController) Submit(context.Context, []content.Block) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (f *fakeController) SubmitToLoop(context.Context, uuid.UUID, []content.Block) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (f *fakeController) Compact(context.Context) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (f *fakeController) CompactToLoop(_ context.Context, loopID uuid.UUID) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.compactTargets = append(f.compactTargets, loopID)
	return f.compactResult, f.compactErr
}
func (f *fakeController) SubscribeEvents(event.EventFilter) (event.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := f.subscribeCalls
	f.subscribeCalls++
	if index < len(f.subscribeErrors) && f.subscribeErrors[index] != nil {
		return nil, f.subscribeErrors[index]
	}
	if index < len(f.subscribeQueue) {
		return f.subscribeQueue[index], nil
	}
	return f.sub, nil
}
func (f *fakeController) RespondGate(_ context.Context, r gate.GateResponse) error {
	f.mu.Lock()
	f.gotGate = append(f.gotGate, r)
	f.mu.Unlock()
	return nil
}
func (f *fakeController) Interrupt(context.Context) (bool, error) { return false, nil }
func (f *fakeController) SetActiveLoop(_ context.Context, id uuid.UUID) error {
	if h, ok := f.loops[id]; ok {
		f.active = h
		return nil
	}
	return errNoReplay
}
func (f *fakeController) LoopController(uuid.UUID) (loop.Controller, bool) { return nil, false }
func (f *fakeController) CheckpointWorkspace(context.Context) (workspacestore.Ref, error) {
	return "", nil
}
func (f *fakeController) RestoreWorkspace(context.Context, workspacestore.Ref) error { return nil }
func (f *fakeController) Shutdown(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdowns++
	f.shutdownCtxErr = ctx.Err()
	f.shutdownDeadline, f.shutdownHasDeadline = ctx.Deadline()
	return f.shutdownErr
}

func (f *fakeController) responses() []gate.GateResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]gate.GateResponse(nil), f.gotGate...)
}

func (f *fakeController) compactCalls() []uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uuid.UUID(nil), f.compactTargets...)
}

func (f *fakeController) shutdownState() (int, time.Time, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdowns, f.shutdownDeadline, f.shutdownHasDeadline, f.shutdownCtxErr
}

func (f *fakeController) subscriptionCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subscribeCalls
}

// mustUUID mints a uuid or fails the test.
func mustUUID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.New()
	if err != nil {
		t.Fatalf("uuid.New() error = %v", err)
	}
	return id
}

func TestSessionAdapterInitializationFailureShutsDownController(t *testing.T) {
	t.Parallel()

	shutdownErr := errors.New("shutdown failed")
	tests := []struct {
		name    string
		primary error
		opener  func(error) replayOpener
	}{
		{
			name:    "event replayer acquisition",
			primary: &replayInitError{message: "open event replayer failed"},
			opener: func(primary error) replayOpener {
				return scriptedReplayOpener{err: primary}
			},
		},
		{
			name:    "replay cursor open",
			primary: &replayInitError{message: "open replay cursor failed"},
			opener: func(primary error) replayOpener {
				return scriptedReplayOpener{replayer: scriptedEventReplayer{openErr: primary}}
			},
		},
		{
			name:    "replay cursor drain",
			primary: &replayInitError{message: "drain replay cursor failed"},
			opener: func(primary error) replayOpener {
				return scriptedReplayOpener{replayer: scriptedEventReplayer{
					cursor: &scriptedEventCursor{nextErr: primary},
				}}
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fc := &fakeController{sessionID: mustUUID(t), shutdownErr: shutdownErr}
			callerCtx, cancel := context.WithCancel(context.Background())
			cancel()

			a, err := newSessionAdapter(callerCtx, fc, tt.opener(tt.primary), true)
			if a != nil {
				t.Fatalf("newSessionAdapter() agent = %p, want nil on initialization failure", a)
			}
			if !errors.Is(err, tt.primary) || !errors.Is(err, shutdownErr) {
				t.Fatalf("newSessionAdapter() error = %v, want primary + shutdown errors", err)
			}
			var typedPrimary *replayInitError
			if !errors.As(err, &typedPrimary) || typedPrimary != tt.primary {
				t.Fatalf("newSessionAdapter() error = %v, want typed primary discoverable with errors.As", err)
			}
			shutdowns, deadline, hasDeadline, shutdownCtxErr := fc.shutdownState()
			if shutdowns != 1 {
				t.Fatalf("Shutdown calls = %d, want exactly 1", shutdowns)
			}
			if shutdownCtxErr != nil {
				t.Fatalf("Shutdown context error = %v, want live bounded background context", shutdownCtxErr)
			}
			if !hasDeadline {
				t.Fatal("Shutdown context has no deadline, want bounded cleanup")
			}
			if remaining := time.Until(deadline); remaining <= 0 || remaining > sessionAdapterInitShutdownTimeout {
				t.Fatalf("Shutdown deadline remaining = %v, want within (0, %v]", remaining, sessionAdapterInitShutdownTimeout)
			}
		})
	}
}

// newFakeAgent builds a *sessionAdapter over a fakeController with two loops (active + one
// other), exercising the NEW-session construction path.
func newFakeAgent(t *testing.T) (*sessionAdapter, *fakeController, uuid.UUID, uuid.UUID) {
	t.Helper()
	rootID := mustUUID(t)
	otherID := mustUUID(t)
	imageModel := testModel()
	imageModel.Caps.AcceptsImages = true
	fc := &fakeController{
		sessionID: mustUUID(t),
		active:    &fakeHandle{id: rootID, model: testModel()},
		sub:       newFakeSub(),
		loops: map[uuid.UUID]*fakeHandle{
			rootID:  {id: rootID, model: testModel()},
			otherID: {id: otherID, model: imageModel},
		},
	}
	a, err := newSessionAdapter(context.Background(), fc, fakeReplayOpener{}, false)
	if err != nil {
		t.Fatalf("newSessionAdapter() error = %v", err)
	}
	return a, fc, rootID, otherID
}

// gateOpened fabricates a GateOpened for loopID with the given tool-execution id and gate id.
func gateOpened(loopID, toolExecID, gateID uuid.UUID) event.GateOpened {
	return event.GateOpened{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}},
		Gate:   gate.Gate{ID: gateID, Subject: gate.Subject{ToolExecutionID: toolExecID}},
	}
}

func TestActiveLoopIDTracksControllerSelection(t *testing.T) {
	t.Parallel()
	a, fc, rootID, otherID := newFakeAgent(t)

	if got := a.ActiveLoopID(); got != rootID {
		t.Fatalf("ActiveLoopID() = %v, want the active primer %v", got, rootID)
	}
	if err := fc.SetActiveLoop(context.Background(), otherID); err != nil {
		t.Fatalf("SetActiveLoop error = %v", err)
	}
	if got := a.ActiveLoopID(); got != otherID {
		t.Errorf("ActiveLoopID() = %v, want %v after switch", got, otherID)
	}
}

// TestAcceptsImagesPerLoopFailsClosed proves AcceptsImages queries the CURRENT model of the
// named loop and fails closed for an unknown loop.
func TestAcceptsImagesPerLoopFailsClosed(t *testing.T) {
	t.Parallel()
	a, _, rootID, imageLoop := newFakeAgent(t)

	tests := []struct {
		name string
		loop uuid.UUID
		want bool
	}{
		{name: "text-only loop", loop: rootID, want: false},
		{name: "image-capable loop", loop: imageLoop, want: true},
		{name: "unknown loop fails closed", loop: mustUUID(t), want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := a.AcceptsImages(tt.loop); got != tt.want {
				t.Errorf("AcceptsImages(%v) = %v, want %v", tt.loop, got, tt.want)
			}
		})
	}
}

type compactTestError struct{ message string }

func (e *compactTestError) Error() string { return e.message }

func TestCompactToLoopForwardsExactTargetAndResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    func(*testing.T, uuid.UUID, uuid.UUID) uuid.UUID
		result    func(*testing.T) uuid.UUID
		resultErr error
	}{
		{
			name:   "selected non-active loop and command id",
			target: func(_ *testing.T, _ uuid.UUID, other uuid.UUID) uuid.UUID { return other },
			result: mustUUID,
		},
		{
			name:      "controller error identity",
			target:    func(_ *testing.T, active uuid.UUID, _ uuid.UUID) uuid.UUID { return active },
			result:    mustUUID,
			resultErr: &compactTestError{message: "compaction unavailable"},
		},
		{
			name:   "zero target is forwarded without active fallback",
			target: func(_ *testing.T, _ uuid.UUID, _ uuid.UUID) uuid.UUID { return uuid.UUID{} },
			result: mustUUID,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a, controller, active, other := newFakeAgent(t)
			target := tt.target(t, active, other)
			controller.compactResult = tt.result(t)
			controller.compactErr = tt.resultErr

			got, err := a.CompactToLoop(context.Background(), target)
			if err != tt.resultErr {
				t.Fatalf("CompactToLoop() error = %v, want identity %v", err, tt.resultErr)
			}
			if got != controller.compactResult {
				t.Errorf("CompactToLoop() id = %v, want %v", got, controller.compactResult)
			}
			if calls := controller.compactCalls(); len(calls) != 1 || calls[0] != target {
				t.Errorf("controller targets = %v, want exactly [%v]", calls, target)
			}
		})
	}
}

// TestGateIndexResolvesPerLoopAndCall proves the (loopID, toolExecutionID)→GateID index: the
// SAME tool-execution id in two loops resolves to two DISTINCT gate ids, an unknown (loop,
// call) fails secure with *GateNotOpenError, and a GateResolved removes the entry.
func TestGateIndexResolvesPerLoopAndCall(t *testing.T) {
	t.Parallel()
	a, fc, loop1, loop2 := newFakeAgent(t)

	call := mustUUID(t) // the SAME tool-execution id in both loops
	gate1 := mustUUID(t)
	gate2 := mustUUID(t)
	a.foldGate(gateOpened(loop1, call, gate1))
	a.foldGate(gateOpened(loop2, call, gate2))

	if err := a.Approve(context.Background(), loop1, call, gate.ApprovalApprove); err != nil {
		t.Fatalf("Approve(loop1) error = %v", err)
	}
	if err := a.Approve(context.Background(), loop2, call, gate.ApprovalApproveAlwaysWorkspace); err != nil {
		t.Fatalf("Approve(loop2) error = %v", err)
	}
	got := fc.responses()
	if len(got) != 2 {
		t.Fatalf("RespondGate called %d times, want 2", len(got))
	}
	if got[0].GateID != gate1 || got[1].GateID != gate2 {
		t.Errorf("gate ids resolved = %v,%v, want %v,%v (same call in two loops keys two gates)", got[0].GateID, got[1].GateID, gate1, gate2)
	}
	// The exact gate.ApprovalAction strings reach the wire, in the clear and with no
	// values (the combined prompt carries no scope payload).
	if got[0].Action != string(gate.ApprovalApprove) || len(got[0].Values) != 0 {
		t.Errorf("response[0] = {Action:%q Values:%v}, want %q with no values", got[0].Action, got[0].Values, gate.ApprovalApprove)
	}
	if got[1].Action != string(gate.ApprovalApproveAlwaysWorkspace) {
		t.Errorf("response[1] Action = %q, want %q", got[1].Action, gate.ApprovalApproveAlwaysWorkspace)
	}

	// An unknown (loop, call) fails secure — and Deny sends the exact deny action.
	var notOpen *GateNotOpenError
	if err := a.Deny(context.Background(), mustUUID(t), call); !errors.As(err, &notOpen) {
		t.Errorf("Deny(unknown loop) error = %v, want *GateNotOpenError", err)
	}
	if err := a.Deny(context.Background(), loop2, call); err != nil {
		t.Fatalf("Deny(loop2) error = %v", err)
	}
	if denies := fc.responses(); denies[len(denies)-1].Action != string(gate.ApprovalDeny) {
		t.Errorf("Deny Action = %q, want %q", denies[len(denies)-1].Action, gate.ApprovalDeny)
	}

	// A GateResolved removes the entry: a later Approve fails secure.
	a.foldGate(event.GateResolved{GateID: gate1})
	if err := a.Approve(context.Background(), loop1, call, gate.ApprovalApprove); !errors.As(err, &notOpen) {
		t.Errorf("Approve after GateResolved error = %v, want *GateNotOpenError", err)
	}
}

// TestSubscribeFoldsGatesBeforeForwarding proves Subscribe returns one wrapping subscription
// that folds a GateOpened into the index BEFORE forwarding the event, and that Close tears the
// forwarder down cleanly.
func TestSubscribeFoldsGatesBeforeForwarding(t *testing.T) {
	t.Parallel()
	a, fc, loop1, _ := newFakeAgent(t)

	stream, err := a.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe error = %v", err)
	}

	call := mustUUID(t)
	gateID := mustUUID(t)
	fc.sub.ch <- event.Delivery{Event: gateOpened(loop1, call, gateID)}

	// Receiving the forwarded delivery guarantees the fold ran first (fold-before-forward).
	select {
	case d := <-stream.Events():
		if _, ok := d.Event.(event.GateOpened); !ok {
			t.Fatalf("forwarded event type = %T, want GateOpened", d.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("never received the forwarded GateOpened")
	}

	// The index was updated before the event surfaced: Approve resolves the folded gate.
	if err := a.Approve(context.Background(), loop1, call, gate.ApprovalApprove); err != nil {
		t.Fatalf("Approve after fold error = %v", err)
	}
	if got := fc.responses(); len(got) != 1 || got[0].GateID != gateID {
		t.Errorf("RespondGate = %+v, want one response with gate %v", got, gateID)
	}

	if err := stream.Close(); err != nil {
		t.Errorf("stream Close error = %v", err)
	}
}

type visibilityCase struct {
	name       string
	ev         event.Event
	wantPublic bool
}

type visibilityFixture struct {
	cases           []visibilityCase
	internalGateKey gateKey
	publicGateKey   gateKey
	publicGateID    gate.ID
}

func newVisibilityFixture(t *testing.T, loopID uuid.UUID) visibilityFixture {
	t.Helper()
	publicHeader := func() event.Header {
		return event.Header{
			Coordinates: identity.Coordinates{LoopID: loopID},
			EventID:     mustUUID(t),
		}
	}
	internalHeader := func() event.Header {
		header := publicHeader()
		header.EventVisibility = event.Internal
		return header
	}
	internalCall, internalGateID := mustUUID(t), mustUUID(t)
	publicCall, publicGateID := mustUUID(t), mustUUID(t)
	internalGate := gateOpened(loopID, internalCall, internalGateID)
	internalGate.Header = internalHeader()
	publicGate := gateOpened(loopID, publicCall, publicGateID)
	publicGate.Header = publicHeader()
	return visibilityFixture{
		cases: []visibilityCase{
			{name: "internal hustle started", ev: event.HustleStarted{Header: internalHeader()}},
			{name: "internal hustle completed", ev: event.HustleCompleted{Header: internalHeader()}},
			{name: "internal hustle failed", ev: event.HustleFailed{Header: internalHeader()}},
			{name: "public compaction committed", ev: event.CompactionCommitted{Header: publicHeader()}, wantPublic: true},
			{name: "public compaction rejected", ev: event.CompactionRejected{Header: publicHeader()}, wantPublic: true},
			{name: "unknown visibility", ev: event.SessionStarted{Header: event.Header{EventID: mustUUID(t), EventVisibility: event.EventVisibility(99)}}},
			{name: "internal gate before fold", ev: internalGate},
			{name: "public gate before fold", ev: publicGate, wantPublic: true},
		},
		internalGateKey: gateKey{loopID: loopID, toolExecutionID: internalCall},
		publicGateKey:   gateKey{loopID: loopID, toolExecutionID: publicCall},
		publicGateID:    publicGateID,
	}
}

func publicEventIDs(cases []visibilityCase) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(cases))
	for _, testCase := range cases {
		if testCase.wantPublic {
			ids = append(ids, testCase.ev.EventHeader().EventID)
		}
	}
	return ids
}

func eventIDs(events []event.Event) []uuid.UUID {
	ids := make([]uuid.UUID, len(events))
	for index, ev := range events {
		ids[index] = ev.EventHeader().EventID
	}
	return ids
}

func assertVisibilityGateFold(t *testing.T, a *sessionAdapter, fixture visibilityFixture) {
	t.Helper()
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.forward[fixture.internalGateKey]; ok {
		t.Errorf("internal gate %v was folded before visibility filtering", fixture.internalGateKey)
	}
	if got, ok := a.forward[fixture.publicGateKey]; !ok || got != fixture.publicGateID {
		t.Errorf("public gate fold = %v,%v, want %v,true", got, ok, fixture.publicGateID)
	}
}

func TestSubscribeFiltersVisibilityBeforeGateFoldAndDelivery(t *testing.T) {
	t.Parallel()
	a, controller, loopID, _ := newFakeAgent(t)
	fixture := newVisibilityFixture(t, loopID)

	stream, err := a.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	for _, testCase := range fixture.cases {
		controller.sub.ch <- event.Delivery{Event: testCase.ev}
	}
	if err := controller.sub.Close(); err != nil {
		t.Fatalf("inner subscription Close() error = %v", err)
	}

	var got []event.Event
	for delivery := range stream.Events() {
		got = append(got, delivery.Event)
	}
	if want := publicEventIDs(fixture.cases); !reflect.DeepEqual(eventIDs(got), want) {
		t.Errorf("delivered event ids = %v, want public ids %v", eventIDs(got), want)
	}
	assertVisibilityGateFold(t, a, fixture)
	if err := stream.Close(); err != nil {
		t.Errorf("subscription Close() error = %v", err)
	}
}

func TestColdReplayFiltersVisibilityBeforeGateFoldAndBacklog(t *testing.T) {
	t.Parallel()
	loopID := mustUUID(t)
	fixture := newVisibilityFixture(t, loopID)
	events := make([]event.Event, len(fixture.cases))
	for index, testCase := range fixture.cases {
		events[index] = testCase.ev
	}
	controller := &fakeController{sessionID: mustUUID(t), sub: newFakeSub()}
	a, err := newSessionAdapter(context.Background(), controller, scriptedReplayOpener{
		replayer: scriptedEventReplayer{cursor: &scriptedEventCursor{events: events}},
	}, true)
	if err != nil {
		t.Fatalf("newSessionAdapter() error = %v", err)
	}

	backlog, err := a.ReplayBacklog(context.Background())
	if err != nil {
		t.Fatalf("ReplayBacklog() error = %v", err)
	}
	if want := publicEventIDs(fixture.cases); !reflect.DeepEqual(eventIDs(backlog), want) {
		t.Errorf("backlog event ids = %v, want public ids %v", eventIDs(backlog), want)
	}
	assertVisibilityGateFold(t, a, fixture)
}

// TestReplayBacklogNilForNewSession proves a NEW session has no restore backlog.
func TestReplayBacklogNilForNewSession(t *testing.T) {
	t.Parallel()
	a, _, _, _ := newFakeAgent(t)
	backlog, err := a.ReplayBacklog(context.Background())
	if err != nil {
		t.Fatalf("ReplayBacklog error = %v", err)
	}
	if backlog != nil {
		t.Errorf("ReplayBacklog() = %v, want nil for a new session", backlog)
	}
}

func TestNewWithReplayReturnsFreshDurableBootstrap(t *testing.T) {
	t.Parallel()
	loopID := mustUUID(t)
	want := event.LoopStarted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}}
	controller := &fakeController{sessionID: mustUUID(t), sub: newFakeSub()}
	a, err := NewWithReplay(context.Background(), controller, scriptedReplayOpener{
		replayer: scriptedEventReplayer{cursor: &scriptedEventCursor{events: []event.Event{want}}},
	})
	if err != nil {
		t.Fatalf("NewWithReplay() error = %v", err)
	}
	backlog, err := a.ReplayBacklog(context.Background())
	if err != nil {
		t.Fatalf("ReplayBacklog() error = %v", err)
	}
	if len(backlog) != 1 {
		t.Fatalf("ReplayBacklog() length = %d, want 1", len(backlog))
	}
	if _, ok := backlog[0].(event.LoopStarted); !ok {
		t.Fatalf("ReplayBacklog()[0] = %T, want LoopStarted", backlog[0])
	}
}

// TestCloseShutsDownOnce proves Close is idempotent: it Shuts the session down exactly once.
func TestCloseShutsDownOnce(t *testing.T) {
	t.Parallel()
	a, fc, _, _ := newFakeAgent(t)
	if err := a.Close(context.Background()); err != nil {
		t.Fatalf("first Close error = %v", err)
	}
	if err := a.Close(context.Background()); err != nil {
		t.Errorf("second Close error = %v", err)
	}
	fc.mu.Lock()
	got := fc.shutdowns
	fc.mu.Unlock()
	if got != 1 {
		t.Errorf("Shutdown called %d times, want 1 (idempotent Close)", got)
	}
}

// TestRespondFormPassesThroughWithoutIndexLookup proves the form-gate reply path.
//
// The two assertions that matter are that the gate id reaches RespondGate unchanged
// and that NO gate was ever opened to make that work: a form gate is named by its
// GateOpened envelope, so the adapter must not require the (loop, tool-execution)
// index that the permission/AskUser trio depends on. An integration's form gate
// routinely has no tool execution at all, so an index lookup here would fail
// exactly the gates this path exists to serve.
func TestRespondFormPassesThroughWithoutIndexLookup(t *testing.T) {
	t.Parallel()
	a, fc, _, _ := newFakeAgent(t)

	gateID := mustUUID(t)
	values := map[string]json.RawMessage{
		"username": json.RawMessage(`"ada"`),
		"consent":  json.RawMessage(`true`),
	}
	if err := a.RespondGate(context.Background(), gateID, gate.FormActionAccept, values); err != nil {
		t.Fatalf("RespondGate error = %v", err)
	}
	got := fc.responses()
	if len(got) != 1 {
		t.Fatalf("RespondGate called %d times, want 1", len(got))
	}
	if got[0].GateID != gateID {
		t.Errorf("gate id = %v, want %v", got[0].GateID, gateID)
	}
	if got[0].Action != gate.FormActionAccept {
		t.Errorf("action = %q, want %q", got[0].Action, gate.FormActionAccept)
	}
	if string(got[0].Values["consent"]) != "true" {
		t.Errorf("consent = %s, want the bool true passed through unchanged", got[0].Values["consent"])
	}
	if got[0].Source.Kind != gate.ResponseFromUser {
		t.Errorf("source = %q, want %q", got[0].Source.Kind, gate.ResponseFromUser)
	}
}

// TestRespondFormDeclineCarriesNoValues proves a decline is an explicit non-answer:
// it records the refusal and submits nothing.
func TestRespondFormDeclineCarriesNoValues(t *testing.T) {
	t.Parallel()
	a, fc, _, _ := newFakeAgent(t)

	gateID := mustUUID(t)
	if err := a.RespondGate(context.Background(), gateID, gate.FormActionDecline, nil); err != nil {
		t.Fatalf("RespondGate error = %v", err)
	}
	got := fc.responses()
	if len(got) != 1 || got[0].Action != gate.FormActionDecline {
		t.Fatalf("responses = %+v, want one decline", got)
	}
	if len(got[0].Values) != 0 {
		t.Errorf("a decline submitted values: %+v", got[0].Values)
	}
}
