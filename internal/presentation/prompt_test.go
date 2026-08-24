package presentation

import (
	"testing"

	"github.com/looprig/harness/pkg/tool"
)

func TestPromptFromPermissionCarriesTheDiff(t *testing.T) {
	t.Parallel()

	preview := &tool.MutationPreview{
		Path:        "a.go",
		UnifiedDiff: "@@ -1 +1 @@\n-a\n+b\n",
	}

	p := promptFromPermission(callID(1), toolRequest("EditFile", "edit a.go"), preview)

	if p.DiffPath != preview.Path || p.Diff != preview.UnifiedDiff {
		t.Fatalf("preview not carried: %+v", p)
	}
}

func TestPromptFromPermissionWithoutAPreview(t *testing.T) {
	t.Parallel()

	p := promptFromPermission(callID(1), toolRequest("EditFile", "edit a.go"), nil)
	if p.Diff != "" || p.DiffPath != "" || p.DiffCreates {
		t.Fatalf("nil preview produced preview fields: %+v", p)
	}
}

func TestPromptFromPermissionCarriesCreateAndCopiesPreview(t *testing.T) {
	t.Parallel()

	preview := &tool.MutationPreview{
		Path:        "new.go",
		UnifiedDiff: "@@ -0,0 +1 @@\n+package new\n",
		Creates:     true,
	}
	p := promptFromPermission(callID(1), toolRequest("WriteFile", "write new.go"), preview)

	preview.Path = "mutated.go"
	preview.UnifiedDiff = "mutated"
	preview.Creates = false

	if p.DiffPath != "new.go" || p.Diff != "@@ -0,0 +1 @@\n+package new\n" || !p.DiffCreates {
		t.Fatalf("prompt retained or failed to copy preview values: %+v", p)
	}
}

// TestPromptFromPermission covers building the ONE combined permission prompt
// view-model from a typed prepared tool.Request: the ToolName/Summary are copied
// straight off the request, EVERY unmet requirement AND its exact persisted candidates
// project into a single prompt (one prompt, N requirements), Kind is promptPermission,
// and freeText stays false (a permission gate is never free-text).
func TestPromptFromPermission(t *testing.T) {
	t.Parallel()

	// A multi-capability request: two unmet requirements, each with its own
	// display-ready rule candidates. The one prompt must carry them ALL.
	req := toolRequest("Bash", "run the release script",
		requirement("execute /bin/release", "always allow /bin/release"),
		requirement("write /etc/hosts", "always allow writes under /etc", "always allow writes under /etc/hosts"),
	)

	id := callID(7)
	p := promptFromPermission(id, req, nil)

	if p.ToolExecutionID != id {
		t.Errorf("ToolExecutionID = %v, want %v", p.ToolExecutionID, id)
	}
	if p.Kind != promptPermission {
		t.Errorf("Kind = %d, want promptPermission (%d)", p.Kind, promptPermission)
	}
	if p.freeText {
		t.Error("freeText = true, want false for a permission prompt")
	}
	if p.ToolName != "Bash" || p.Summary != "run the release script" {
		t.Errorf("header = (%q, %q), want (Bash, run the release script)", p.ToolName, p.Summary)
	}
	if len(p.Requirements) != 2 {
		t.Fatalf("Requirements len = %d, want 2 (every unmet capability in one prompt)", len(p.Requirements))
	}
	if p.Requirements[0].Description != "execute /bin/release" ||
		len(p.Requirements[0].Candidates) != 1 || p.Requirements[0].Candidates[0] != "always allow /bin/release" {
		t.Errorf("requirement[0] = %+v, want the execute requirement with its one candidate", p.Requirements[0])
	}
	if p.Requirements[1].Description != "write /etc/hosts" || len(p.Requirements[1].Candidates) != 2 {
		t.Errorf("requirement[1] = %+v, want the write requirement with its two candidates", p.Requirements[1])
	}
}

// TestPromptFromPermissionPureToolNoRequirements covers a pure tool that prepares an
// empty request: the prompt still builds (header only), with no requirement rows.
func TestPromptFromPermissionPureToolNoRequirements(t *testing.T) {
	t.Parallel()

	p := promptFromPermission(callID(8), toolRequest("Mystery", "does a thing"), nil)
	if p.Kind != promptPermission || p.ToolName != "Mystery" || p.Summary != "does a thing" {
		t.Errorf("prompt = %+v, want a header-only Mystery permission prompt", p)
	}
	if len(p.Requirements) != 0 {
		t.Errorf("Requirements = %+v, want none for a pure tool", p.Requirements)
	}
}

// TestPromptFromUserInput covers building a user-input prompt view-model: the
// Question/Choices are copied straight off the event, Kind is promptUserInput,
// and freeText is true exactly when there are no choices.
func TestPromptFromUserInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		question     string
		choices      []string
		wantFreeText bool
	}{
		{
			name:         "with choices is a choice prompt",
			question:     "pick one",
			choices:      []string{"a", "b"},
			wantFreeText: false,
		},
		{
			name:         "no choices is free-text",
			question:     "what is your name?",
			choices:      nil,
			wantFreeText: true,
		},
		{
			name:         "empty choices slice is free-text",
			question:     "describe it",
			choices:      []string{},
			wantFreeText: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id := callID(3)
			p := promptFromUserInput(id, tt.question, tt.choices)

			if p.ToolExecutionID != id {
				t.Errorf("ToolExecutionID = %v, want %v", p.ToolExecutionID, id)
			}
			if p.Kind != promptUserInput {
				t.Errorf("Kind = %d, want promptUserInput (%d)", p.Kind, promptUserInput)
			}
			if p.Question != tt.question {
				t.Errorf("Question = %q, want %q", p.Question, tt.question)
			}
			if len(p.Choices) != len(tt.choices) {
				t.Errorf("Choices len = %d, want %d", len(p.Choices), len(tt.choices))
			}
			if p.freeText != tt.wantFreeText {
				t.Errorf("freeText = %v, want %v", p.freeText, tt.wantFreeText)
			}
			if p.selected != 0 {
				t.Errorf("selected = %d, want 0 (head selection)", p.selected)
			}
		})
	}
}
