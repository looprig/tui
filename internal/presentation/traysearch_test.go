package presentation

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/looprig/tui/components"
)

func TestSessionTraySearchUsesInputAndReplacesWorkspaceFooter(t *testing.T) {
	t.Parallel()

	m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
	m.presentation = SessionPresentation{WorkspaceRoot: "/workspace", ProfileName: "writable"}
	m, _ = updateScreen(t, m, sessionsListedMsg{sessions: []SessionSummary{
		{ID: callID(2), Title: "Migration", Description: "billing backfill", CreatedAt: time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)},
		{ID: callID(3), Title: "Release", Description: "deployment check", CreatedAt: time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)},
	}})
	if m.sessionTray == nil {
		t.Fatal("session tray is nil after listing sessions")
	}

	m, _ = updateScreen(t, m, tea.PasteMsg{Content: "billing"})
	if got := m.interaction.input.Value(); got != "billing" {
		t.Fatalf("search input = %q, want pasted session query", got)
	}
	if got := m.sessionTray.Selected().Title; got != "Migration" {
		t.Errorf("filtered session = %q, want description match Migration", got)
	}
	if input := stripANSI(m.bottomBoxView()); !strings.Contains(input, "billing") {
		t.Errorf("bottom input = %q, want the active session query", input)
	}
	footer := stripANSI(m.footerView())
	if !strings.Contains(footer, "search description or ID") || !strings.Contains(footer, "Enter resume") {
		t.Errorf("session footer = %q, want session-specific search and resume help", footer)
	}
	if strings.Contains(footer, "/workspace") || strings.Contains(footer, "[WRITABLE]") {
		t.Errorf("session footer = %q, want workspace metadata temporarily replaced by help", footer)
	}

	m, _ = updateScreen(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.sessionTray != nil {
		t.Fatal("Esc left the session tray open")
	}
	if got := stripANSI(m.footerView()); !strings.Contains(got, "/workspace") || !strings.Contains(got, "[WRITABLE]") {
		t.Errorf("footer after close = %q, want workspace metadata restored", got)
	}
}

func TestModelTraySearchUsesInputAndReplacesWorkspaceFooter(t *testing.T) {
	t.Parallel()

	m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
	m.presentation = SessionPresentation{WorkspaceRoot: "/workspace", ProfileName: "writable"}
	m.runtimeTray = components.NewModelComplete([]components.ValueItem{
		{ID: "gpt-5.4", Provider: "OpenAI", Label: "GPT-5.4", Description: "coding"},
		{ID: "claude-sonnet-4.5", Provider: "Anthropic", Label: "Claude Sonnet 4.5", Description: "balanced"},
	})
	m.runtimeTrayKind = runtimeTrayModel

	m, _ = updateScreen(t, m, tea.PasteMsg{Content: "openai"})
	if got := m.interaction.input.Value(); got != "openai" {
		t.Fatalf("search input = %q, want pasted model query", got)
	}
	if got := m.runtimeTray.Selected().ID; got != "gpt-5.4" {
		t.Errorf("filtered model = %q, want provider match gpt-5.4", got)
	}
	footer := stripANSI(m.footerView())
	if !strings.Contains(footer, "search provider or name") || !strings.Contains(footer, "Enter choose") {
		t.Errorf("model footer = %q, want model-specific search and choose help", footer)
	}
	if strings.Contains(footer, "/workspace") || strings.Contains(footer, "[WRITABLE]") {
		t.Errorf("model footer = %q, want workspace metadata temporarily replaced by help", footer)
	}
}

func TestNoMatchTraySearchLeavesEnterInert(t *testing.T) {
	t.Parallel()

	t.Run("sessions", func(t *testing.T) {
		m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
		m, _ = updateScreen(t, m, sessionsListedMsg{sessions: []SessionSummary{
			{ID: callID(2), Title: "Migration", CreatedAt: time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC)},
		}})
		m, _ = updateScreen(t, m, tea.PasteMsg{Content: "no such session"})
		if got := m.sessionTray.Selected(); got.ID != "" {
			t.Fatalf("no-match session selection = %#v, want zero value", got)
		}

		m, _ = updateScreen(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.sessionTray == nil {
			t.Fatal("Enter closed a zero-match session tray, want it to remain open")
		}
	})

	t.Run("models", func(t *testing.T) {
		m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
		m.runtimeTray = components.NewModelComplete([]components.ValueItem{{
			ID: "gpt-5.4", Provider: "OpenAI", Label: "GPT-5.4",
		}})
		m.runtimeTrayKind = runtimeTrayModel
		m, _ = updateScreen(t, m, tea.PasteMsg{Content: "no such model"})
		if got := m.runtimeTray.Selected(); got.ID != "" {
			t.Fatalf("no-match model selection = %#v, want zero value", got)
		}

		m, _ = updateScreen(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
		if m.runtimeTray == nil {
			t.Fatal("Enter closed a zero-match model tray, want it to remain open")
		}
	})
}

func TestNonModelRuntimeTrayDismissalPreservesComposerDraft(t *testing.T) {
	t.Parallel()

	m := newScreenSized(t, &fakeAgent{activeLoopID: callID(1)}, 80, 24)
	m.interaction.input.SetValue("keep this draft")
	m.runtimeTray = components.NewValueComplete([]components.ValueItem{{ID: "review", Label: "Review"}}, "")
	m.runtimeTrayKind = runtimeTrayMode

	m, _ = updateScreen(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if got := m.interaction.input.Value(); got != "keep this draft" {
		t.Errorf("composer after closing a non-search tray = %q, want preserved draft", got)
	}
}
