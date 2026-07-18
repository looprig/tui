// Package restore provides a reusable, interactive session.RestoreDecider that an
// application embedding tui wires into its harness Rig via rig.WithRestoreDecider.
//
// The decider runs at application STARTUP, inside the app's blocking RestoreSession
// call, BEFORE tui's main Bubble Tea program starts. tui does not compose the Rig or
// call RestoreSession itself; the embedding app does, then hands the already-built
// session.SessionController to tui/sessionadapter. This decider is therefore a
// self-contained unit the app wires up front:
//
//	rig.WithRestoreDecider(restore.NewDecider(restore.NewTerminalUI()))
//
// Behavior: informational drift (DriftInfo) is auto-accepted and surfaced without
// blocking; warn-level drift (DriftWarn) blocks for an interactive y/n confirmation.
// An accepting decision is always attributed to the user (DecisionSourceUser), and a
// ctx cancellation or timeout is treated as a fail-secure rejection carrying the cause.
package restore

import (
	"context"

	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/session"
)

// UI is the narrow presentation seam the Decider depends on. It is defined here, at
// the consuming package, so the decision logic is unit-testable with a fake and never
// forces a caller to depend on Bubble Tea. Interface segregation: two methods, each
// with a single responsibility.
type UI interface {
	// ConfirmDrift renders the warn changes and blocks for a user answer, honoring
	// ctx (a cancelled/expired ctx must return promptly). note is an optional
	// user-authored message recorded on the resulting adoption.
	ConfirmDrift(ctx context.Context, warns []event.DriftChange) (accept bool, note string, err error)
	// Notify surfaces accepted informational drift without blocking.
	Notify(infos []event.DriftChange)
}

// Decider is an interactive session.RestoreDecider. It auto-accepts info-only drift
// (surfacing it via UI.Notify) and interactively confirms warn-level drift via
// UI.ConfirmDrift.
//
// An ACCEPTING decision is always attributed to the user (event.DecisionSourceUser):
// info-only drift is accepted on the user's behalf at startup, and warn-level drift is
// accepted only after the user explicitly confirms. A ctx cancellation or timeout
// surfaces from ConfirmDrift as an error, which the Decider returns alongside a
// non-accepting decision — harness treats a decider error as a fail-secure rejection
// carrying the cause.
type Decider struct {
	ui UI
}

// NewDecider builds a Decider backed by ui.
func NewDecider(ui UI) Decider { return Decider{ui: ui} }

// DecideRestore partitions the assessment into warn and info changes, auto-accepts
// when there are no warns (notifying on any infos), and otherwise blocks on an
// interactive confirmation. See Decider for the Source and ctx-cancellation contract.
func (d Decider) DecideRestore(ctx context.Context, a event.DriftAssessment) (session.RestoreDecision, error) {
	var warns, infos []event.DriftChange
	for _, change := range a.Changes {
		if change.Severity == event.DriftWarn {
			warns = append(warns, change)
		} else {
			infos = append(infos, change)
		}
	}

	if len(warns) == 0 {
		if len(infos) > 0 {
			d.ui.Notify(infos)
		}
		return session.RestoreDecision{Accept: true, Source: event.DecisionSourceUser}, nil
	}

	accept, note, err := d.ui.ConfirmDrift(ctx, warns)
	if err != nil {
		// A ctx timeout/cancel surfaces here; return the cause with a fail-secure
		// non-accepting decision.
		return session.RestoreDecision{Accept: false}, err
	}
	if !accept {
		return session.RestoreDecision{Accept: false, Source: event.DecisionSourceUser}, nil
	}
	return session.RestoreDecision{Accept: true, Source: event.DecisionSourceUser, Message: note}, nil
}
