package restore

import (
	"context"
	"errors"
	"testing"

	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/session"
)

// fakeUI records calls and returns a preset confirm result, so the decision logic is
// tested without a terminal.
type fakeUI struct {
	confirmAccept bool
	confirmNote   string
	confirmErr    error

	confirmCalls int
	confirmWarns []event.DriftChange
	notifyCalls  int
	notifyInfos  []event.DriftChange
}

func (f *fakeUI) ConfirmDrift(_ context.Context, warns []event.DriftChange) (bool, string, error) {
	f.confirmCalls++
	f.confirmWarns = warns
	return f.confirmAccept, f.confirmNote, f.confirmErr
}

func (f *fakeUI) Notify(infos []event.DriftChange) {
	f.notifyCalls++
	f.notifyInfos = infos
}

func infoChange() event.DriftChange {
	return event.DriftChange{Category: event.DriftModel, Old: "m1", New: "m2", Severity: event.DriftInfo}
}

func warnChange() event.DriftChange {
	return event.DriftChange{Category: event.DriftWorkspace, Old: "/a", New: "/b", Severity: event.DriftWarn}
}

func TestDecideRestore(t *testing.T) {
	errSentinel := errors.New("ctx deadline exceeded")

	tests := []struct {
		name          string
		changes       []event.DriftChange
		confirmAccept bool
		confirmNote   string
		confirmErr    error
		wantAccept    bool
		wantSource    event.DecisionSource
		wantMessage   string
		wantErr       error
		wantConfirm   int
		wantNotify    int
	}{
		{
			name:        "info only auto-accepts and notifies",
			changes:     []event.DriftChange{infoChange()},
			wantAccept:  true,
			wantSource:  event.DecisionSourceUser,
			wantConfirm: 0,
			wantNotify:  1,
		},
		{
			name:        "no changes auto-accepts without notify",
			changes:     nil,
			wantAccept:  true,
			wantSource:  event.DecisionSourceUser,
			wantConfirm: 0,
			wantNotify:  0,
		},
		{
			name:          "warn confirmed accepts with user source and note",
			changes:       []event.DriftChange{warnChange(), infoChange()},
			confirmAccept: true,
			confirmNote:   "approved by operator",
			wantAccept:    true,
			wantSource:    event.DecisionSourceUser,
			wantMessage:   "approved by operator",
			wantConfirm:   1,
			wantNotify:    0,
		},
		{
			name:          "warn declined rejects with user source",
			changes:       []event.DriftChange{warnChange()},
			confirmAccept: false,
			wantAccept:    false,
			wantSource:    event.DecisionSourceUser,
			wantConfirm:   1,
			wantNotify:    0,
		},
		{
			name:        "confirm error rejects and surfaces the cause",
			changes:     []event.DriftChange{warnChange()},
			confirmErr:  errSentinel,
			wantAccept:  false,
			wantErr:     errSentinel,
			wantConfirm: 1,
			wantNotify:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ui := &fakeUI{confirmAccept: tt.confirmAccept, confirmNote: tt.confirmNote, confirmErr: tt.confirmErr}
			d := NewDecider(ui)

			got, err := d.DecideRestore(context.Background(), event.DriftAssessment{Changes: tt.changes})

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected err = %v", err)
			}
			if got.Accept != tt.wantAccept {
				t.Errorf("Accept = %v, want %v", got.Accept, tt.wantAccept)
			}
			if tt.wantErr == nil && got.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tt.wantSource)
			}
			if got.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tt.wantMessage)
			}
			if ui.confirmCalls != tt.wantConfirm {
				t.Errorf("ConfirmDrift calls = %d, want %d", ui.confirmCalls, tt.wantConfirm)
			}
			if ui.notifyCalls != tt.wantNotify {
				t.Errorf("Notify calls = %d, want %d", ui.notifyCalls, tt.wantNotify)
			}
		})
	}
}

// TestDecideRestorePassesOnlyWarnsToConfirm proves the info/warn partition: ConfirmDrift
// receives only warn changes, never the accompanying infos.
func TestDecideRestorePassesOnlyWarnsToConfirm(t *testing.T) {
	t.Parallel()
	ui := &fakeUI{confirmAccept: true}
	d := NewDecider(ui)

	_, err := d.DecideRestore(context.Background(), event.DriftAssessment{
		Changes: []event.DriftChange{infoChange(), warnChange(), infoChange()},
	})
	if err != nil {
		t.Fatalf("unexpected err = %v", err)
	}
	if len(ui.confirmWarns) != 1 || ui.confirmWarns[0].Severity != event.DriftWarn {
		t.Fatalf("ConfirmDrift warns = %#v, want exactly one warn change", ui.confirmWarns)
	}
}

// TestDeciderImplementsInterface pins the RestoreDecider contract at compile time.
var _ session.RestoreDecider = Decider{}
