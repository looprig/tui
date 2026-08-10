package sessionadapter

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/gate"
	"github.com/looprig/harness/pkg/hub"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/journal"
	"github.com/looprig/harness/pkg/sessionstore"
)

type replayPlan struct {
	deliveries []event.Delivery
	cursor     journal.EventCursor
	openErr    error
}

type replayTestOpener struct {
	mu       sync.Mutex
	plans    map[uint64]replayPlan
	fallback []event.Delivery
	opens    []uint64
}

type sequenceAppender struct {
	mu   sync.Mutex
	next uint64
}

type temporarySubscriptionError struct{}

func (temporarySubscriptionError) Error() string   { return "session temporarily unavailable" }
func (temporarySubscriptionError) Temporary() bool { return true }

func (a *sequenceAppender) AppendEvent(context.Context, event.Event) (uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.next++
	return a.next, nil
}

func (o *replayTestOpener) OpenEventReplayer(_ uuid.UUID, req sessionstore.ReplayRequest) (journal.EventReplayer, error) {
	o.mu.Lock()
	o.opens = append(o.opens, req.FromSeq)
	o.mu.Unlock()
	return replayTestReplayer{owner: o, from: req.FromSeq}, nil
}

type replayTestReplayer struct {
	owner *replayTestOpener
	from  uint64
}

func (r replayTestReplayer) Open(context.Context, journal.ReplayRequest) (journal.EventCursor, error) {
	r.owner.mu.Lock()
	plan, found := r.owner.plans[r.from]
	if !found && len(r.owner.fallback) > 0 {
		for _, delivery := range r.owner.fallback {
			if delivery.JournalSeq >= r.from {
				plan.deliveries = append(plan.deliveries, delivery)
			}
		}
	}
	r.owner.mu.Unlock()
	if plan.openErr != nil {
		return nil, plan.openErr
	}
	if plan.cursor != nil {
		return plan.cursor, nil
	}
	return &replayTestCursor{deliveries: append([]event.Delivery(nil), plan.deliveries...)}, nil
}

type replayTestCursor struct {
	deliveries []event.Delivery
	index      int
}

func (c *replayTestCursor) Next(context.Context) (event.Event, uint64, error) {
	if c.index == len(c.deliveries) {
		return nil, 0, io.EOF
	}
	d := c.deliveries[c.index]
	c.index++
	return d.Event, d.JournalSeq, nil
}

func (*replayTestCursor) Close() error { return nil }

type blockingReplayCursor struct{}

func (*blockingReplayCursor) Next(ctx context.Context) (event.Event, uint64, error) {
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

func (*blockingReplayCursor) Close() error { return nil }

func replaySessionEvent(sessionID uuid.UUID, id byte) event.Event {
	return event.SessionIdle{Header: event.Header{
		Coordinates: identity.Coordinates{SessionID: sessionID},
		EventID:     uuid.UUID{id},
	}}
}

func replayWorkflowEvent(sessionID uuid.UUID, id byte, label string) event.Event {
	return event.WorkflowActivity{
		Header: event.Header{
			Coordinates: identity.Coordinates{SessionID: sessionID},
			EventID:     uuid.UUID{id},
		},
		RunID:             uuid.UUID{0xE1},
		WorkflowName:      "source_document_extract",
		WorkflowVersion:   "v1",
		Kind:              event.WorkflowActivityVertexCompleted,
		Status:            event.WorkflowRunStatusRunning,
		VertexID:          uuid.UUID{0xE2},
		VertexLabel:       label,
		CompletedVertices: 1,
		TotalVertices:     2,
		OccurredAt:        time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
}

func replayChatEvent(sessionID uuid.UUID, id byte) event.Event {
	return event.TurnRejected{Header: event.Header{
		Coordinates: identity.Coordinates{
			SessionID: sessionID,
			LoopID:    uuid.UUID{0xE3},
		},
		EventID: uuid.UUID{id},
	}, Reason: event.RejectInternal}
}

func replayToolEvent(sessionID uuid.UUID, id byte) event.Event {
	return event.PermissionRequested{
		Header: event.Header{
			Coordinates: identity.Coordinates{
				SessionID: sessionID,
				LoopID:    uuid.UUID{0xE3},
			},
			EventID: uuid.UUID{id},
		},
		ToolExecutionID: uuid.UUID{id},
	}
}

func overflowDelivery(sessionID uuid.UUID, seq uint64) event.Delivery {
	id := byte(seq)
	var ev event.Event
	switch seq % 3 {
	case 0:
		ev = replayWorkflowEvent(sessionID, id, "Parse")
	case 1:
		ev = replayChatEvent(sessionID, id)
	default:
		ev = replayToolEvent(sessionID, id)
	}
	return event.Delivery{Event: ev, JournalSeq: seq}
}

func waitForSubscriptionCalls(t *testing.T, controller *fakeController, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if controller.subscriptionCalls() >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("SubscribeEvents calls = %d, want at least %d", controller.subscriptionCalls(), want)
		case <-time.After(time.Millisecond):
		}
	}
}

