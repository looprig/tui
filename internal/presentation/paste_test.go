package presentation

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/tui/components"
)

// TestBracketedPasteInsertsIntoComposer pins the bracketed-paste contract: Bubble Tea v2
// delivers pasted text as a distinct tea.PasteMsg (NOT a KeyPressMsg carrying a paste flag,
// as v1 did), so Screen.Update must route it to the composer or the paste is silently lost.
func TestBracketedPasteInsertsIntoComposer(t *testing.T) {
	agent := &fakeAgent{activeLoopID: callID(1)}
	m := newScreenSized(t, agent, 80, 24)

	m, _ = updateScreen(t, m, tea.PasteMsg{Content: "hello world"})

	if got := m.interaction.input.Value(); got != "hello world" {
		t.Fatalf("composer value after paste = %q, want %q", got, "hello world")
	}
}

// TestMultilinePasteDoesNotSubmit guards the property that makes pasting a code block
// safe: the newlines inside a paste are TEXT, not Enter keypresses. Because a paste
// arrives as one message rather than as its constituent keys, it can never trip the
// composer's Enter-submits binding mid-way and fire off a half-pasted turn.
func TestMultilinePasteDoesNotSubmit(t *testing.T) {
	agent := &fakeAgent{activeLoopID: callID(1)}
	m := newScreenSized(t, agent, 80, 24)

	m, _ = updateScreen(t, m, tea.PasteMsg{Content: "func main() {\n\tprintln(\"hi\")\n}"})

	got := m.interaction.input.Value()
	if !strings.Contains(got, "func main() {") || !strings.Contains(got, "println(\"hi\")") {
		t.Fatalf("composer value after multiline paste = %q, want the whole block", got)
	}
	if strings.Count(got, "\n") != 2 {
		t.Errorf("newline count = %d, want 2 (newlines kept as text)", strings.Count(got, "\n"))
	}
	if agent.submitCalled || agent.submitToLoopCalled {
		t.Error("a multiline paste submitted a turn; it must only fill the composer")
	}
}

// TestPasteFillsFreeTextAnswerPrompt covers the other text-entry mode: when a tool asks a
// free-text question the input box IS the answer field, so a paste must land there too.
func TestPasteFillsFreeTextAnswerPrompt(t *testing.T) {
	m := newInteractionModel()
	m = m.ApplyEvent(event.UserInputRequested{ToolExecutionID: callID(3), Question: "name?", Choices: nil})
	if m.mode != modeAnswerPrompt {
		t.Fatalf("mode = %d, want modeAnswerPrompt", m.mode)
	}

	m, _ = m.ForwardToEditor(tea.PasteMsg{Content: "pasted answer"})

	if got := m.input.Value(); got != "pasted answer" {
		t.Fatalf("answer box after paste = %q, want %q", got, "pasted answer")
	}
}

// TestPastedSlashCommandOpensTheCompletionPanel proves a paste refreshes the completion
// panels just as typing does — pasting "/cle" must offer the command panel, not sit there
// as inert text. It asserts against the TYPED outcome rather than a hardcoded expectation
// so it keeps testing "paste behaves like typing" even if the command table changes.
//
// It also guards the value-gate added for timer-driven messages: the gate must still let a
// real content change through to refreshCompletion.
func TestPastedSlashCommandOpensTheCompletionPanel(t *testing.T) {
	agent := &fakeAgent{activeLoopID: callID(1)}

	pasted := newScreenSized(t, agent, 80, 24)
	beforeEpoch := pasted.trayGlowEpoch
	pasted, cmd := updateScreen(t, pasted, tea.PasteMsg{Content: "/cle"})

	typed := newScreenSized(t, agent, 80, 24)
	for _, r := range "/cle" {
		typed, _ = updateScreen(t, typed, tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	if typed.interaction.slash == nil {
		t.Fatal("typing a command prefix left the panel closed; the fixture no longer exercises completion")
	}
	if pasted.interaction.slash == nil {
		t.Fatal("pasting a command prefix left the completion panel closed, but typing it opens it")
	}
	if got, want := pasted.interaction.input.Value(), typed.interaction.input.Value(); got != want {
		t.Errorf("pasted composer = %q, typed composer = %q; a paste must match typing", got, want)
	}
	if pasted.trayGlowEpoch != beforeEpoch+1 {
		t.Errorf("pasted tray glow epoch = %d, want %d after opening", pasted.trayGlowEpoch, beforeEpoch+1)
	}
	if pasted.trayGlowFrame != 0 {
		t.Errorf("pasted tray glow frame = %d, want 0 after opening", pasted.trayGlowFrame)
	}
	if cmd == nil {
		t.Error("pasted tray opening did not schedule the background transition")
	}
}

// TestPasteIsInertWhileTrayOwnsTheKeyboard pins the precedence guard. A /model-style tray
// is a modal list with no text field; it swallows every key it does not use. A paste must
// follow suit rather than silently accumulating in the composer hidden behind the tray,
// which the user would only discover after dismissing it.
func TestPasteIsInertWhileTrayOwnsTheKeyboard(t *testing.T) {
	agent := &fakeAgent{activeLoopID: callID(1)}
	m := newScreenSized(t, agent, 80, 24)
	m.runtimeTray = components.NewValueComplete([]components.ValueItem{{ID: "sonnet", Label: "Sonnet"}}, "")

	m, _ = updateScreen(t, m, tea.PasteMsg{Content: "leaked"})

	if got := m.interaction.input.Value(); got != "" {
		t.Fatalf("composer value = %q, want empty (the tray owns the keyboard)", got)
	}
}
