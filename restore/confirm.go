package restore

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/harness/pkg/event"
)

// confirmModel is the Bubble Tea confirm presentation for warn-level drift. It renders
// the warn changes and a y/n prompt, recording the outcome. It follows the module's
// component idiom — a small model advanced by Update — adapted to bubbletea v2's
// Update(tea.Msg) (tea.Model, tea.Cmd) / View() tea.View interface (see NOTE below).
//
// The pointer receiver mutates in place; Update returns the same pointer so the running
// program observes the recorded outcome. y accepts; n or esc declines. Any other key is
// ignored so a stray keypress cannot silently resolve the prompt.
type confirmModel struct {
	warns    []event.DriftChange
	accepted bool
	declined bool
}

func newConfirmModel(warns []event.DriftChange) *confirmModel {
	return &confirmModel{warns: append([]event.DriftChange(nil), warns...)}
}

// Init satisfies tea.Model; the confirm prompt has no startup command.
func (m *confirmModel) Init() tea.Cmd { return nil }

// Update records the y/n/esc outcome and quits the program on a decision.
func (m *confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.Code {
	case 'y', 'Y':
		m.accepted = true
		return m, tea.Quit
	case 'n', 'N', tea.KeyEsc:
		m.declined = true
		return m, tea.Quit
	}
	return m, nil
}

// viewString is the pure string rendering of the prompt, kept separate from View so it
// is directly assertable in tests without inspecting a tea.View.
func (m *confirmModel) viewString() string {
	var b strings.Builder
	b.WriteString("Session configuration drift requires confirmation:\n\n")
	for _, line := range formatChanges(m.warns) {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\nAccept these changes and restore the session? [y/N]")
	return b.String()
}

// View satisfies tea.Model, wrapping viewString in a tea.View.
func (m *confirmModel) View() tea.View { return tea.NewView(m.viewString()) }

// program is the narrow slice of *tea.Program the terminal UI drives: run the confirm
// prompt to completion and force-quit it on ctx cancellation. *tea.Program satisfies
// it; defining it here lets tests fake the program without a terminal. Mirrors the
// seam in runtime/run.go.
type program interface {
	Run() (tea.Model, error)
	Quit()
}

// newProgram is the Bubble Tea program-construction seam: it builds a program for the
// model. It is a package-level var so tests can swap it for a fake (a real tea.Program
// needs a terminal). The default builds a real *tea.Program.
var newProgram = func(model tea.Model, opts ...tea.ProgramOption) program {
	return tea.NewProgram(model, opts...)
}

// terminalUI is the production UI: it confirms warn drift by running confirmModel as a
// short-lived tea.Program, and notifies info drift by printing to stderr (there is no
// running TUI at startup, so a non-blocking print is the right surface).
type terminalUI struct{}

// NewTerminalUI builds the production UI backing a Decider.
func NewTerminalUI() UI { return terminalUI{} }

// ConfirmDrift runs the confirm prompt, watching ctx so a cancellation/timeout
// force-quits the program and is reported as (false, "", ctx.Err()). A nil ctx error
// with a non-accepting model is a plain decline.
func (terminalUI) ConfirmDrift(ctx context.Context, warns []event.DriftChange) (bool, string, error) {
	prog := newProgram(newConfirmModel(warns))

	// Force-quit the program if ctx is cancelled before the user answers. done releases
	// the watcher when Run returns; watcherDone is joined before ConfirmDrift returns so
	// the goroutine never outlives the call.
	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			prog.Quit()
		case <-done:
		}
	}()

	final, err := prog.Run()
	close(done)
	<-watcherDone
	if err != nil {
		return false, "", err
	}
	// A ctx cancellation that force-quit the program surfaces as a rejection carrying
	// the cause, even though Run itself returned no error.
	if cerr := ctx.Err(); cerr != nil {
		return false, "", cerr
	}

	model, ok := final.(*confirmModel)
	if !ok || !model.accepted {
		return false, "", nil
	}
	return true, "", nil
}

// Notify prints the accepted informational drift to stderr, one change per line. It is
// non-blocking and best-effort — there is no interactive UI at startup.
func (terminalUI) Notify(infos []event.DriftChange) {
	for _, line := range formatChanges(infos) {
		fmt.Fprintln(os.Stderr, line)
	}
}
