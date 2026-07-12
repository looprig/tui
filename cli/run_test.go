package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/looprig/cli/tui"
	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/tool"
)

// fakeAgent is a no-op tui.Agent stand-in: construction-success path needs a live
// agent so Run can build the TUI model and bound a teardown Close. Every method is a
// benign no-op; Close records that teardown ran.
type fakeAgent struct {
	loopID uuid.UUID
	closed *bool
}

func (a *fakeAgent) Submit(context.Context, []content.Block) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (a *fakeAgent) SubmitToLoop(context.Context, uuid.UUID, []content.Block) (uuid.UUID, error) {
	return uuid.UUID{}, nil
}
func (a *fakeAgent) RootLoopID() uuid.UUID                   { return a.loopID }
func (a *fakeAgent) ActiveLoopID() uuid.UUID                 { return a.loopID }
func (a *fakeAgent) Interrupt(context.Context) (bool, error) { return false, nil }
func (a *fakeAgent) AcceptsImages(uuid.UUID) bool            { return false }
func (a *fakeAgent) ReplayBacklog(context.Context) ([]event.Event, error) {
	return nil, nil
}
func (a *fakeAgent) Subscribe(event.EventFilter) (tui.EventStream, error) { return nil, nil }
func (a *fakeAgent) Approve(context.Context, uuid.UUID, uuid.UUID, tool.ApprovalScope) error {
	return nil
}
func (a *fakeAgent) Deny(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (a *fakeAgent) ProvideAnswer(context.Context, uuid.UUID, uuid.UUID, string) error {
	return nil
}
func (a *fakeAgent) Close(context.Context) error {
	if a.closed != nil {
		*a.closed = true
	}
	return nil
}

// fakeProgram is a program stand-in: Run reports a preset final model + error
// without touching a terminal; Quit is a no-op (no signal in these tests).
type fakeProgram struct {
	final tea.Model
	err   error
	onRun func()
}

func (p *fakeProgram) Run() (tea.Model, error) {
	if p.onRun != nil {
		p.onRun()
	}
	return p.final, p.err
}
func (p *fakeProgram) Quit() {}

// fakeHolder is a tea.Model that ALSO satisfies tui.AgentHolder, standing in for a final
// model whose live agent Run's teardown must resolve via the AgentHolder interface rather
// than the concrete Screen — e.g. the fresh agent a /clear swapped in mid-session.
// Its Agent() returns a distinct agent so a test can prove teardown closes the FINAL model's
// agent, not the initial one.
type fakeHolder struct {
	agent tui.Agent
}

func (f fakeHolder) Init() tea.Cmd                       { return nil }
func (f fakeHolder) Update(tea.Msg) (tea.Model, tea.Cmd) { return f, nil }
func (f fakeHolder) View() tea.View                      { return tea.NewView("") }
func (f fakeHolder) Agent() tui.Agent                    { return f.agent }

// Compile-time proof fakeHolder is exactly the two contracts the teardown path needs: a
// tea.Model (so the fake program can return it as its final model) and a tui.AgentHolder
// (so Run's teardown assertion resolves its Agent()).
var (
	_ tea.Model       = fakeHolder{}
	_ tui.AgentHolder = fakeHolder{}
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

// TestBannerAgentBanner proves Banner maps verbatim onto tui.AgentBanner (no
// defaulting, no field swap) so the startup notice shows exactly what the caller set.
func TestBannerAgentBanner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		banner Banner
		want   tui.AgentBanner
	}{
		{name: "name and description", banner: Banner{Name: "SWE", Description: "swarm"}, want: tui.AgentBanner{Name: "SWE", Description: "swarm"}},
		{name: "name only", banner: Banner{Name: "SWE"}, want: tui.AgentBanner{Name: "SWE"}},
		{name: "empty banner", banner: Banner{}, want: tui.AgentBanner{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.banner.agentBanner(); got != tt.want {
				t.Errorf("agentBanner() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestLogFilePath proves the log path resolves to <dir>/.looprig/looprig.log against a
// supplied home, joining with filepath.Join (no hardcoded separators).
func TestLogFilePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		home    string
		wantDir string
		wantLog string
	}{
		{
			name:    "typical home",
			home:    "/home/alice",
			wantDir: filepath.Join("/home/alice", logDirName),
			wantLog: filepath.Join("/home/alice", logDirName, logFileName),
		},
		{
			name:    "home with trailing slash",
			home:    "/root/",
			wantDir: filepath.Join("/root/", logDirName),
			wantLog: filepath.Join("/root/", logDirName, logFileName),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir, log := logFilePath(tt.home)
			if dir != tt.wantDir {
				t.Errorf("dir = %q, want %q", dir, tt.wantDir)
			}
			if log != tt.wantLog {
				t.Errorf("log = %q, want %q", log, tt.wantLog)
			}
		})
	}
}

func TestClearTerminalForFreshLaunch(t *testing.T) {
	writeErr := errors.New("write failed")
	success := &bytes.Buffer{}

	tests := []struct {
		name    string
		writer  io.Writer
		output  func() string
		want    string
		wantErr bool
	}{
		{
			name:   "clears visible screen and scrollback then homes cursor",
			writer: success,
			output: success.String,
			want:   "\x1b[2J\x1b[3J\x1b[H",
		},
		{
			name:    "writer failure is returned",
			writer:  failingWriter{err: writeErr},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := clearTerminalForFreshLaunch(tt.writer)
			if (err != nil) != tt.wantErr {
				t.Fatalf("clearTerminalForFreshLaunch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got := tt.output(); got != tt.want {
				t.Errorf("clearTerminalForFreshLaunch() wrote %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunClearsTerminalBeforeProgramRun(t *testing.T) {
	tests := []struct {
		name string
	}{
		{name: "success path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var order []string
			swapClearTerminal(t, func(io.Writer) error {
				order = append(order, "clear")
				return nil
			})
			swapNewProgram(t, func(m tea.Model, _ ...tea.ProgramOption) program {
				return &fakeProgram{
					final: m,
					onRun: func() {
						order = append(order, "run")
					},
				}
			})

			var closed bool
			newAgent := func(context.Context) (tui.Agent, error) {
				return &fakeAgent{loopID: newLoopID(t), closed: &closed}, nil
			}

			got := Run(context.Background(), newAgent, Banner{Name: "SWE"})
			if got != exitOK {
				t.Errorf("Run() exit = %d, want %d", got, exitOK)
			}
			if want := []string{"clear", "run"}; !equalStrings(order, want) {
				t.Errorf("order = %v, want %v", order, want)
			}
		})
	}
}

// TestRunConstructionError proves the newAgent-failure path returns the agent
// failure exit code and never panics (no model built, no program run).
//
// The Run* tests swap the package-level runProgram seam, so they share mutable
// global state and must NOT run in parallel with each other.
func TestRunConstructionError(t *testing.T) {
	var ran bool
	swapNewProgram(t, func(m tea.Model, _ ...tea.ProgramOption) program {
		ran = true
		return &fakeProgram{final: m}
	})

	boom := errors.New("construct failed")
	newAgent := func(context.Context) (tui.Agent, error) { return nil, boom }

	got := Run(context.Background(), newAgent, Banner{Name: "SWE"})
	if got != exitAgentError {
		t.Errorf("Run() exit = %d, want %d", got, exitAgentError)
	}
	if ran {
		t.Error("program ran despite construction failure")
	}
}

// TestRunHappyPath proves the success path: newAgent yields an agent, the program
// runs via the seam and returns no error, the agent is Closed at teardown, and Run
// returns exitOK.
func TestRunHappyPath(t *testing.T) {
	var progRan bool
	swapNewProgram(t, func(m tea.Model, _ ...tea.ProgramOption) program {
		progRan = true
		return &fakeProgram{final: m}
	})

	var closed bool
	newAgent := func(context.Context) (tui.Agent, error) {
		return &fakeAgent{loopID: newLoopID(t), closed: &closed}, nil
	}

	got := Run(context.Background(), newAgent, Banner{Name: "SWE"})
	if got != exitOK {
		t.Errorf("Run() exit = %d, want %d", got, exitOK)
	}
	if !progRan {
		t.Error("program seam was not invoked")
	}
	if !closed {
		t.Error("agent was not Closed at teardown")
	}
}

// TestRunProgramError proves a tea.Program run error maps to the agent-error exit
// code and still tears the agent down.
func TestRunProgramError(t *testing.T) {
	swapNewProgram(t, func(m tea.Model, _ ...tea.ProgramOption) program {
		return &fakeProgram{final: m, err: errors.New("run failed")}
	})

	var closed bool
	newAgent := func(context.Context) (tui.Agent, error) {
		return &fakeAgent{loopID: newLoopID(t), closed: &closed}, nil
	}

	got := Run(context.Background(), newAgent, Banner{Name: "SWE"})
	if got != exitAgentError {
		t.Errorf("Run() exit = %d, want %d", got, exitAgentError)
	}
	if !closed {
		t.Error("agent was not Closed after a run error")
	}
}

// TestRunBuildsModernScreen proves Run wires the MODERN VIEWPORT as the design: the model
// it hands the program seam is a tui.Screen (NewModern), not the legacy scrollback
// Screen. Rev 3 dropped the --modern flag / env / RunOption, so every entry point that calls
// Run now launches the viewport with no toggle.
//
// The Run* tests swap the package-level newProgram seam, so they share mutable global state
// and must NOT run in parallel with each other.
func TestRunBuildsModernScreen(t *testing.T) {
	var captured tea.Model
	swapNewProgram(t, func(m tea.Model, _ ...tea.ProgramOption) program {
		captured = m
		return &fakeProgram{final: m}
	})

	var closed bool
	newAgent := func(context.Context) (tui.Agent, error) {
		return &fakeAgent{loopID: newLoopID(t), closed: &closed}, nil
	}

	got := Run(context.Background(), newAgent, Banner{Name: "SWE"})
	if got != exitOK {
		t.Fatalf("Run() exit = %d, want %d", got, exitOK)
	}
	if _, ok := captured.(tui.Screen); !ok {
		t.Fatalf("Run built %T, want tui.Screen", captured)
	}
	if !closed {
		t.Error("agent was not Closed at teardown")
	}
}

// TestRunTeardownViaAgentHolder proves teardown resolves the agent to Close through the
// tui.AgentHolder interface off the FINAL model — not the concrete Screen and not the
// initially-constructed agent. The fake program returns a fakeHolder wrapping a DISTINCT agent
// (as a /clear swap would), so Run must Close that final-model agent and leave the initial one
// untouched.
func TestRunTeardownViaAgentHolder(t *testing.T) {
	var initialClosed, finalClosed bool
	finalAgent := &fakeAgent{loopID: newLoopID(t), closed: &finalClosed}
	swapNewProgram(t, func(_ tea.Model, _ ...tea.ProgramOption) program {
		return &fakeProgram{final: fakeHolder{agent: finalAgent}}
	})

	newAgent := func(context.Context) (tui.Agent, error) {
		return &fakeAgent{loopID: newLoopID(t), closed: &initialClosed}, nil
	}

	got := Run(context.Background(), newAgent, Banner{Name: "SWE"})
	if got != exitOK {
		t.Fatalf("Run() exit = %d, want %d", got, exitOK)
	}
	if !finalClosed {
		t.Error("final model's agent (resolved via tui.AgentHolder) was not Closed")
	}
	if initialClosed {
		t.Error("initial agent was Closed; teardown must prefer the final model's agent")
	}
}

// swapNewProgram replaces the package-level program-construction seam for the
// duration of the test and restores it on cleanup, so Run is exercised without a
// real terminal.
func swapNewProgram(t *testing.T, fn func(tea.Model, ...tea.ProgramOption) program) {
	t.Helper()
	prev := newProgram
	newProgram = fn
	t.Cleanup(func() { newProgram = prev })
}

func swapClearTerminal(t *testing.T, fn func(io.Writer) error) {
	t.Helper()
	prev := clearTerminalForFreshLaunch
	clearTerminalForFreshLaunch = fn
	t.Cleanup(func() { clearTerminalForFreshLaunch = prev })
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// newLoopID mints a non-zero loop id for a fake agent's RootLoopID.
func newLoopID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.New()
	if err != nil {
		t.Fatalf("uuid.New() error = %v", err)
	}
	return id
}
