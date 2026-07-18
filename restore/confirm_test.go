package restore

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/harness/pkg/event"
)

func warnChanges() []event.DriftChange {
	return []event.DriftChange{
		{Category: event.DriftWorkspace, Old: "/a", New: "/b", Severity: event.DriftWarn},
		{Category: event.DriftPermission, Field: "posture", Old: "strict", New: "loose", Severity: event.DriftWarn},
	}
}

func TestConfirmModelUpdate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		key          tea.KeyPressMsg
		wantAccepted bool
		wantDeclined bool
		wantQuit     bool
	}{
		{name: "y accepts", key: tea.KeyPressMsg{Code: 'y', Text: "y"}, wantAccepted: true, wantQuit: true},
		{name: "uppercase Y accepts", key: tea.KeyPressMsg{Code: 'Y', Text: "Y"}, wantAccepted: true, wantQuit: true},
		{name: "n declines", key: tea.KeyPressMsg{Code: 'n', Text: "n"}, wantDeclined: true, wantQuit: true},
		{name: "esc declines", key: tea.KeyPressMsg{Code: tea.KeyEsc}, wantDeclined: true, wantQuit: true},
		{name: "other key is ignored", key: tea.KeyPressMsg{Code: 'x', Text: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newConfirmModel(warnChanges())
			got, cmd := m.Update(tt.key)
			cm, ok := got.(*confirmModel)
			if !ok {
				t.Fatalf("Update returned %T, want *confirmModel", got)
			}
			if cm.accepted != tt.wantAccepted {
				t.Errorf("accepted = %v, want %v", cm.accepted, tt.wantAccepted)
			}
			if cm.declined != tt.wantDeclined {
				t.Errorf("declined = %v, want %v", cm.declined, tt.wantDeclined)
			}
			gotQuit := cmd != nil && isQuit(cmd)
			if gotQuit != tt.wantQuit {
				t.Errorf("quit = %v, want %v", gotQuit, tt.wantQuit)
			}
		})
	}
}

// isQuit reports whether cmd is tea.Quit by invoking it and inspecting the message.
func isQuit(cmd tea.Cmd) bool {
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestConfirmModelViewRendersWarnCategories(t *testing.T) {
	t.Parallel()
	m := newConfirmModel(warnChanges())
	view := m.View().Content
	for _, want := range []string{"workspace", "permission.posture", "[y/N]"} {
		if !strings.Contains(view, want) {
			t.Errorf("View content missing %q:\n%s", want, view)
		}
	}
}

// fakeProgram stands in for a tea.Program so ConfirmDrift is exercised without a
// terminal. Run reports a preset final model + error; Quit records a force-quit.
type fakeProgram struct {
	final  tea.Model
	err    error
	onRun  func()
	quitCh chan struct{}
}

func (p *fakeProgram) Run() (tea.Model, error) {
	if p.onRun != nil {
		p.onRun()
	}
	return p.final, p.err
}

func (p *fakeProgram) Quit() {
	if p.quitCh != nil {
		close(p.quitCh)
	}
}

func withFakeProgram(t *testing.T, fp *fakeProgram) {
	t.Helper()
	prev := newProgram
	newProgram = func(tea.Model, ...tea.ProgramOption) program { return fp }
	t.Cleanup(func() { newProgram = prev })
}

// The TestTerminalUI* tests swap the package-level newProgram seam, so they share
// mutable global state and must NOT run in parallel with each other.
func TestTerminalUIConfirmDriftAccept(t *testing.T) {
	withFakeProgram(t, &fakeProgram{final: &confirmModel{accepted: true}})

	accept, note, err := terminalUI{}.ConfirmDrift(context.Background(), warnChanges())
	if err != nil {
		t.Fatalf("unexpected err = %v", err)
	}
	if !accept {
		t.Errorf("accept = false, want true")
	}
	if note != "" {
		t.Errorf("note = %q, want empty", note)
	}
}

func TestTerminalUIConfirmDriftDecline(t *testing.T) {
	withFakeProgram(t, &fakeProgram{final: &confirmModel{declined: true}})

	accept, _, err := terminalUI{}.ConfirmDrift(context.Background(), warnChanges())
	if err != nil {
		t.Fatalf("unexpected err = %v", err)
	}
	if accept {
		t.Errorf("accept = true, want false")
	}
}

func TestTerminalUIConfirmDriftProgramError(t *testing.T) {
	sentinel := errors.New("program boom")
	withFakeProgram(t, &fakeProgram{err: sentinel})

	accept, _, err := terminalUI{}.ConfirmDrift(context.Background(), warnChanges())
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
	if accept {
		t.Errorf("accept = true, want false on error")
	}
}

func TestTerminalUIConfirmDriftCtxCancelled(t *testing.T) {
	quitCh := make(chan struct{})
	// The program's Run blocks until ctx cancellation force-quits it, mirroring the real
	// program's behavior so the ctx watcher is exercised.
	fp := &fakeProgram{final: &confirmModel{}, quitCh: quitCh}
	fp.onRun = func() { <-quitCh }
	withFakeProgram(t, fp)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	accept, _, err := terminalUI{}.ConfirmDrift(ctx, warnChanges())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if accept {
		t.Errorf("accept = true, want false on ctx cancel")
	}
}