func readDelivery(t *testing.T, sub event.Subscription) event.Delivery {
	t.Helper()
	select {
	case delivery, ok := <-sub.Events():
		if !ok {
			t.Fatal("replaying subscription closed before delivery")
		}
		return delivery
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replayed delivery")
		return event.Delivery{}
	}
}

func TestReplayingSubscriptionRecoversGapInJournalOrderWithoutOverlap(t *testing.T) {
	t.Parallel()

	sessionID := uuid.UUID{0xD1}
	initial, replacement := newFakeSub(), newFakeSub()
	d1 := event.Delivery{Event: replaySessionEvent(sessionID, 1), JournalSeq: 1}
	d2 := event.Delivery{Event: replaySessionEvent(sessionID, 2), JournalSeq: 2}
	d3 := event.Delivery{Event: replayWorkflowEvent(sessionID, 3, "Parse"), JournalSeq: 3}
	d4 := event.Delivery{Event: replaySessionEvent(sessionID, 4), JournalSeq: 4}
	d5 := event.Delivery{Event: replayWorkflowEvent(sessionID, 5, "Map"), JournalSeq: 5}
	opener := &replayTestOpener{plans: map[uint64]replayPlan{
		0: {deliveries: []event.Delivery{d1}},
		2: {},
		3: {deliveries: []event.Delivery{d3, d4}},
	}}
	controller := &fakeController{
		sessionID:      sessionID,
		sub:            initial,
		subscribeQueue: []event.Subscription{initial, replacement},
		active:         &fakeHandle{id: uuid.UUID{0xD2}, model: testModel()},
		loops:          map[uuid.UUID]*fakeHandle{uuid.UUID{0xD2}: {id: uuid.UUID{0xD2}, model: testModel()}},
	}
	adapter, err := NewWithReplay(context.Background(), controller, opener)
	if err != nil {
		t.Fatalf("NewWithReplay() = %v", err)
	}
	defer func() { _ = adapter.Close(context.Background()) }()

	stream, err := adapter.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	defer func() { _ = stream.Close() }()
	waitForSubscriptionCalls(t, controller, 1)

	initial.ch <- d2
	if got := readDelivery(t, stream); got.JournalSeq != 2 {
		t.Fatalf("initial live sequence = %d, want 2", got.JournalSeq)
	}
	initial.fail(&hub.SubscriptionLossError{DroppedClass: event.Enduring})
	waitForSubscriptionCalls(t, controller, 2)

	// These are queued while the replacement is repairing the gap. d3 overlaps the
	// journal replay and must be dropped; d5 is newer and must follow the replay.
	replacement.ch <- d3
	replacement.ch <- d5
	gotSeq := []uint64{readDelivery(t, stream).JournalSeq, readDelivery(t, stream).JournalSeq, readDelivery(t, stream).JournalSeq}
	wantSeq := []uint64{3, 4, 5}
	if len(gotSeq) != len(wantSeq) {
		t.Fatalf("recovered sequence count = %v, want %v", gotSeq, wantSeq)
	}
	for i := range wantSeq {
		if gotSeq[i] != wantSeq[i] {
			t.Errorf("recovered sequence[%d] = %d, want %d (got %v)", i, gotSeq[i], wantSeq[i], gotSeq)
		}
	}
	opener.mu.Lock()
	gotOpens := append([]uint64(nil), opener.opens...)
	opener.mu.Unlock()
	wantOpens := []uint64{0, 2, 3}
	if !reflect.DeepEqual(gotOpens, wantOpens) {
		t.Errorf("replay open positions = %v, want %v", gotOpens, wantOpens)
	}
}

