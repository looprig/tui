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

// TestToolActivitySummaryVocabulary pins the complete built-in presentation vocabulary,
// including irregular plurals. Every statically defined callable tool has a semantic
// activity phrase; dynamic and future tools use the separate unknown fallback tested below.
func TestToolActivitySummaryVocabulary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		singular string
		plural   string
	}{
		{name: "ReadFile", singular: "1 file read", plural: "2 files read"},
		{name: "WriteFile", singular: "1 file written", plural: "2 files written"},
		{name: "EditFile", singular: "1 file edited", plural: "2 files edited"},
		{name: "Glob", singular: "1 file search", plural: "2 file searches"},
		{name: "Grep", singular: "1 content search", plural: "2 content searches"},
		{name: "Bash", singular: "1 command executed", plural: "2 commands executed"},
		{name: "ProcessOutput", singular: "1 process checked", plural: "2 processes checked"},
		{name: "ProcessInput", singular: "1 process input sent", plural: "2 process inputs sent"},
		{name: "ProcessStop", singular: "1 process stopped", plural: "2 processes stopped"},
		{name: "WebSearch", singular: "1 web search", plural: "2 web searches"},
		{name: "Fetch", singular: "1 page fetched", plural: "2 pages fetched"},
		{name: "TaskCreate", singular: "1 task created", plural: "2 tasks created"},
		{name: "TaskUpdate", singular: "1 task updated", plural: "2 tasks updated"},
		{name: "TaskGet", singular: "1 task read", plural: "2 tasks read"},
		{name: "TaskList", singular: "1 task list viewed", plural: "2 task lists viewed"},
		{name: "AskUser", singular: "1 question asked", plural: "2 questions asked"},
		{name: "Skill", singular: "1 skill loaded", plural: "2 skills loaded"},
		{name: "StartAgent", singular: "1 agent started", plural: "2 agents started"},
		{name: "MessageAgent", singular: "1 agent message sent", plural: "2 agent messages sent"},
		{name: "ListAgents", singular: "1 agent check", plural: "2 agent checks"},
		{name: "StopAgent", singular: "1 agent stopped", plural: "2 agents stopped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			one, failed := toolRunSummary([]ToolCallView{{ToolName: tt.name, Status: ToolOK}})
			if one != tt.singular || failed {
				t.Errorf("singular toolRunSummary(%q) = (%q, %v), want (%q, false)", tt.name, one, failed, tt.singular)
			}
			two, failed := toolRunSummary([]ToolCallView{{ToolName: tt.name, Status: ToolOK}, {ToolName: tt.name, Status: ToolOK}})
			if two != tt.plural || failed {
				t.Errorf("plural toolRunSummary(%q) = (%q, %v), want (%q, false)", tt.name, two, failed, tt.plural)
			}
		})
	}
}

// TestToolRunSummary pins semantic grouping, stable first-use ordering, the shared unknown
// fallback, and category-level failure marking. Synthetic subagents retain their richer
// existing label instead of becoming an ordinary activity count.
func TestToolRunSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		calls       []ToolCallView
		want        string
		wantAnyFail bool
	}{
		{
			name: "groups repeated activities in first-use order",
			calls: []ToolCallView{
				{ToolName: "ReadFile", Status: ToolOK},
				{ToolName: "Bash", Status: ToolOK},
				{ToolName: "ReadFile", Status: ToolOK},
			},
			want:        "2 files read, 1 command executed",
			wantAnyFail: false,
		},
		{
			name: "failed call marks its category and flips",
			calls: []ToolCallView{
				{ToolName: "ReadFile", Status: ToolOK},
				{ToolName: "Bash", Status: ToolError},
				{ToolName: "ReadFile", Status: ToolOK},
			},
			want:        "2 files read, 1 command executed ✗",
			wantAnyFail: true,
		},
		{
			name: "cancelled category marks and flips",
			calls: []ToolCallView{
				{ToolName: "Bash", Status: ToolCancelled},
			},
			want:        "1 command executed ✗",
			wantAnyFail: true,
		},
		{
			name: "unknown tools share one fallback category",
			calls: []ToolCallView{
				{ToolName: "mcp__github__search", Status: ToolOK},
				{ToolName: "FutureTool", Status: ToolOK},
			},
			want:        "2 tools used",
			wantAnyFail: false,
		},
		{
			name: "known and unknown categories preserve first use",
			calls: []ToolCallView{
				{ToolName: "FutureTool", Status: ToolOK},
				{ToolName: "EditFile", Status: ToolOK},
				{ToolName: "mcp__github__search", Status: ToolError},
				{ToolName: "WebSearch", Status: ToolOK},
			},
			want:        "2 tools used ✗, 1 file edited, 1 web search",
			wantAnyFail: true,
		},
		{
			name: "internal final output is excluded",
			calls: []ToolCallView{
				{ToolName: "_looprig_final_output", Status: ToolOK},
				{ToolName: "Bash", Status: ToolOK},
			},
			want:        "1 command executed",
			wantAnyFail: false,
		},
		{
			name: "done subagent names agent, no mark",
			calls: []ToolCallView{
				{ToolName: "Subagent", Agent: "explore", SubStatus: subDone},
			},
			want:        "Subagent(explore)",
			wantAnyFail: false,
		},
		{
			name: "failed subagent marks and flips",
			calls: []ToolCallView{
				{ToolName: "Subagent", Agent: "explore", SubStatus: subFailed},
			},
			want:        "Subagent(explore) ✗",
			wantAnyFail: true,
		},
		{
			name: "interrupted subagent marks and flips",
			calls: []ToolCallView{
				{ToolName: "Subagent", Agent: "build", SubStatus: subInterrupted},
			},
			want:        "Subagent(build) ✗",
			wantAnyFail: true,
		},
		{
			name:        "single unknown tool",
			calls:       []ToolCallView{{ToolName: "Read", Status: ToolOK}},
			want:        "1 tool used",
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
