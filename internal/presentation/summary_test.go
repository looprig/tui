package presentation

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTaskToolUseSummaryRedacted pins the durable presentation boundary for the
// loop-scoped task tools: the card keeps its concrete tool name, but its
// reconstructed detail is intentionally empty even when stored arguments contain
// task text, metadata, and identifiers.
func TestTaskToolUseSummaryRedacted(t *testing.T) {
	t.Parallel()

	const (
		subject     = "sensitive task subject"
		description = "sensitive task description"
		metadata    = "sensitive task metadata"
		taskID      = "11111111-2222-3333-4444-555555555555"
	)

	inputs := []struct {
		name  string
		input string
	}{
		{name: "TaskCreate", input: `{"subject":"` + subject + `","description":"` + description + `","metadata":{"secret":"` + metadata + `"},"taskId":"` + taskID + `"}`},
		{name: "TaskUpdate", input: `{"subject":"` + subject + `","description":"` + description + `","metadata":{"secret":"` + metadata + `"},"taskId":"` + taskID + `"}`},
		{name: "TaskGet", input: `{"subject":"` + subject + `","description":"` + description + `","metadata":{"secret":"` + metadata + `"},"taskId":"` + taskID + `"}`},
		{name: "TaskList", input: `{"subject":"` + subject + `","description":"` + description + `","metadata":{"secret":"` + metadata + `"},"taskId":"` + taskID + `"}`},
	}
	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := toolUseSummary(tt.name, json.RawMessage(tt.input))
			if got != "" {
				t.Fatalf("toolUseSummary(%q) = %q, want empty detail", tt.name, got)
			}
			for _, secret := range []string{subject, description, metadata, taskID} {
				if strings.Contains(got, secret) {
					t.Errorf("toolUseSummary(%q) = %q contains sensitive value %q", tt.name, got, secret)
				}
			}
		})
	}
}

// TestTaskSummaryHasNoTodoPresentationCase prevents the removed Todo tool from
// regaining a presentation-specific argument summary.
func TestTaskSummaryHasNoTodoPresentationCase(t *testing.T) {
	t.Parallel()

	if got := toolUseSummary("Todo", json.RawMessage(`{"action":"create"}`)); got != "" {
		t.Fatalf("toolUseSummary(%q) = %q, want no Todo-specific summary", "Todo", got)
	}
}

// TestToolRunSummary pins the collapsed-run label builder: the "N tools · names" text and
// the any-failed flag. A failed tool (ToolError/ToolCancelled) or subagent (subFailed/
// subInterrupted) is marked " ✗" and flips anyFailed; a subagent names its agent as
// "Subagent(agent)".
func TestToolRunSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		calls       []ToolCallView
		want        string
		wantAnyFail bool
	}{
		{
			name: "three ok tools",
			calls: []ToolCallView{
				{ToolName: "Read", Status: ToolOK},
				{ToolName: "Bash", Status: ToolOK},
				{ToolName: "Grep", Status: ToolOK},
			},
			want:        "3 tools · Read, Bash, Grep",
			wantAnyFail: false,
		},
		{
			name: "one failed tool marks and flips",
			calls: []ToolCallView{
				{ToolName: "Read", Status: ToolOK},
				{ToolName: "Bash", Status: ToolError},
				{ToolName: "Grep", Status: ToolOK},
			},
			want:        "3 tools · Read, Bash ✗, Grep",
			wantAnyFail: true,
		},
		{
			name: "cancelled tool marks and flips",
			calls: []ToolCallView{
				{ToolName: "Bash", Status: ToolCancelled},
			},
			want:        "1 tool · Bash ✗",
			wantAnyFail: true,
		},
		{
			name: "done subagent names agent, no mark",
			calls: []ToolCallView{
				{ToolName: "Subagent", Agent: "explore", SubStatus: subDone},
			},
			want:        "1 tool · Subagent(explore)",
			wantAnyFail: false,
		},
		{
			name: "failed subagent marks and flips",
			calls: []ToolCallView{
				{ToolName: "Subagent", Agent: "explore", SubStatus: subFailed},
			},
			want:        "1 tool · Subagent(explore) ✗",
			wantAnyFail: true,
		},
		{
			name: "interrupted subagent marks and flips",
			calls: []ToolCallView{
				{ToolName: "Subagent", Agent: "build", SubStatus: subInterrupted},
			},
			want:        "1 tool · Subagent(build) ✗",
			wantAnyFail: true,
		},
		{
			name:        "single tool",
			calls:       []ToolCallView{{ToolName: "Read", Status: ToolOK}},
			want:        "1 tool · Read",
			wantAnyFail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, anyFail := toolRunSummary(tt.calls)
			if got != tt.want {
				t.Errorf("toolRunSummary() text = %q, want %q", got, tt.want)
			}
			if anyFail != tt.wantAnyFail {
				t.Errorf("toolRunSummary() anyFailed = %v, want %v", anyFail, tt.wantAnyFail)
			}
		})
	}
}

// TestToolCallFailed pins the per-call failure predicate for both tool and subagent cards.
func TestToolCallFailed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call ToolCallView
		want bool
	}{
		{name: "tool ok", call: ToolCallView{ToolName: "Read", Status: ToolOK}, want: false},
		{name: "tool running", call: ToolCallView{ToolName: "Read", Status: ToolRunning}, want: false},
		{name: "tool error", call: ToolCallView{ToolName: "Read", Status: ToolError}, want: true},
		{name: "tool cancelled", call: ToolCallView{ToolName: "Read", Status: ToolCancelled}, want: true},
		{name: "subagent done", call: ToolCallView{ToolName: "Subagent", Agent: "x", SubStatus: subDone}, want: false},
		{name: "subagent running", call: ToolCallView{ToolName: "Subagent", Agent: "x", SubStatus: subRunning}, want: false},
		{name: "subagent failed", call: ToolCallView{ToolName: "Subagent", Agent: "x", SubStatus: subFailed}, want: true},
		{name: "subagent interrupted", call: ToolCallView{ToolName: "Subagent", Agent: "x", SubStatus: subInterrupted}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := toolCallFailed(tt.call); got != tt.want {
				t.Errorf("toolCallFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}
