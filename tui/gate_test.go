package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/tool"
)

// subagentStep builds a SUBAGENT loop's collapsed StepDone (a non-primary loop id) that
// commits an Enduring assistant entry — the kind of commit that streams in while the
// PRIMARY loop is blocked on a permission gate. lines controls its rendered height.
func subagentStep(lines int) event.Event {
	body := make([]string, 0, lines)
	for i := 0; i < lines; i++ {
		body = append(body, "subagent line "+strconv.Itoa(i))
	}
	// callID(9) is a fixed non-zero loop id, distinct from the fake agent's zero
	// primaryLoopID, so this is attributed to a subagent and never finalizes the primary.
	return stepDoneFrom(callID(9), aiMessage("", strings.Join(body, "\n")))
}

// TestPromptGateHoldsCommits is the scrollback-strand fix: while a permission gate owns
// the bottom surface, a sibling/subagent commit must be HELD in the surface, NOT emitted
// to scrollback. Emitting it (printToScrollback → insertAbove) while the box is up
// repaints and strands the box + its "awaiting approval" status line into native history
// — the reported "awaiting approval shown multiple times, box never went away" symptom.
// With no prompt active the same commit prints straight to scrollback (heldLines stays
// empty), the control case that proves the gate is what changes the behavior.
func TestPromptGateHoldsCommits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		gated         bool // a permission prompt is active before the commit
		wantHeldGrows bool // the commit is held in the surface (true) or printed (false)
	}{
		{name: "prompt active holds commit in surface", gated: true, wantHeldGrows: true},
		{name: "no prompt prints commit to scrollback", gated: false, wantHeldGrows: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := runningScreen(t, &fakeAgent{})
			m, _ = updateScreen(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

			if tt.gated {
				m = feed(t, m, event.PermissionRequested{
					ToolExecutionID: callID(1),
					Request:         tool.BashRequest{Command: "grep -n x main.go"},
				})
				if !m.scrollbackGated() {
					t.Fatal("setup: permission event did not gate scrollback")
				}
			}

			before := len(m.heldLines)
			m = feed(t, m, subagentStep(3))
			grew := len(m.heldLines) > before

			if grew != tt.wantHeldGrows {
				t.Errorf("heldLines grew = %v (%d -> %d), want %v", grew, before, len(m.heldLines), tt.wantHeldGrows)
			}
			if tt.gated {
				// The box must NOT vanish while commits stream in (the reported symptom):
				// the commit is held ABOVE the still-present box, not written under it.
				if v := stripANSI(m.View().Content); !strings.Contains(v, "Approve Bash?") {
					t.Errorf("permission box vanished while a commit streamed in; view = %q", v)
				}
			}
		})
	}
}

// TestFlushGatedHoldsThenReleases locks flush()'s gate-awareness — the out-of-band
// commit path (errors, notices) must not strand the box either. While a prompt gate is
// active, flush HOLDS its entries in the surface (returns no print command); once the
// gate resolves, flush drains the whole held tail to scrollback.
func TestFlushGatedHoldsThenReleases(t *testing.T) {
	t.Parallel()

	m := runningScreen(t, &fakeAgent{})
	m, _ = updateScreen(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// Gate, then commit an out-of-band error and flush it.
	m = feed(t, m, event.PermissionRequested{
		ToolExecutionID: callID(1),
		Request:         tool.BashRequest{Command: "grep -n x main.go"},
	})
	m.transcript = m.transcript.CommitError(errors.New("egress overflow"))
	if cmd := m.flush(); cmd != nil {
		t.Error("flush emitted a scrollback print while gated (would strand the box); want nil (held)")
	}
	if len(m.heldLines) == 0 {
		t.Fatal("gated flush did not hold the error entry in the surface")
	}
	if v := stripANSI(m.View().Content); !strings.Contains(v, "Approve Bash?") {
		t.Errorf("permission box vanished after a gated flush; view = %q", v)
	}

	// Resolve the gate (approve), then flush again — it must now drain to scrollback.
	m, _ = updateScreen(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.scrollbackGated() {
		t.Fatal("approving did not clear the gate (prompt still active)")
	}
	if cmd := m.flush(); cmd == nil {
		t.Error("flush produced no print command after the gate resolved; want the held tail drained to scrollback")
	}
	if len(m.heldLines) != 0 {
		t.Errorf("held tail not drained after resolution: heldLines = %d, want 0", len(m.heldLines))
	}
}
