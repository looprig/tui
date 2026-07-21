package presentation

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type sessionBrowserFake struct {
	sessions []SessionSummary
	resumed  SessionID
	fresh    Agent
}

func (f *sessionBrowserFake) ListSessions(context.Context) ([]SessionSummary, error) {
	return append([]SessionSummary(nil), f.sessions...), nil
}
func (f *sessionBrowserFake) ResumeSession(_ context.Context, id SessionID) (Agent, error) {
	f.resumed = id
	return f.fresh, nil
}

func TestSessionBrowserOptionAddsSessionsAndResumeCommands(t *testing.T) {
	agent := &fakeAgent{activeLoopID: callID(1)}
	browser := &sessionBrowserFake{}
	screen := New(context.Background(), agent, func(context.Context) (Agent, error) { return agent, nil }, AgentBanner{}, WithSessionBrowser(browser))
	want := map[string]bool{"/sessions": false, "/resume": false}
	for _, command := range screen.interaction.slashCommands {
		if _, ok := want[command.Name]; ok {
			want[command.Name] = true
		}
	}
	for command, found := range want {
		if !found {
			t.Errorf("dynamic slash catalog missing %s", command)
		}
	}
}

func TestIdleSessionResumeUsesCloseBeforeOpenHandoff(t *testing.T) {
	old := &fakeAgent{activeLoopID: callID(1)}
	fresh := &fakeAgent{activeLoopID: callID(2)}
	browser := &sessionBrowserFake{fresh: fresh}
	screen := New(context.Background(), old, func(context.Context) (Agent, error) { return old, nil }, AgentBanner{}, WithSessionBrowser(browser))
	id := callID(3)
	msg := screen.beginSessionResume(id)().(reopenResultMsg)
	if msg.err != nil || msg.agent != fresh || browser.resumed != id {
		t.Fatalf("resume result=%#v selected=%s", msg, browser.resumed)
	}
	if old.closeCalls != 1 {
		t.Fatalf("old Close calls = %d, want 1", old.closeCalls)
	}
}

var _ SessionBrowser = (*sessionBrowserFake)(nil)

// presenterAgent is a fakeAgent that ALSO implements SessionPresenter, so a resumed
// session supplies its own security-context presentation (fixed profile, workspace,
// pre-gate diagnostics) — the additive contract CodeRig Phase 5 fills on its agent.
type presenterAgent struct {
	*fakeAgent
	pres SessionPresentation
}

func (p presenterAgent) SessionPresentation() SessionPresentation { return p.pres }

var _ SessionPresenter = presenterAgent{}

// committedJoined joins every committed transcript entry's plain text so a test can
// assert which diagnostics the reopen banner surfaced.
func committedJoined(m Screen) string {
	var b strings.Builder
	for _, e := range m.transcript.testCommitted() {
		b.WriteString(committedText(e))
		b.WriteByte('\n')
	}
	return b.String()
}