func TestReplayingSubscriptionRepairsOutOfOrderLiveDeliveries(t *testing.T) {
	t.Parallel()

	sessionID := uuid.UUID{0xD5}
	initial := newFakeSub()
	d1 := event.Delivery{Event: replaySessionEvent(sessionID, 1), JournalSeq: 1}
	d2 := event.Delivery{Event: replaySessionEvent(sessionID, 2), JournalSeq: 2}
	d3 := event.Delivery{Event: replayWorkflowEvent(sessionID, 3, "Parse"), JournalSeq: 3}
	opener := &replayTestOpener{plans: map[uint64]replayPlan{
		0: {deliveries: []event.Delivery{d1}},
		2: {deliveries: []event.Delivery{d2}},
	}}
	controller := &fakeController{
		sessionID:      sessionID,
		sub:            initial,
		subscribeQueue: []event.Subscription{initial},
	}
	adapter, err := NewWithReplay(context.Background(), controller, opener)
	if err != nil {
		t.Fatalf("NewWithReplay() = %v", err)
	}
	defer func() { _ = adapter.Close(context.Background()) }()

	stream, err := adapter.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	defer func() { _ = stream.Close() }()
	waitForSubscriptionCalls(t, controller, 1)

	// The higher live sequence arrives first. The replay cursor repairs d2 before
	// d3 is exposed; the later live d2 is then discarded as overlap.
	initial.ch <- d3
	initial.ch <- d2
	got := []uint64{readDelivery(t, stream).JournalSeq, readDelivery(t, stream).JournalSeq}
	want := []uint64{2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("out-of-order recovery = %v, want %v", got, want)
	}
}

func TestReplayingSubscriptionFiltersBeforeGateFold(t *testing.T) {
	t.Parallel()

	sessionID := uuid.UUID{0xD8}
	targetLoop := uuid.UUID{0xE4}
	otherLoop := uuid.UUID{0xE5}
	targetCall := uuid.UUID{0xE6}
	otherCall := uuid.UUID{0xE7}
	targetGate := uuid.UUID{0xE8}
	otherGate := uuid.UUID{0xE9}
	initial := newFakeSub()
	d1 := event.Delivery{Event: replaySessionEvent(sessionID, 1), JournalSeq: 1}
	other := event.Delivery{Event: gateOpened(otherLoop, otherCall, otherGate), JournalSeq: 2}
	target := event.Delivery{Event: gateOpened(targetLoop, targetCall, targetGate), JournalSeq: 3}
	opener := &replayTestOpener{plans: map[uint64]replayPlan{
		0: {deliveries: []event.Delivery{d1}},
		2: {deliveries: []event.Delivery{other, target}},
	}}
	controller := &fakeController{
		sessionID:      sessionID,
		sub:            initial,
		subscribeQueue: []event.Subscription{initial},
	}
	adapter, err := NewWithReplay(context.Background(), controller, opener)
	if err != nil {
		t.Fatalf("NewWithReplay() = %v", err)
	}
	defer func() { _ = adapter.Close(context.Background()) }()

	stream, err := adapter.Subscribe(event.EventFilter{
		Enduring: event.LoopScope{Loops: map[uuid.UUID]struct{}{targetLoop: {}}},
	})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	defer func() { _ = stream.Close() }()
	if got := readDelivery(t, stream).JournalSeq; got != 3 {
		t.Fatalf("replay delivery sequence = %d, want target gate sequence 3", got)
	}
	if err := adapter.Approve(context.Background(), targetLoop, targetCall, gate.ApprovalApprove); err != nil {
		t.Fatalf("Approve(target) = %v", err)
	}
	if err := adapter.Approve(context.Background(), otherLoop, otherCall, gate.ApprovalApprove); err == nil {
		t.Fatal("Approve(other) succeeded after filtered replay gate")
	}
}

