package presentation

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/harness/pkg/event"
)

// TestEscInterruptsTheLoopTheUserIsWatching pins Esc to the loop the UI is actually
// showing. The status line follows the FOCUSED loop (focusedStatus), and a composer
// submit goes to the FOCUSED loop (routeToInteraction → submitToLoop) — so gating the
// interrupt on the session-ACTIVE loop's status instead made Esc a dead key exactly when
// the user could see a running turn and had messages piling up behind it.
func TestEscInterruptsTheLoopTheUserIsWatching(t *testing.T) {
	active := callID(0xAA)
	watched := callID(0xBB)
	agent := &fakeAgent{activeLoopID: active}
	m := newScreenSized(t, agent, 80, 24)

	// The watched loop runs a turn; the session-active loop stays idle.
	m = feed(t, m, event.TurnStarted{Header: hdr(watched)})
	m.focusedLoopID = watched
	if got := m.focusedStatus(); got != StatusRunning {
		t.Fatalf("focusedStatus = %v, want StatusRunning (the user is watching a running turn)", got)
	}

	m, cmd := updateScreen(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})

	if cmd == nil {
		t.Fatal("esc issued no interrupt while the watched loop was running")
	}
	if m.status != StatusInterrupting {
		t.Errorf("status = %v, want StatusInterrupting", m.status)
	}
}

// TestInterruptResolvesOnTheInterruptedLoopsTerminal closes the other half of the wedge.
// Session interrupt is session-wide, so the loop that actually reports a terminal need not
// be the session-active one. Resolving StatusInterrupting only on the ACTIVE loop's
// terminal left the whole UI pinned to "Interrupting…" forever — a session-global status,
// so every loop's status line froze with it.
func TestInterruptResolvesOnTheInterruptedLoopsTerminal(t *testing.T) {
	active := callID(0xAA)
	watched := callID(0xBB)
	agent := &fakeAgent{activeLoopID: active}
	m := newScreenSized(t, agent, 80, 24)

	m = feed(t, m, event.TurnStarted{Header: hdr(watched)})
	m.focusedLoopID = watched
	m, _ = updateScreen(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.status != StatusInterrupting {
		t.Fatalf("status = %v, want StatusInterrupting before the terminal", m.status)
	}

	m = feed(t, m, event.TurnInterrupted{Header: hdr(watched)})

	if m.status != StatusIdle {
		t.Errorf("status = %v, want StatusIdle once the interrupted loop reported its terminal", m.status)
	}
}
