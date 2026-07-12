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

// foldBacklog folds backlog through the SAME pure reducers the background fold uses,
// from the same zero state restoreBacklogCmd starts at, so a test can assert the
// command's reducer state matches a direct, per-event fold. It mirrors the production
// fold exactly (transcript.ApplyEvent + interaction.ApplyEvent), which is the point:
// the repaint is correct iff the background fold equals this fold.
func foldBacklog(primary uuid.UUID, backlog []event.Event) (transcriptModel, interactionModel) {
	tr := transcriptModel{}
	in := newInteractionModel()
	for _, ev := range backlog {
		tr = tr.ApplyEvent(ev)
		in = in.ApplyEvent(ev)
	}
	return tr, in
}

// runRestoreCmd executes restoreBacklogCmd off the update loop the way the runtime
// would, returning the single restoredMsg it produces. It fails the test if the
// command yields any other message type — the fold must surface exactly one result.
func runRestoreCmd(t *testing.T, cmd tea.Cmd) restoredMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("restoreBacklogCmd returned a nil command")
	}
	msg, ok := cmd().(restoredMsg)
	if !ok {
		t.Fatalf("restoreBacklogCmd produced %T, want restoredMsg", cmd())
	}
	return msg
}

// TestReplayBacklogSeam covers the narrow Agent backlog seam: a NEW (non-restored)
// session returns an empty/nil backlog (no repaint), a restored session returns its
// historical Enduring events, and a read failure surfaces a typed error the fold maps
// onto the restore-error path.
func TestReplayBacklogSeam(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)

	tests := []struct {
		name    string
		backlog []event.Event
		err     error
		wantLen int
		wantErr bool
	}{
		{name: "new session returns nil backlog", backlog: nil, wantLen: 0},
		{name: "new session returns empty backlog", backlog: []event.Event{}, wantLen: 0},
		{
			name:    "restored session returns its enduring backlog",
			backlog: []event.Event{event.TurnStarted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}}}},
			wantLen: 1,
		},
		{name: "read failure surfaces a typed error", err: errors.New("replay read"), wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := &fakeAgent{activeLoopID: primary, backlog: tt.backlog, replayErr: tt.err}
			got, err := agent.ReplayBacklog(context.Background())
			if (err != nil) != tt.wantErr {
				t.Fatalf("ReplayBacklog() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !agent.replayCalled {
				t.Error("ReplayBacklog seam not exercised")
			}
			if !tt.wantErr && len(got) != tt.wantLen {
				t.Errorf("backlog len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// TestRestoreBacklogFoldsOffLoopOnce is the headline no-UI-hang property: a LARGE
// backlog folds inside the background tea.Cmd (off the update loop) and yields EXACTLY
// ONE restoredMsg — the reducers are applied per-event INSIDE the command, never via N
// per-event update-loop messages. The Screen's Update is driven O(1) times (once with
// the single restoredMsg), not O(N) in backlog size, so a 5–10k-event backlog cannot
// hang the UI by flooding it with per-event messages.
func TestRestoreBacklogFoldsOffLoopOnce(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)

	// A large backlog: alternating TurnStarted + StepDone for the active loop, so the
	// fold exercises the real commit path many thousands of times inside the command.
	const turns = 6000
	backlog := make([]event.Event, 0, turns*2)
	for i := 0; i < turns; i++ {
		backlog = append(backlog,
			event.TurnStarted{
				Header:  event.Header{Coordinates: identity.Coordinates{LoopID: primary}},
				Message: &content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: []content.Block{&content.TextBlock{Text: "q"}}}},
			},
			event.StepDone{
				Header:   event.Header{Coordinates: identity.Coordinates{LoopID: primary}},
				Messages: content.AgenticMessages{aiMessage("", "a")},
			},
		)
	}

	agent := &fakeAgent{activeLoopID: primary, backlog: backlog}

	// The fold runs OFF the update loop in restoreBacklogCmd. Executing it once yields a
	// SINGLE restoredMsg carrying the already-folded reducer state — no per-event message.
	msg := runRestoreCmd(t, restoreBacklogCmd(context.Background(), agent))
	if msg.err != nil {
		t.Fatalf("restoredMsg err = %v, want nil", msg.err)
	}

	// Folding the same backlog directly through the reducers must equal the command's
	// result: the command folded every event itself, off-loop.
	wantTr, _ := foldBacklog(primary, backlog)
	if got, want := len(msg.transcript.committed), len(wantTr.committed); got != want {
		t.Fatalf("folded committed = %d, want %d (the command must fold the WHOLE backlog off-loop)", got, want)
	}
}

// TestRestoredMsgRepaintCorrectness covers the repaint-correctness property: a backlog
// of TurnStarted + StepDone (+ TurnFoldedInto) folds into the EXACT committed transcript
// you get by feeding those same events through ApplyEvent directly, and a pending gate
// in the backlog is reflected in the rebuilt interaction model.
func TestRestoredMsgRepaintCorrectness(t *testing.T) {
	t.Parallel()

	primary := callID(0xAA)
	hdr := event.Header{Coordinates: identity.Coordinates{LoopID: primary}}

	backlog := []event.Event{
		event.TurnStarted{
			Header:  hdr,
			Message: &content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: []content.Block{&content.TextBlock{Text: "first question"}}}},
		},
		event.StepDone{Header: hdr, Messages: content.AgenticMessages{aiMessage("", "first answer")}},
		event.TurnFoldedInto{
			Header:  hdr,
			Message: &content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: []content.Block{&content.TextBlock{Text: "folded follow-up"}}}},
		},
		event.StepDone{Header: hdr, Messages: content.AgenticMessages{aiMessage("", "second answer")}},
		event.PermissionRequested{Header: hdr, ToolExecutionID: callID(7), Request: tool.BashRequest{Command: "ls"}},
	}

	agent := &fakeAgent{activeLoopID: primary, backlog: backlog}

	msg := runRestoreCmd(t, restoreBacklogCmd(context.Background(), agent))
	if msg.err != nil {
		t.Fatalf("restoredMsg err = %v, want nil", msg.err)
	}

	wantTr, wantIn := foldBacklog(primary, backlog)

	// The committed transcript must match the direct fold entry-for-entry.
	if got, want := len(msg.transcript.committed), len(wantTr.committed); got != want {
		t.Fatalf("committed = %d, want %d", got, want)
	}
	for i := range wantTr.committed {
		g, w := msg.transcript.committed[i], wantTr.committed[i]
		if g.Kind != w.Kind {
			t.Errorf("committed[%d].Kind = %d, want %d", i, g.Kind, w.Kind)
		}
		if committedText(g) != committedText(w) {
			t.Errorf("committed[%d] text = %q, want %q", i, committedText(g), committedText(w))
		}
	}

	// The pending permission gate from the backlog is reflected in the interaction model.
	if got, want := msg.interaction.PendingCount(), wantIn.PendingCount(); got != want {
		t.Errorf("pending prompts = %d, want %d (backlog gate must repaint as pending)", got, want)
	}
}

// NOTE: the old scrollback Screen's restore INTEGRATION tests (flush-once, live handoff,
// read-error notice, new-session no-repaint, Init-triggers-restore) were removed when that
// shell was archived. Screen's equivalents live in modern_test.go —
// TestModernHandleRestored, TestModernRestoreEmptyBacklogPreservesBanner, and
// TestModernInitTriggersRestore. The shell-independent fold/reducer correctness stays here
// (TestReplayBacklogSeam, TestRestoredMsgRepaintCorrectness, TestRestoreBacklogFoldsOffLoopOnce).
