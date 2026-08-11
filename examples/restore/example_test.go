package restore_test

import (
	"context"
	"fmt"

	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/tui/restore"
)

type decisionUI struct {
	accept bool
}

func (ui *decisionUI) Notify(infos []event.DriftChange) {
	fmt.Println("informational changes:", len(infos))
}

func (ui *decisionUI) ConfirmDrift(_ context.Context, warns []event.DriftChange) (bool, string, error) {
	fmt.Println("changes requiring confirmation:", len(warns))
	return ui.accept, "reviewed at startup", nil
}

func Example_restoreDecision() {
	ui := &decisionUI{}
	decider := restore.NewDecider(ui)

	infoDecision, err := decider.DecideRestore(context.Background(), event.DriftAssessment{
		Changes: []event.DriftChange{{Category: event.DriftModel, Severity: event.DriftInfo}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("info-only restore accepted:", infoDecision.Accept)

	warnDecision, err := decider.DecideRestore(context.Background(), event.DriftAssessment{
		Changes: []event.DriftChange{{Category: event.DriftWorkspace, Severity: event.DriftWarn}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("unconfirmed expansion accepted:", warnDecision.Accept)

	// Output:
	// informational changes: 1
	// info-only restore accepted: true
	// changes requiring confirmation: 1
	// unconfirmed expansion accepted: false
}