// TestBrowserResumeRefreshesSecurityPresentation pins the fix for the stale
// security-context bug: a cross-session browser resume can land on a session with a
// DIFFERENT workspace root and DIFFERENT fixed access profile, so after the resume the
// footer profile/workspace and the pre-gate permission diagnostics must reflect the
// RESUMED session (supplied by the resumed Agent via SessionPresenter), never the prior
// session's context.
func TestBrowserResumeRefreshesSecurityPresentation(t *testing.T) {
	old := &fakeAgent{activeLoopID: callID(1)}
	fresh := presenterAgent{
		fakeAgent: &fakeAgent{activeLoopID: callID(2)},
		pres: SessionPresentation{
			ProfileName:           "ReadOnly",
			WorkspaceRoot:         "/resumed/root",
			PermissionDiagnostics: []string{"resumed diag"},
		},
	}
	browser := &sessionBrowserFake{fresh: fresh}
	m := New(context.Background(), old, fakeOpen(old), AgentBanner{Name: "CodeRig"},
		WithSessionBrowser(browser),
		WithSessionPresentation(SessionPresentation{ProfileName: "Writable", WorkspaceRoot: "/prior/root", PermissionDiagnostics: []string{"prior diag"}}))
	m.restoring = false
	m, _ = updateScreen(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	msg := m.beginSessionResume(callID(3))().(reopenResultMsg)
	m, _ = updateScreen(t, m, msg)

	footer := stripANSI(m.footerView())
	if !strings.Contains(footer, "ReadOnly") || !strings.Contains(footer, "/resumed/root") {
		t.Errorf("footer = %q, want the RESUMED session's fixed profile + workspace", footer)
	}
	if strings.Contains(footer, "Writable") || strings.Contains(footer, "/prior/root") {
		t.Errorf("footer = %q, still shows the PRIOR session's security context", footer)
	}
	if joined := committedJoined(m); !strings.Contains(joined, "resumed diag") || strings.Contains(joined, "prior diag") {
		t.Errorf("committed = %q, want resumed diagnostics only", joined)
	}
}

// TestBrowserResumeClearsPresentationWhenAgentSuppliesNone pins the fail-safe: a
// cross-session browser resume onto an Agent that does NOT implement SessionPresenter
// CLEARS the presentation (show nothing) rather than retaining the prior session's
// profile/workspace/diagnostics. Showing nothing is acceptable; showing a different
// session's security context is not.
func TestBrowserResumeClearsPresentationWhenAgentSuppliesNone(t *testing.T) {
	old := &fakeAgent{activeLoopID: callID(1)}
	fresh := &fakeAgent{activeLoopID: callID(2)} // no SessionPresenter
	browser := &sessionBrowserFake{fresh: fresh}
	m := New(context.Background(), old, fakeOpen(old), AgentBanner{Name: "CodeRig"},
		WithSessionBrowser(browser),
		WithSessionPresentation(SessionPresentation{ProfileName: "Writable", WorkspaceRoot: "/prior/root", PermissionDiagnostics: []string{"prior diag"}}))
	m.restoring = false
	m, _ = updateScreen(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	msg := m.beginSessionResume(callID(3))().(reopenResultMsg)
	m, _ = updateScreen(t, m, msg)

	footer := stripANSI(m.footerView())
	if strings.Contains(footer, "Writable") || strings.Contains(footer, "/prior/root") {
		t.Errorf("footer = %q, want the prior security context CLEARED on resume", footer)
	}
	if joined := committedJoined(m); strings.Contains(joined, "prior diag") {
		t.Errorf("committed = %q, want the prior diagnostics dropped on resume", joined)
	}
}

// TestClearRetainsPresentationWhenAgentSuppliesNone guards the /clear semantics: /clear
// stays in the SAME session family (same workspace + fixed profile), so when the reopened
// Agent supplies no presentation the construction-time value is RETAINED (still correct),
// not cleared. This is the case beginSessionResume must NOT regress.
func TestClearRetainsPresentationWhenAgentSuppliesNone(t *testing.T) {
	old := &fakeAgent{activeLoopID: callID(1)}
	fresh := &fakeAgent{activeLoopID: callID(2)} // no SessionPresenter
	m := New(context.Background(), old, fakeOpen(old), AgentBanner{Name: "CodeRig"},
		WithSessionPresentation(SessionPresentation{ProfileName: "Writable", WorkspaceRoot: "/prior/root"}))
	m.restoring = false
	m, _ = updateScreen(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	// A /clear reopen never sets m.resuming; model it with a direct reopen result.
	m, _ = updateScreen(t, m, reopenResultMsg{agent: fresh})

	footer := stripANSI(m.footerView())
	if !strings.Contains(footer, "Writable") || !strings.Contains(footer, "/prior/root") {
		t.Errorf("footer = %q, want the same-family profile + workspace retained on /clear", footer)
	}
}
