package presentation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

type sessionBrowserFake struct {
	sessions  []SessionSummary
	resumed   SessionID
	fresh     Agent
	resumeErr error
}

func (f *sessionBrowserFake) ListSessions(context.Context) ([]SessionSummary, error) {
	return append([]SessionSummary(nil), f.sessions...), nil
}
func (f *sessionBrowserFake) ResumeSession(_ context.Context, id SessionID) (Agent, error) {
	f.resumed = id
	if f.resumeErr != nil {
		return nil, f.resumeErr
	}
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

// TestSessionResumeRejectionFallsBackToFreshSession proves a rejected/failed resume (e.g.
// harness's RestoreRejectedError from config drift) does not surface as a plain reopen
// failure: it falls back to opening a fresh session — the same path /clear uses — carrying
// the original error as a non-fatal warning rather than a fatal one, so the caller never
// has to choose between losing the TUI and pretending the resume succeeded.
func TestSessionResumeRejectionFallsBackToFreshSession(t *testing.T) {
	old := &fakeAgent{activeLoopID: callID(1)}
	fresh := &fakeAgent{activeLoopID: callID(2)}
	resumeErr := errors.New("session: restore rejected by policy: 1 warn category (runtime); 0 info changes")
	browser := &sessionBrowserFake{resumeErr: resumeErr}
	screen := New(context.Background(), old, func(context.Context) (Agent, error) { return fresh, nil }, AgentBanner{}, WithSessionBrowser(browser))
	id := callID(3)

	msg := screen.beginSessionResume(id)().(reopenResultMsg)
	if msg.err != nil {
		t.Fatalf("reopen result err = %v, want nil (fallback should recover)", msg.err)
	}
	if msg.agent != fresh {
		t.Fatalf("reopen result agent = %#v, want the fallback fresh session", msg.agent)
	}
	if browser.resumed != id {
		t.Fatalf("resumed session id = %s, want %s", browser.resumed, id)
	}
	if !errors.Is(msg.warning, resumeErr) {
		t.Fatalf("reopen result warning = %v, want it to be/wrap %v", msg.warning, resumeErr)
	}
	if old.closeCalls != 1 {
		t.Fatalf("old Close calls = %d, want 1", old.closeCalls)
	}
}

// TestSessionResumeRejectionStaysAliveWithWarningNotice drives the fallback through
// Update, proving the TUI stays live (a real agent, not stuck in StatusResetting) and
// surfaces the original rejection as a committed notice instead of silently swallowing
// it.
func TestSessionResumeRejectionStaysAliveWithWarningNotice(t *testing.T) {
	old := &fakeAgent{activeLoopID: callID(1)}
	fresh := &fakeAgent{activeLoopID: callID(2)}
	resumeErr := errors.New("session: restore rejected by policy: 1 warn category (runtime); 0 info changes")
	browser := &sessionBrowserFake{resumeErr: resumeErr}
	m := New(context.Background(), old, func(context.Context) (Agent, error) { return fresh, nil }, AgentBanner{Name: "CodeRig"}, WithSessionBrowser(browser))
	m.restoring = false
	m, _ = updateScreen(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	msg := m.beginSessionResume(callID(3))().(reopenResultMsg)
	m, _ = updateScreen(t, m, msg)

	if m.agent != fresh {
		t.Fatalf("agent after rejected resume = %#v, want the fallback fresh session (TUI must stay live)", m.agent)
	}
	if m.status == StatusResetting {
		t.Fatalf("status after rejected resume = %v, want NOT stuck in Resetting", m.status)
	}
	if joined := committedJoined(m); !strings.Contains(joined, resumeErr.Error()) {
		t.Errorf("committed = %q, want the rejection surfaced as a notice", joined)
	}
}

// TestSessionResumeRejectionStaysFatalWhenFallbackAlsoFails proves the fallback is not a
// blanket error swallow: if even a fresh session cannot be opened, there is genuinely
// nowhere left to land, and the reopen must still surface as a real (fatal) error carrying
// both failures.
func TestSessionResumeRejectionStaysFatalWhenFallbackAlsoFails(t *testing.T) {
	old := &fakeAgent{activeLoopID: callID(1)}
	resumeErr := errors.New("session: restore rejected by policy")
	fallbackErr := errors.New("open: disk full")
	browser := &sessionBrowserFake{resumeErr: resumeErr}
	screen := New(context.Background(), old, func(context.Context) (Agent, error) { return nil, fallbackErr }, AgentBanner{}, WithSessionBrowser(browser))

	msg := screen.beginSessionResume(callID(3))().(reopenResultMsg)
	if msg.err == nil {
		t.Fatal("reopen result err = nil, want a fatal error when both resume and fallback fail")
	}
	if !errors.Is(msg.err, resumeErr) || !errors.Is(msg.err, fallbackErr) {
		t.Fatalf("reopen result err = %v, want it to wrap both %v and %v", msg.err, resumeErr, fallbackErr)
	}
	if msg.agent != nil {
		t.Fatalf("reopen result agent = %#v, want nil", msg.agent)
	}
}

// TestSessionsListedShowsLastUsedFallingBackToCreated pins the picker's second row to
// recency, not creation: a session that has actually run a turn shows when it was last
// active; a session that never ran one (LastActiveAt stays the zero value) falls back to
// when it was opened, rather than showing a zero/blank date.
func TestSessionsListedShowsLastUsedFallingBackToCreated(t *testing.T) {
	agent := &fakeAgent{activeLoopID: callID(1)}
	active := SessionID(callID(2))
	neverActive := SessionID(callID(3))
	browser := &sessionBrowserFake{sessions: []SessionSummary{
		{ID: active, Title: "active session", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), LastActiveAt: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)},
		{ID: neverActive, Title: "never active", CreatedAt: time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)},
	}}
	m := New(context.Background(), agent, func(context.Context) (Agent, error) { return agent, nil }, AgentBanner{}, WithSessionBrowser(browser))

	m, _ = updateScreen(t, m, sessionsListedMsg{sessions: browser.sessions})

	if m.sessionTray == nil {
		t.Fatal("sessionTray is nil after sessionsListedMsg")
	}
	if got := m.sessionTray.Selected().LastUsed; got != "2026-07-15" {
		t.Errorf("active session LastUsed = %q, want 2026-07-15 (LastActiveAt)", got)
	}
	m.sessionTray.Down()
	if got := m.sessionTray.Selected().LastUsed; got != "2026-07-14" {
		t.Errorf("never-active session LastUsed = %q, want 2026-07-14 (falls back to CreatedAt)", got)
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
