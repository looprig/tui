package presentation

import (
	"context"
	"testing"
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
