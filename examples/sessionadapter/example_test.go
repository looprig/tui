package sessionadapter_test

import (
	"context"
	"fmt"
	"sync"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/gate"
	"github.com/looprig/harness/pkg/loop"
	"github.com/looprig/harness/pkg/workspacestore"
	"github.com/looprig/inference/model"
	"github.com/looprig/tui/sessionadapter"
)

type loopHandle struct {
	id    uuid.UUID
	model model.Model
}

func (h loopHandle) ID() uuid.UUID       { return h.id }
func (h loopHandle) Mode() loop.ModeName { return "chat" }
func (h loopHandle) Model() model.Model  { return h.model }

type subscription struct {
	events chan event.Delivery
	once   sync.Once
}

func (s *subscription) Events() <-chan event.Delivery { return s.events }
func (s *subscription) Err() error                    { return nil }
func (s *subscription) Close() error {
	s.once.Do(func() { close(s.events) })
	return nil
}

// controller is the harness-owned session surface being adapted. A real program
// receives this from its Rig; the TUI does not construct the Rig.
type controller struct {
	sessionID uuid.UUID
	active    loopHandle
	sub       *subscription
	shutdowns int
}

func (c *controller) SessionID() uuid.UUID                  { return c.sessionID }
func (c *controller) ActiveLoop() loop.Handle               { return c.active }
func (c *controller) Loop(id uuid.UUID) (loop.Handle, bool) { return c.active, id == c.active.id }
func (c *controller) Submit(context.Context, []content.Block) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (c *controller) SubmitToLoop(context.Context, uuid.UUID, []content.Block) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (c *controller) Compact(context.Context) (uuid.UUID, error) { return uuid.UUID{}, nil }
func (c *controller) CompactToLoop(context.Context, uuid.UUID) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (c *controller) SubscribeEvents(event.EventFilter) (event.Subscription, error) {
	return c.sub, nil
}
func (c *controller) RespondGate(context.Context, gate.GateResponse) error { return nil }
func (c *controller) Interrupt(context.Context) (bool, error)              { return false, nil }
func (c *controller) SetActiveLoop(context.Context, uuid.UUID) error       { return nil }
func (c *controller) LoopController(uuid.UUID) (loop.Controller, bool)     { return nil, false }
func (c *controller) CheckpointWorkspace(context.Context) (workspacestore.Ref, error) {
	return "", nil
}
func (c *controller) RestoreWorkspace(context.Context, workspacestore.Ref) error { return nil }
func (c *controller) Shutdown(context.Context) error {
	c.shutdowns++
	return nil
}

func Example_adapterOwnsOnlySessionPresentation() {
	sessionID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	loopID := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	inner := &subscription{events: make(chan event.Delivery, 1)}
	ctrl := &controller{
		sessionID: sessionID,
		active: loopHandle{
			id:    loopID,
			model: model.Model{Caps: model.Capabilities{AcceptsImages: true}},
		},
		sub: inner,
	}

	agent := sessionadapter.New(ctrl)
	stream, err := agent.Subscribe(event.EventFilter{Enduring: event.LoopScope{All: true}})
	if err != nil {
		panic(err)
	}
	inner.events <- event.Delivery{
		Event:      event.SessionActive{Header: event.Header{}},
		JournalSeq: 7,
	}
	delivery := <-stream.Events()
	_, sessionBecameActive := delivery.Event.(event.SessionActive)

	fmt.Println("controller supplied by composition root:", agent.Controller() == ctrl)
	fmt.Println("active loop accepts images:", agent.AcceptsImages(loopID))
	fmt.Println("session event delivered at sequence:", sessionBecameActive, delivery.JournalSeq)
	_ = agent.Close(context.Background())
	_ = agent.Close(context.Background())
	fmt.Println("session shutdowns:", ctrl.shutdowns)
	_ = stream.Close()

	// Output:
	// controller supplied by composition root: true
	// active loop accepts images: true
	// session event delivered at sequence: true 7
	// session shutdowns: 1
}