func TestReplayingSubscriptionRecoversActualHubOverflowInJournalOrder(t *testing.T) {
	sessionID := uuid.UUID{0xD7}
	appender := &sequenceAppender{}
	h := hub.New(sessionID, hub.WithAppender(appender))
	d1 := overflowDelivery(sessionID, 1)
	if err := h.PublishEvent(context.Background(), d1.Event); err != nil {
		t.Fatalf("PublishEvent(d1) = %v", err)
	}
	live, err := h.SubscribeEvents(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Hub.SubscribeEvents() = %v", err)
	}
	replacement := newFakeSub()
	all := make([]event.Delivery, 0, 259)
	for seq := uint64(2); seq <= 260; seq++ {
		all = append(all, overflowDelivery(sessionID, seq))
	}
	opener := &replayTestOpener{
		plans: map[uint64]replayPlan{
			0: {deliveries: []event.Delivery{d1}},
			2: {},
		},
		fallback: all,
	}
	controller := &fakeController{
		sessionID:      sessionID,
		sub:            newFakeSub(),
		subscribeQueue: []event.Subscription{live, replacement},
	}
	adapter, err := NewWithReplay(context.Background(), controller, opener)
	if err != nil {
		t.Fatalf("NewWithReplay() = %v", err)
	}
	defer func() { _ = adapter.Close(context.Background()) }()

	stream, err := adapter.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	defer func() { _ = stream.Close() }()
	waitForSubscriptionCalls(t, controller, 1)
	if err := h.PublishEvent(context.Background(), all[0].Event); err != nil {
		t.Fatalf("PublishEvent(d2) = %v", err)
	}
	if got := readDelivery(t, stream); got.JournalSeq != 2 {
		t.Fatalf("first live sequence = %d, want 2", got.JournalSeq)
	}

	// Do not read while flooding. The real Hub's bounded egress therefore fails
	// the live subscription on an Enduring overflow; the adapter must recover the
	// missing tail from the journal and then suppress the replacement overlap.
	for _, delivery := range all[1:] {
		if err := h.PublishEvent(context.Background(), delivery.Event); err != nil {
			t.Fatalf("PublishEvent(seq %d) = %v", delivery.JournalSeq, err)
		}
	}
	gotLive := make(chan event.Delivery, len(all)+1)
	go func() {
		for delivery := range stream.Events() {
			gotLive <- delivery
		}
		close(gotLive)
	}()
	waitForSubscriptionCalls(t, controller, 2)
	replacement.ch <- all[len(all)-1] // seq 260: replay overlap
	replacement.ch <- overflowDelivery(sessionID, 261)

	for wantSeq := uint64(3); wantSeq <= 261; wantSeq++ {
		select {
		case got := <-gotLive:
			if got.JournalSeq != wantSeq {
				t.Fatalf("recovered Hub sequence = %d, want %d", got.JournalSeq, wantSeq)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for recovered Hub sequence %d", wantSeq)
		}
	}
}

func TestReplayingSubscriptionBacksOffAndRetriesSubscription(t *testing.T) {
	t.Parallel()

	sessionID := uuid.UUID{0xD3}
	initial := newFakeSub()
	controller := &fakeController{
		sessionID:       sessionID,
		sub:             initial,
		subscribeQueue:  []event.Subscription{initial},
		subscribeErrors: []error{temporarySubscriptionError{}},
	}
	opener := &replayTestOpener{plans: map[uint64]replayPlan{1: {}}}
	adapter, err := NewWithReplay(context.Background(), controller, opener)
	if err != nil {
		t.Fatalf("NewWithReplay() = %v", err)
	}
	defer func() { _ = adapter.Close(context.Background()) }()

	stream, err := adapter.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	waitForSubscriptionCalls(t, controller, 2)
	initial.ch <- event.Delivery{Event: replaySessionEvent(sessionID, 6), JournalSeq: 1}
	if got := readDelivery(t, stream); got.JournalSeq != 1 {
		t.Fatalf("retried live sequence = %d, want 1", got.JournalSeq)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}

func TestReplayingSubscriptionStopsOnPermanentSubscriptionError(t *testing.T) {
	t.Parallel()

	permanentErr := errors.New("session is permanently unavailable")
	var callsMu sync.Mutex
	calls := 0
	stream, err := newReplayingSubscription(replayingSubscriptionConfig{
		ctx: context.Background(),
		subscribe: func(event.EventFilter) (event.Subscription, error) {
			callsMu.Lock()
			calls++
			callsMu.Unlock()
			return nil, permanentErr
		},
		openReplay: func(uint64) (journal.EventReplayer, error) {
			return nil, errors.New("openReplay must not run without a live subscription")
		},
	})
	if err != nil {
		t.Fatalf("newReplayingSubscription() = %v", err)
	}
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("subscription yielded a delivery after permanent subscribe failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription retried a permanent subscribe failure")
	}
	if !errors.Is(stream.Err(), permanentErr) {
		t.Fatalf("subscription Err() = %v, want %v", stream.Err(), permanentErr)
	}
	callsMu.Lock()
	gotCalls := calls
	callsMu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("SubscribeEvents calls = %d, want 1", gotCalls)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}

func TestReplayingSubscriptionPropagatesNonOverflowTermination(t *testing.T) {
	t.Parallel()

	terminalErr := errors.New("session aborted")
	initial := newFakeSub()
	controller := &fakeController{
		sub:            initial,
		subscribeQueue: []event.Subscription{initial},
		sessionID:      uuid.UUID{0xD6},
	}
	opener := &replayTestOpener{plans: map[uint64]replayPlan{1: {}}}
	adapter, err := NewWithReplay(context.Background(), controller, opener)
	if err != nil {
		t.Fatalf("NewWithReplay() = %v", err)
	}
	defer func() { _ = adapter.Close(context.Background()) }()

	stream, err := adapter.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	waitForSubscriptionCalls(t, controller, 1)
	initial.fail(terminalErr)
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("subscription yielded a delivery after terminal close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription did not terminate after terminal close")
	}
	if !errors.Is(stream.Err(), terminalErr) {
		t.Fatalf("subscription Err() = %v, want %v", stream.Err(), terminalErr)
	}
	if got := controller.subscriptionCalls(); got != 1 {
		t.Fatalf("SubscribeEvents calls = %d, want 1 after terminal close", got)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}

func TestReplayingSubscriptionStopsRetryingPermanentReplayError(t *testing.T) {
	t.Parallel()

	replayErr := errors.New("journal is corrupt")
	initial := newFakeSub()
	stream, err := newReplayingSubscription(replayingSubscriptionConfig{
		ctx:        context.Background(),
		subscribe:  func(event.EventFilter) (event.Subscription, error) { return initial, nil },
		openReplay: func(uint64) (journal.EventReplayer, error) { return scriptedEventReplayer{openErr: replayErr}, nil },
	})
	if err != nil {
		t.Fatalf("newReplayingSubscription() = %v", err)
	}
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("subscription yielded a delivery after permanent replay failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription retried a permanent replay failure")
	}
	if !errors.Is(stream.Err(), replayErr) {
		t.Fatalf("subscription Err() = %v, want %v", stream.Err(), replayErr)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}

func TestReplayingSubscriptionCancellationStopsBackoff(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var callsMu sync.Mutex
	calls := 0
	stream, err := newReplayingSubscription(replayingSubscriptionConfig{
		ctx: ctx,
		subscribe: func(event.EventFilter) (event.Subscription, error) {
			callsMu.Lock()
			calls++
			callsMu.Unlock()
			return nil, temporarySubscriptionError{}
		},
		openReplay: func(uint64) (journal.EventReplayer, error) {
			return nil, errors.New("unexpected replay before live subscription")
		},
	})
	if err != nil {
		t.Fatalf("newReplayingSubscription() = %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	cancel()
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("subscription yielded a delivery after cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription did not stop its retry backoff after cancellation")
	}
	if !errors.Is(stream.Err(), context.Canceled) {
		t.Fatalf("subscription Err() = %v, want context.Canceled", stream.Err())
	}
	callsMu.Lock()
	gotCalls := calls
	callsMu.Unlock()
	if gotCalls == 0 {
		t.Fatal("SubscribeEvents was never attempted")
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}

func TestReplayingSubscriptionCloseCancelsBlockedReplay(t *testing.T) {
	t.Parallel()

	sessionID := uuid.UUID{0xD4}
	initial := newFakeSub()
	controller := &fakeController{sessionID: sessionID, sub: initial}
	opener := &replayTestOpener{plans: map[uint64]replayPlan{1: {cursor: &blockingReplayCursor{}}}}
	adapter, err := NewWithReplay(context.Background(), controller, opener)
	if err != nil {
		t.Fatalf("NewWithReplay() = %v", err)
	}
	defer func() { _ = adapter.Close(context.Background()) }()

	stream, err := adapter.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}
	waitForSubscriptionCalls(t, controller, 1)
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("subscription yielded a delivery after Close")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription output did not close after cancelling replay")
	}
}

func TestReplayingSubscriptionRequiresDurableReplayFactory(t *testing.T) {
	t.Parallel()

	_, err := newReplayingSubscription(replayingSubscriptionConfig{
		ctx:       context.Background(),
		subscribe: func(event.EventFilter) (event.Subscription, error) { return newFakeSub(), nil },
	})
	if !errors.Is(err, errDurableReplayUnavailable) {
		t.Fatalf("newReplayingSubscription() error = %v, want durable replay error", err)
	}
}

func TestReplayingSubscriptionStopsWhenReplayFactoryHasNoReplayer(t *testing.T) {
	t.Parallel()

	stream, err := newReplayingSubscription(replayingSubscriptionConfig{
		ctx:       context.Background(),
		subscribe: func(event.EventFilter) (event.Subscription, error) { return newFakeSub(), nil },
		openReplay: func(uint64) (journal.EventReplayer, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("newReplayingSubscription() = %v", err)
	}
	select {
	case _, ok := <-stream.Events():
		if ok {
			t.Fatal("subscription yielded a delivery without a durable replayer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscription did not terminate without a durable replayer")
	}
	if !errors.Is(stream.Err(), errDurableReplayUnavailable) {
		t.Fatalf("subscription Err() = %v, want durable replay error", stream.Err())
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}
