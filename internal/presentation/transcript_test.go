package presentation

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/looprig/core/content"
	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/identity"
	"github.com/looprig/harness/pkg/tool"
	"github.com/looprig/tui/styles"
)

// callID is defined in screen_test.go (same package): it builds a deterministic,
// non-zero uuid.UUID from a single byte so tests can correlate
// ToolCallStarted/ToolCallCompleted without crypto/rand.

// TestLiveSegThinkDuration covers the thinking-span measurement: no thinking streamed
// (thinkStart zero) is an unmeasured zero (the bare "thought" fallback); a real span is
// end-start (thinkEnd, else the thinkLast fallback); and thinking that WAS measured but
// collapsed to a non-positive span (all chunks in one tick, or clock skew) floors to the
// positive measuredFloor sentinel so it never reads as timeless.
func TestLiveSegThinkDuration(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		start time.Time
		end   time.Time
		last  time.Time
		want  time.Duration
	}{
		{name: "no thinking streamed is an unmeasured zero", want: 0},
		{name: "measured span is end minus start", start: base, end: base.Add(3 * time.Second), want: 3 * time.Second},
		{name: "end unset falls back to the last thinking chunk", start: base, last: base.Add(2 * time.Second), want: 2 * time.Second},
		{name: "same-tick span floors to the sentinel", start: base, end: base, want: measuredFloor},
		{name: "negative span (clock skew) floors to the sentinel", start: base.Add(5 * time.Second), end: base, want: measuredFloor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := liveSeg{thinkStart: tt.start, thinkEnd: tt.end, thinkLast: tt.last}
			if got := s.thinkDuration(); got != tt.want {
				t.Errorf("thinkDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSplitLines covers the tool-result preview splitter (transcript.go).
func TestSplitLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty yields nil", in: "", want: nil},
		{name: "single line", in: "one", want: []string{"one"}},
		{name: "two lines", in: "a\nb", want: []string{"a", "b"}},
		{name: "trailing newline keeps empty tail", in: "a\n", want: []string{"a", ""}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitLines(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("splitLines(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("splitLines(%q)[%d] = %q, want %q", tt.in, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// toolStarted builds a real event.ToolCallStarted for the given call.
func toolStarted(id uuid.UUID, name, summary string) event.Event {
	return event.ToolCallStarted{ToolExecutionID: id, ToolName: name, Summary: summary}
}

// toolCompleted builds a real event.ToolCallCompleted for the given call.
func toolCompleted(id uuid.UUID, isErr bool, preview string) event.Event {
	return event.ToolCallCompleted{ToolExecutionID: id, IsError: isErr, ResultPreview: preview}
}

// textChunk builds a real *content.TextChunk TokenDelta event carrying t.
func textChunk(s string) event.Event {
	return event.TokenDelta{Chunk: &content.TextChunk{Text: s}}
}

func loopHdr(loopID uuid.UUID) event.Header {
	return event.Header{Coordinates: identity.Coordinates{LoopID: loopID}}
}

func workflowActivityEvent(eventID, runID uuid.UUID, kind event.WorkflowActivityKind, status event.WorkflowRunStatus, label string) event.Event {
	return event.WorkflowActivity{
		Header: event.Header{
			Coordinates: identity.Coordinates{SessionID: callID(0xF1)},
			EventID:     eventID,
		},
		RunID:             runID,
		WorkflowName:      "source_document_extract",
		WorkflowVersion:   "v1",
		Kind:              kind,
		Status:            status,
		VertexID:          callID(0xF2),
		VertexLabel:       label,
		CompletedVertices: 1,
		TotalVertices:     2,
		OccurredAt:        time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
	}
}

func TestWorkflowActivityTranscriptEntriesAreGlobalAndUngrouped(t *testing.T) {
	t.Parallel()

	runA, runB := callID(0xA1), callID(0xB1)
	events := []event.Event{
		workflowActivityEvent(callID(0x01), runA, event.WorkflowActivityRunStarted, event.WorkflowRunStatusRunning, "Extract"),
		workflowActivityEvent(callID(0x02), runA, event.WorkflowActivityVertexCompleted, event.WorkflowRunStatusRunning, "Parse"),
		workflowActivityEvent(callID(0x03), runB, event.WorkflowActivityRunStarted, event.WorkflowRunStatusRunning, "Map"),
		workflowActivityEvent(callID(0x04), runA, event.WorkflowActivityRunCompleted, event.WorkflowRunStatusCompleted, "Done"),
		workflowActivityEvent(callID(0x05), runA, event.WorkflowActivityRunResumed, event.WorkflowRunStatusRunning, "Resume"),
	}

	m := transcriptModel{}
	for _, ev := range events {
		m = m.ApplyEvent(ev)
	}

	if len(m.global) != len(events) {
		t.Fatalf("global workflow entries = %d, want %d", len(m.global), len(events))
	}
	if len(m.projections) != 0 {
		t.Fatalf("workflow entries created loop projections: %d", len(m.projections))
	}
	wantContinuation := []bool{false, true, false, true, false}
	for i, entry := range m.global {
		if entry.Kind != kindWorkflowActivity {
			t.Errorf("entry[%d].Kind = %d, want workflow activity", i, entry.Kind)
		}
		if entry.Workflow == nil {
			t.Fatalf("entry[%d].Workflow = nil", i)
		}
		if entry.Workflow.Continuation != wantContinuation[i] {
			t.Errorf("entry[%d].Continuation = %v, want %v", i, entry.Workflow.Continuation, wantContinuation[i])
		}
		text := committedText(entry)
		if strings.Contains(text, entry.Workflow.EventID.String()) || strings.Contains(text, entry.Workflow.RunID.String()) {
			t.Errorf("entry[%d] leaks an identifier: %q", i, text)
		}
		if !strings.Contains(text, "source_document_extract") || !strings.Contains(text, "running") && !strings.Contains(text, "completed") {
			t.Errorf("entry[%d] omits safe workflow metadata: %q", i, text)
		}
	}
}

func TestWorkflowActivityDuplicateEventIDDoesNotAppend(t *testing.T) {
	t.Parallel()

	activity := workflowActivityEvent(callID(0x11), callID(0x21), event.WorkflowActivityRunStarted, event.WorkflowRunStatusRunning, "Extract")
	m := transcriptModel{}.ApplyEvent(activity).ApplyEvent(activity)
	if got := len(m.global); got != 1 {
		t.Fatalf("duplicate workflow activity entries = %d, want 1", got)
	}
}

func TestRenderWorkflowActivityUsesBlueIndependentMarkers(t *testing.T) {
	t.Parallel()

	runID := callID(0x81)
	m := transcriptModel{}
	m = m.ApplyEvent(workflowActivityEvent(callID(0x82), runID, event.WorkflowActivityRunStarted, event.WorkflowRunStatusRunning, "Extract"))
	m = m.ApplyEvent(workflowActivityEvent(callID(0x83), runID, event.WorkflowActivityVertexCompleted, event.WorkflowRunStatusRunning, "Parse"))
	if len(m.global) != 2 {
		t.Fatalf("workflow entries = %d, want 2", len(m.global))
	}

	first, second := renderEntry(m.global[0], false, 120), renderEntry(m.global[1], false, 120)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("rendered workflow rows = (%d, %d), want one row each", len(first), len(second))
	}
	if got := plainFromStyled(first[0]); !strings.HasPrefix(got, "● ") {
		t.Errorf("first workflow row = %q, want solid marker", got)
	}
	if got := plainFromStyled(second[0]); !strings.HasPrefix(got, "| ") {
		t.Errorf("continuation workflow row = %q, want continuation marker", got)
	}
	if !strings.HasPrefix(first[0], styles.WorkflowActivityStyle.Render(styles.WorkflowActivityMarker)+" ") {
		t.Errorf("first marker does not use workflow blue style: %q", first[0])
	}
	if !strings.HasPrefix(second[0], styles.WorkflowActivityStyle.Render(styles.WorkflowActivityContinuationMarker)+" ") {
		t.Errorf("continuation marker does not use workflow blue style: %q", second[0])
	}
	if strings.Contains(plainFromStyled(first[0]), runID.String()) || strings.Contains(plainFromStyled(first[0]), m.global[0].Workflow.EventID.String()) {
		t.Error("workflow row leaked a raw identifier")
	}
}

func findLoop(loops []loopInfo, id uuid.UUID) (loopInfo, bool) {
	for _, li := range loops {
		if li.ID == id {
			return li, true
		}
	}
	return loopInfo{}, false
}

// thinkingChunk builds a real *content.ThinkingChunk TokenDelta event carrying s.
func thinkingChunk(s string) event.Event {
	return event.TokenDelta{Chunk: &content.ThinkingChunk{Thinking: s}}
}

// aiMessage builds an *content.AIMessage from leading thinking, narration text, and
// any tool-use blocks (each by id+name), in that block order. Empty thinking/text
// are omitted so the blocks mirror the materialized AIMessage shape.
func aiMessage(thinking, text string, tools ...content.ToolUseBlock) *content.AIMessage {
	var blocks []content.Block
	if thinking != "" {
		blocks = append(blocks, &content.ThinkingBlock{Thinking: thinking})
	}
	if text != "" {
		blocks = append(blocks, &content.TextBlock{Text: text})
	}
	for i := range tools {
		tb := tools[i]
		blocks = append(blocks, &tb)
	}
	return &content.AIMessage{Message: content.Message{Role: content.RoleAssistant, Blocks: blocks}}
}

// toolUse builds a ToolUseBlock with the given provider id, name, and raw input.
func toolUse(id, name, input string) content.ToolUseBlock {
	return content.ToolUseBlock{ID: id, Name: name, Input: []byte(input)}
}

// toolResult builds a *content.ToolResultMessage answering toolUseID with text.
func toolResult(toolUseID, text string) *content.ToolResultMessage {
	return &content.ToolResultMessage{
		Message:   content.Message{Role: content.RoleTool, Blocks: []content.Block{&content.TextBlock{Text: text}}},
		ToolUseID: toolUseID,
	}
}

// toolResultErr builds a *content.ToolResultMessage answering toolUseID with an
// error result (IsError=true), used to exercise the stepToolCard fallback status.
func toolResultErr(toolUseID, text string) *content.ToolResultMessage {
	r := toolResult(toolUseID, text)
	r.IsError = true
	return r
}

// stepDone builds an event.StepDone carrying the given finalized group.
func stepDone(msgs ...content.Conversation) event.Event {
	return event.StepDone{Messages: content.AgenticMessages(msgs)}
}

// stepDoneFrom builds an event.StepDone stamped with a producing loop id, so a test
// can drive a SUBAGENT loop's collapsed StepDone (a primary StepDone uses the zero/
// active loop id via stepDone above).
func stepDoneFrom(loopID uuid.UUID, msgs ...content.Conversation) event.Event {
	return event.StepDone{
		Header:   event.Header{Coordinates: identity.Coordinates{LoopID: loopID}},
		Messages: content.AgenticMessages(msgs),
	}
}

// loopStarted builds an event.LoopStarted stamped with a loop id and the agent name
// driving it, the source of the transcript's LoopID→agent attribution map.
func loopStarted(loopID uuid.UUID, name identity.AgentName) event.Event {
	return event.LoopStarted{Header: event.Header{
		Coordinates: identity.Coordinates{LoopID: loopID},
		AgentName:   name,
	}}
}

// loopStartedNamed builds an event.LoopStarted carrying BOTH the agent name (on the Header)
// and the loop's user-facing DisplayName, so a test can drive the display-label fold: the
// stored/displayed label is DisplayName when non-empty, else the Header.AgentName (older
// journals carry no DisplayName), else the loopID short form.
func loopStartedNamed(id uuid.UUID, agentName identity.AgentName, displayName string) event.Event {
	return event.LoopStarted{
		Header: event.Header{
			Coordinates: identity.Coordinates{LoopID: id},
			AgentName:   agentName,
		},
		DisplayName: displayName,
	}
}

// TestTranscriptLoopStartedDisplayName proves the LoopStarted display-label fold: DisplayName
// becomes the stored/displayed label, an empty DisplayName falls back to Header.AgentName (older
// journals), and both empty falls back to the loopID short form — while the display metadata
// never changes the loop's identity (the loop table stays keyed by LoopID).
func TestTranscriptLoopStartedDisplayName(t *testing.T) {
	t.Parallel()

	l := callID(0x11)

	tests := []struct {
		name        string
		agentName   identity.AgentName
		displayName string
		wantLabel   string
	}{
		{name: "DisplayName becomes the stored label", agentName: "agent-x", displayName: "Pretty Reviewer", wantLabel: "Pretty Reviewer"},
		{name: "empty DisplayName falls back to AgentName", agentName: "agent-x", displayName: "", wantLabel: "agent-x"},
		{name: "both empty falls back to loopID short form", agentName: "", displayName: "", wantLabel: loopShortForm(l)},
		{name: "DisplayName wins even when AgentName is also set", agentName: "raw-name", displayName: "Nice Name", wantLabel: "Nice Name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := transcriptModel{}
			m = m.ApplyEvent(loopStartedNamed(l, tt.agentName, tt.displayName))

			if got := m.agentLabel(l); got != tt.wantLabel {
				t.Errorf("agentLabel = %q, want %q", got, tt.wantLabel)
			}
			li, ok := findLoop(m.loops(), l)
			if !ok {
				t.Fatalf("loop %v absent from loops()", l)
			}
			if li.Name != tt.wantLabel {
				t.Errorf("loops() Name = %q, want %q", li.Name, tt.wantLabel)
			}
			// Display metadata never changes the loop's identity: the table stays keyed by LoopID.
			if li.ID != l {
				t.Errorf("loops() ID = %v, want %v (a display label must not change loop identity)", li.ID, l)
			}
		})
	}
}

// blockText returns the Text of b if it is a *content.TextBlock, else "".
func blockText(b content.Block) string {
	if tb, ok := b.(*content.TextBlock); ok {
		return tb.Text
	}
	return ""
}

// blockThinking returns the Thinking of b if it is a *content.ThinkingBlock, else "".
func blockThinking(b content.Block) string {
	if th, ok := b.(*content.ThinkingBlock); ok {
		return th.Thinking
	}
	return ""
}

func TestTranscriptApplyEvent(t *testing.T) {
	tests := []struct {
		name   string
		events []event.Event
		want   func(t *testing.T, m transcriptModel)
	}{
		{
			name: "text chunk accumulates into live",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("ab"),
				textChunk("cd"),
			},
			want: func(t *testing.T, m transcriptModel) {
				if got := m.testLive().Text; got != "abcd" {
					t.Errorf("live.Text = %q, want %q", got, "abcd")
				}
				if m.testLive().Thinking != "" {
					t.Errorf("live.Thinking = %q, want empty", m.testLive().Thinking)
				}
				if len(m.testCommitted()) != 0 {
					t.Errorf("committed = %d entries, want 0", len(m.testCommitted()))
				}
			},
		},
		{
			name: "thinking chunk accumulates into live",
			events: []event.Event{
				event.TurnStarted{},
				thinkingChunk("rea"),
				thinkingChunk("soning"),
			},
			want: func(t *testing.T, m transcriptModel) {
				if got := m.testLive().Thinking; got != "reasoning" {
					t.Errorf("live.Thinking = %q, want %q", got, "reasoning")
				}
				if m.testLive().Text != "" {
					t.Errorf("live.Text = %q, want empty", m.testLive().Text)
				}
				if len(m.testCommitted()) != 0 {
					t.Errorf("committed = %d entries, want 0", len(m.testCommitted()))
				}
			},
		},
		{
			name: "TurnDone commits live to one entry with stable ID",
			events: []event.Event{
				event.TurnStarted{},
				thinkingChunk("because reasons"),
				textChunk("the answer"),
				event.TurnDone{},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d entries, want exactly 1", len(m.testCommitted()))
				}
				e := m.testCommitted()[0]
				if e.ID == 0 {
					t.Errorf("entry ID = 0, want nonzero stable ID")
				}
				if e.Kind != kindAssistant {
					t.Errorf("entry Kind = %v, want kindAssistant", e.Kind)
				}
				// blocks: leading ThinkingBlock, then TextBlock (mirrors commitLive).
				if len(e.Blocks) != 2 {
					t.Fatalf("entry Blocks = %d, want 2 (thinking + text)", len(e.Blocks))
				}
				if got := blockThinking(e.Blocks[0]); got != "because reasons" {
					t.Errorf("Blocks[0] thinking = %q, want %q", got, "because reasons")
				}
				if got := blockText(e.Blocks[1]); got != "the answer" {
					t.Errorf("Blocks[1] text = %q, want %q", got, "the answer")
				}
				// live reset after commit.
				if !m.testLive().empty() {
					t.Errorf("live not reset after TurnDone: %+v", m.testLive())
				}
			},
		},
		{
			name: "empty live is not committed on TurnDone",
			events: []event.Event{
				event.TurnStarted{},
				event.TurnDone{},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 0 {
					t.Errorf("committed = %d entries, want 0 (empty live must not commit)", len(m.testCommitted()))
				}
			},
		},
		{
			name: "TurnDone with an unresolved running call commits it (not dropped) preserving status, and resets live",
			events: []event.Event{
				event.TurnStarted{},
				toolStarted(callID(1), "Bash", "sleep 9"),
				event.TurnDone{},
			},
			want: func(t *testing.T, m transcriptModel) {
				// The leftover running call must be flushed, not silently dropped.
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (leftover call flushed, not dropped)", len(m.testCommitted()))
				}
				e := m.testCommitted()[0]
				if e.Kind != kindTool {
					t.Fatalf("committed[0].Kind = %v, want kindTool", e.Kind)
				}
				if e.ID == 0 {
					t.Errorf("entry ID = 0, want nonzero stable ID")
				}
				if len(e.Calls) != 1 {
					t.Fatalf("entry Calls = %d, want 1", len(e.Calls))
				}
				c := e.Calls[0]
				if c.ToolExecutionID != callID(1) {
					t.Errorf("ToolExecutionID = %v, want %v", c.ToolExecutionID, callID(1))
				}
				// TurnDone is a normal completion: status preserved (NOT forced cancelled).
				if c.Status != ToolRunning {
					t.Errorf("Status = %v, want ToolRunning (TurnDone preserves status, does not cancel)", c.Status)
				}
				// live fully reset.
				if !m.testLive().empty() || m.testLive().active {
					t.Errorf("live not reset after TurnDone: %+v", m.testLive())
				}
				if len(m.testLive().Calls) != 0 {
					t.Errorf("live.Calls = %d, want 0 after TurnDone", len(m.testLive().Calls))
				}
			},
		},
		{
			name: "TurnDone after a completed call commits exactly one kindTool entry (no double-commit)",
			events: []event.Event{
				event.TurnStarted{},
				toolStarted(callID(1), "Bash", "ls"),
				toolCompleted(callID(1), false, "out"),
				event.TurnDone{},
			},
			want: func(t *testing.T, m transcriptModel) {
				// Defensive commitLive path (no StepDone for this step): the resolved
				// live card is flushed exactly once by TurnDone — never duplicated.
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want exactly 1 (flushed once, no duplication)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindTool {
					t.Errorf("committed[0].Kind = %v, want kindTool", m.testCommitted()[0].Kind)
				}
				if m.testCommitted()[0].Calls[0].Status != ToolOK {
					t.Errorf("Status = %v, want ToolOK", m.testCommitted()[0].Calls[0].Status)
				}
				if !m.testLive().empty() {
					t.Errorf("live not reset after TurnDone: %+v", m.testLive())
				}
			},
		},
		{
			name: "two turns produce distinct stable IDs",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("first"),
				event.TurnDone{},
				event.TurnStarted{},
				textChunk("second"),
				event.TurnDone{},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d entries, want 2", len(m.testCommitted()))
				}
				if m.testCommitted()[0].ID == m.testCommitted()[1].ID {
					t.Errorf("entry IDs not distinct: both %d", m.testCommitted()[0].ID)
				}
				if m.testCommitted()[0].ID == 0 || m.testCommitted()[1].ID == 0 {
					t.Errorf("entry IDs must be nonzero: %d, %d", m.testCommitted()[0].ID, m.testCommitted()[1].ID)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			tt.want(t, m)
		})
	}
}

// TestTranscriptLiveActive locks the live.active contract: TurnStarted marks the
// segment active, and committing a non-empty live segment on TurnDone resets live
// to the zero liveSeg{} (active false again). The empty-live case is included to
// show TurnDone still clears active even when nothing commits.
func TestTranscriptLiveActive(t *testing.T) {
	tests := []struct {
		name       string
		events     []event.Event
		wantActive bool
	}{
		{
			name:       "TurnStarted marks live active",
			events:     []event.Event{event.TurnStarted{}},
			wantActive: true,
		},
		{
			name: "active stays true while streaming chunks",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("partial"),
			},
			wantActive: true,
		},
		{
			name: "TurnDone committing non-empty live resets active to false",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("the answer"),
				event.TurnDone{},
			},
			wantActive: false,
		},
		{
			name: "TurnDone on empty live resets active to false",
			events: []event.Event{
				event.TurnStarted{},
				event.TurnDone{},
			},
			wantActive: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			if m.testLive().active != tt.wantActive {
				t.Errorf("live.active = %v, want %v", m.testLive().active, tt.wantActive)
			}
		})
	}
}

// TestTranscriptToolCalls covers the tool-call state machine: a started call adds
// a running card to live.Calls; a completed call resolves into exactly ONE
// committed kindTool entry at terminal state and leaves live.Calls without it.
func TestTranscriptToolCalls(t *testing.T) {
	tests := []struct {
		name   string
		events []event.Event
		want   func(t *testing.T, m transcriptModel)
	}{
		{
			name: "ToolCallStarted adds a running card to live.Calls",
			events: []event.Event{
				event.TurnStarted{},
				toolStarted(callID(1), "Bash", "ls -la"),
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testLive().Calls) != 1 {
					t.Fatalf("live.Calls = %d, want 1", len(m.testLive().Calls))
				}
				c := m.testLive().Calls[0]
				if c.ToolExecutionID != callID(1) {
					t.Errorf("ToolExecutionID = %v, want %v", c.ToolExecutionID, callID(1))
				}
				if c.ToolName != "Bash" {
					t.Errorf("ToolName = %q, want %q", c.ToolName, "Bash")
				}
				if c.Summary != "ls -la" {
					t.Errorf("Summary = %q, want %q", c.Summary, "ls -la")
				}
				if c.Status != ToolRunning {
					t.Errorf("Status = %v, want ToolRunning", c.Status)
				}
				if len(m.testCommitted()) != 0 {
					t.Errorf("committed = %d, want 0 (no terminal yet)", len(m.testCommitted()))
				}
			},
		},
		{
			name: "ToolCallCompleted (ok) resolves the live card IN PLACE (no commit until StepDone)",
			events: []event.Event{
				event.TurnStarted{},
				toolStarted(callID(1), "Bash", "ls"),
				toolCompleted(callID(1), false, "file1\nfile2"),
			},
			want: func(t *testing.T, m transcriptModel) {
				// The card stays in the live tail, resolved; nothing is committed yet —
				// committing is the step boundary's job (StepDone), not the event's.
				if len(m.testCommitted()) != 0 {
					t.Fatalf("committed = %d, want 0 (no StepDone/terminal yet)", len(m.testCommitted()))
				}
				if len(m.testLive().Calls) != 1 {
					t.Fatalf("live.Calls = %d, want 1 (resolved in place, not removed)", len(m.testLive().Calls))
				}
				c := m.testLive().Calls[0]
				if c.Status != ToolOK {
					t.Errorf("Status = %v, want ToolOK", c.Status)
				}
				if len(c.Result) != 2 || c.Result[0] != "file1" || c.Result[1] != "file2" {
					t.Errorf("Result = %#v, want [file1 file2]", c.Result)
				}
			},
		},
		{
			name: "ToolCallCompleted (error) resolves the live card to ToolError in place",
			events: []event.Event{
				event.TurnStarted{},
				toolStarted(callID(2), "Fetch", "GET /x"),
				toolCompleted(callID(2), true, "boom"),
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 0 {
					t.Fatalf("committed = %d, want 0 (no StepDone/terminal yet)", len(m.testCommitted()))
				}
				if len(m.testLive().Calls) != 1 {
					t.Fatalf("live.Calls = %d, want 1", len(m.testLive().Calls))
				}
				c := m.testLive().Calls[0]
				if c.Status != ToolError {
					t.Errorf("Status = %v, want ToolError", c.Status)
				}
				if len(c.Result) != 1 || c.Result[0] != "boom" {
					t.Errorf("Result = %#v, want [boom]", c.Result)
				}
			},
		},
		{
			name: "StepDone commits the resolved live cards' redacted Summary/preview by position",
			events: []event.Event{
				event.TurnStarted{},
				toolStarted(callID(1), "Bash", "a"),
				toolCompleted(callID(1), false, "out1"),
				toolStarted(callID(2), "Bash", "b"),
				toolCompleted(callID(2), false, "out2"),
				// The finalized step group: two tool-use blocks, in the same order.
				stepDone(
					aiMessage("", "", toolUse("tu-1", "Bash", `{}`), toolUse("tu-2", "Bash", `{}`)),
					toolResult("tu-1", "out1"),
					toolResult("tu-2", "out2"),
				),
			},
			want: func(t *testing.T, m transcriptModel) {
				// empty-text step: NO assistant entry — just the two stand-alone tool nodes.
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d, want 2 (tool, tool)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindTool || m.testCommitted()[1].Kind != kindTool {
					t.Errorf("kinds = %v,%v, want kindTool both", m.testCommitted()[0].Kind, m.testCommitted()[1].Kind)
				}
				if m.testCommitted()[0].ID == m.testCommitted()[1].ID {
					t.Errorf("tool entry IDs not distinct: both %d", m.testCommitted()[0].ID)
				}
				// The committed cards reuse the LIVE cards' redacted Summary by position.
				if got := m.testCommitted()[0].Calls[0].Summary; got != "a" {
					t.Errorf("tool[0] Summary = %q, want the redacted live summary %q", got, "a")
				}
				if got := m.testCommitted()[1].Calls[0].Summary; got != "b" {
					t.Errorf("tool[1] Summary = %q, want the redacted live summary %q", got, "b")
				}
				if !m.testLive().empty() {
					t.Errorf("live not reset after StepDone: %+v", m.testLive())
				}
			},
		},
		{
			name: "StepDone fallback (no live card) reads ToolResultMessage.IsError → ToolError",
			events: []event.Event{
				event.TurnStarted{},
				// No toolStarted/toolCompleted: live.Calls is empty, so stepToolCard
				// takes the fallback branch keyed on the stored ToolResultMessage.
				stepDone(
					aiMessage("", "", toolUse("tu-err", "Bash", `{}`)),
					toolResultErr("tu-err", "tool error: boom"),
				),
			},
			want: func(t *testing.T, m transcriptModel) {
				// single empty-text tool → one stand-alone tool node, no assistant entry.
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (tool node)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindTool {
					t.Errorf("committed[0] = %+v, want a kindTool node", m.testCommitted()[0])
				}
				if len(m.testCommitted()[0].Calls) != 1 {
					t.Fatalf("committed[0].Calls = %d, want 1", len(m.testCommitted()[0].Calls))
				}
				if got := m.testCommitted()[0].Calls[0].Status; got != ToolError {
					t.Errorf("fallback card Status = %v, want ToolError (from IsError)", got)
				}
			},
		},
		{
			name: "StepDone fallback (no live card) with IsError false → ToolOK",
			events: []event.Event{
				event.TurnStarted{},
				stepDone(
					aiMessage("", "", toolUse("tu-ok", "Bash", `{}`)),
					toolResult("tu-ok", "all good"),
				),
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (tool node)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindTool {
					t.Errorf("committed[0] = %+v, want a kindTool node", m.testCommitted()[0])
				}
				if len(m.testCommitted()[0].Calls) != 1 {
					t.Fatalf("committed[0].Calls = %d, want 1", len(m.testCommitted()[0].Calls))
				}
				if got := m.testCommitted()[0].Calls[0].Status; got != ToolOK {
					t.Errorf("fallback card Status = %v, want ToolOK", got)
				}
			},
		},
		{
			name: "unknown completed ToolExecutionID is a no-op (no commit, no panic)",
			events: []event.Event{
				event.TurnStarted{},
				toolCompleted(callID(9), false, "orphan"),
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 0 {
					t.Errorf("committed = %d, want 0 (unknown ToolExecutionID is a no-op)", len(m.testCommitted()))
				}
				if len(m.testLive().Calls) != 0 {
					t.Errorf("live.Calls = %d, want 0", len(m.testLive().Calls))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			tt.want(t, m)
		})
	}
}

// TestTranscriptOrdering locks the per-step append-only ordering rule under the
// StepDone-group model: within a step the AIMessage prose commits as its own
// assistant entry AHEAD of that step's tool card, and a SECOND step's prose lands in a
// later assistant entry — yielding the natural reading order prose1 → tool card →
// prose2 across two StepDone groups (the OLD single-turn interleave maps onto two
// steps now).
func TestTranscriptOrdering(t *testing.T) {
	t.Parallel()
	var m transcriptModel
	for _, ev := range []event.Event{
		event.TurnStarted{},
		// Step 1: prose then a tool use, finalized.
		textChunk("before tool"),
		toolStarted(callID(1), "Bash", "run"),
		toolCompleted(callID(1), false, "done"),
		stepDone(
			aiMessage("", "before tool", toolUse("tu-1", "Bash", `{}`)),
			toolResult("tu-1", "done"),
		),
		// Step 2: the trailing prose, finalized.
		textChunk("after tool"),
		stepDone(aiMessage("", "after tool")),
		event.TurnDone{},
	} {
		m = m.ApplyEvent(ev)
	}
	if len(m.testCommitted()) != 3 {
		t.Fatalf("committed = %d, want 3 (prose1, tool, prose2)", len(m.testCommitted()))
	}
	// [0] step-1 assistant prose committed BEFORE the tool card.
	if m.testCommitted()[0].Kind != kindAssistant {
		t.Fatalf("committed[0].Kind = %v, want kindAssistant", m.testCommitted()[0].Kind)
	}
	if got := blockText(m.testCommitted()[0].Blocks[0]); got != "before tool" {
		t.Errorf("committed[0] text = %q, want %q", got, "before tool")
	}
	// [1] the tool card, AFTER prose1.
	if m.testCommitted()[1].Kind != kindTool {
		t.Errorf("committed[1].Kind = %v, want kindTool", m.testCommitted()[1].Kind)
	}
	// [2] step-2 prose, AFTER the tool card.
	if m.testCommitted()[2].Kind != kindAssistant {
		t.Fatalf("committed[2].Kind = %v, want kindAssistant", m.testCommitted()[2].Kind)
	}
	if got := blockText(m.testCommitted()[2].Blocks[0]); got != "after tool" {
		t.Errorf("committed[2] text = %q, want %q", got, "after tool")
	}
	// IDs strictly increasing in commit order.
	if !(m.testCommitted()[0].ID < m.testCommitted()[1].ID && m.testCommitted()[1].ID < m.testCommitted()[2].ID) {
		t.Errorf("IDs not strictly increasing: %d,%d,%d", m.testCommitted()[0].ID, m.testCommitted()[1].ID, m.testCommitted()[2].ID)
	}
}

// TestTranscriptTerminals covers the TurnInterrupted and TurnFailed terminals:
// remaining live prose/thinking is committed, any still-running call is cancelled
// and committed, the appropriate tombstone/error entry is appended, and live is
// reset.
func TestTranscriptTerminals(t *testing.T) {
	tests := []struct {
		name   string
		events []event.Event
		want   func(t *testing.T, m transcriptModel)
	}{
		{
			name: "TurnInterrupted commits prose, cancels running call, appends tombstone, resets live",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("partial answer"),
				toolStarted(callID(1), "Bash", "sleep"),
				event.TurnInterrupted{},
			},
			want: func(t *testing.T, m transcriptModel) {
				// prose committed, cancelled tool committed, interrupted tombstone.
				if len(m.testCommitted()) != 3 {
					t.Fatalf("committed = %d, want 3 (prose, cancelled tool, tombstone)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindAssistant {
					t.Errorf("committed[0].Kind = %v, want kindAssistant", m.testCommitted()[0].Kind)
				}
				if m.testCommitted()[1].Kind != kindTool {
					t.Errorf("committed[1].Kind = %v, want kindTool", m.testCommitted()[1].Kind)
				}
				if got := m.testCommitted()[1].Calls[0].Status; got != ToolCancelled {
					t.Errorf("running call status = %v, want ToolCancelled", got)
				}
				if m.testCommitted()[2].Kind != kindInterrupted {
					t.Errorf("committed[2].Kind = %v, want kindInterrupted", m.testCommitted()[2].Kind)
				}
				if !m.testLive().empty() || m.testLive().active {
					t.Errorf("live not reset after interrupt: %+v", m.testLive())
				}
			},
		},
		{
			name: "TurnInterrupted with no live content appends only the tombstone",
			events: []event.Event{
				event.TurnStarted{},
				event.TurnInterrupted{},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (tombstone only)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindInterrupted {
					t.Errorf("Kind = %v, want kindInterrupted", m.testCommitted()[0].Kind)
				}
			},
		},
		{
			name: "TurnFailed commits prose then appends an error-level notice carrying the message",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("partial"),
				event.TurnFailed{Err: errBoom{}},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d, want 2 (prose, error)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindAssistant {
					t.Errorf("committed[0].Kind = %v, want kindAssistant", m.testCommitted()[0].Kind)
				}
				if m.testCommitted()[1].Kind != kindNotice || m.testCommitted()[1].Level != noticeError {
					t.Fatalf("committed[1] = (kind %v, level %d), want (kindNotice, noticeError)", m.testCommitted()[1].Kind, m.testCommitted()[1].Level)
				}
				if got := blockText(m.testCommitted()[1].Blocks[0]); got != "boom" {
					t.Errorf("error text = %q, want %q", got, "boom")
				}
				if !m.testLive().empty() || m.testLive().active {
					t.Errorf("live not reset after failure: %+v", m.testLive())
				}
			},
		},
		{
			name: "TurnFailed with nil Err still appends an error-level notice and resets live",
			events: []event.Event{
				event.TurnStarted{},
				event.TurnFailed{Err: nil},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (error only)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindNotice || m.testCommitted()[0].Level != noticeError {
					t.Errorf("committed[0] = (kind %v, level %d), want (kindNotice, noticeError)", m.testCommitted()[0].Kind, m.testCommitted()[0].Level)
				}
				if !m.testLive().empty() {
					t.Errorf("live not reset after failure: %+v", m.testLive())
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			tt.want(t, m)
		})
	}
}

// TestTranscriptStepDoneSelfHeal locks the StepDone-group rendering + self-heal
// contract (Phase 11.2): provisional live prose accumulated from TokenDeltas is
// REPLACED by the finalized StepDone.Messages on commit (a dropped/partial delta
// does not survive past StepDone), the committed entries are built from the stored
// AIMessage (+ its ToolResultMessages), and the live segment is reset to empty.
func TestTranscriptStepDoneSelfHeal(t *testing.T) {
	tests := []struct {
		name   string
		events []event.Event
		want   func(t *testing.T, m transcriptModel)
	}{
		{
			name: "no-tool step: provisional text replaced by finalized AIMessage prose",
			events: []event.Event{
				event.TurnStarted{},
				// Provisional/partial deltas: a torn stream that dropped the tail.
				thinkingChunk("because rea"),
				textChunk("the ans"),
				// The authoritative finalized group: full thinking + full text.
				stepDone(aiMessage("because reasons", "the answer")),
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want exactly 1 (the finalized AIMessage)", len(m.testCommitted()))
				}
				e := m.testCommitted()[0]
				if e.Kind != kindAssistant {
					t.Fatalf("committed[0].Kind = %v, want kindAssistant", e.Kind)
				}
				if e.ID == 0 {
					t.Errorf("entry ID = 0, want nonzero stable ID")
				}
				// SNAP: the finalized message, NOT the partial provisional text.
				if got := thinkingText(e.Blocks); got != "because reasons" {
					t.Errorf("committed thinking = %q, want finalized %q (self-heal)", got, "because reasons")
				}
				if got := assistantText(e.Blocks); got != "the answer" {
					t.Errorf("committed text = %q, want finalized %q (self-heal)", got, "the answer")
				}
				// the provisional live segment is gone: dropped deltas do not survive.
				if !m.testLive().empty() {
					t.Errorf("live not reset after StepDone: %+v", m.testLive())
				}
			},
		},
		{
			name: "provisional text that OVER-ran the finalized message is discarded on snap",
			events: []event.Event{
				event.TurnStarted{},
				// A stale/duplicated provisional render: longer than the truth.
				textChunk("the answer is forty-two and then some garbage"),
				stepDone(aiMessage("", "the answer is forty-two")),
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1", len(m.testCommitted()))
				}
				if got := assistantText(m.testCommitted()[0].Blocks); got != "the answer is forty-two" {
					t.Errorf("committed text = %q, want the finalized %q (provisional discarded)", got, "the answer is forty-two")
				}
			},
		},
		{
			name: "tool-using step: AIMessage prose entry then a separate tool entry carrying the result",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("let me check"),
				stepDone(
					aiMessage("", "let me check", toolUse("tu-1", "Grep", `{"q":"x"}`)),
					toolResult("tu-1", "match\nanother"),
				),
			},
			want: func(t *testing.T, m transcriptModel) {
				// SEPARATE entries: the assistant prose, then the tool card. NOT merged.
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d, want 2 (assistant prose, tool)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindAssistant {
					t.Errorf("committed[0].Kind = %v, want kindAssistant", m.testCommitted()[0].Kind)
				}
				if got := assistantText(m.testCommitted()[0].Blocks); got != "let me check" {
					t.Errorf("assistant text = %q, want %q", got, "let me check")
				}
				tool := m.testCommitted()[1]
				if tool.Kind != kindTool {
					t.Fatalf("committed[1].Kind = %v, want kindTool", tool.Kind)
				}
				if len(tool.Calls) != 1 {
					t.Fatalf("tool entry Calls = %d, want 1", len(tool.Calls))
				}
				c := tool.Calls[0]
				if c.ToolName != "Grep" {
					t.Errorf("tool name = %q, want %q", c.ToolName, "Grep")
				}
				if len(c.Result) != 2 || c.Result[0] != "match" || c.Result[1] != "another" {
					t.Errorf("tool result = %#v, want [match another] (from the stored ToolResultMessage)", c.Result)
				}
				// IDs strictly increasing in commit order.
				if !(m.testCommitted()[0].ID < m.testCommitted()[1].ID) {
					t.Errorf("IDs not increasing: %d, %d", m.testCommitted()[0].ID, m.testCommitted()[1].ID)
				}
				if !m.testLive().empty() {
					t.Errorf("live not reset after StepDone: %+v", m.testLive())
				}
			},
		},
		{
			name: "tool-use-only step (single tool, no narration) commits a stand-alone tool node",
			events: []event.Event{
				event.TurnStarted{},
				stepDone(
					aiMessage("", "", toolUse("tu-9", "ReadFile", `{"path":"a"}`)),
					toolResult("tu-9", "contents"),
				),
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (stand-alone tool node, no umbrella)", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindTool {
					t.Errorf("committed[0] = %+v, want a kindTool node", m.testCommitted()[0])
				}
				if m.testCommitted()[0].Calls[0].ToolName != "ReadFile" {
					t.Errorf("tool name = %q, want ReadFile", m.testCommitted()[0].Calls[0].ToolName)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			tt.want(t, m)
		})
	}
}

// TestTranscriptMultiStepSeparateEntries locks that a multi-step (tool-using) turn
// renders as MULTIPLE separate assistant + tool entries, in order — never collapsed
// into one merged entry. Two StepDone groups (a tool step then a final no-tool
// answer) followed by the lifecycle TurnDone must yield: assistant, tool, assistant.
func TestTranscriptMultiStepSeparateEntries(t *testing.T) {
	t.Parallel()
	var m transcriptModel
	for _, ev := range []event.Event{
		event.TurnStarted{},
		// Step 1: assistant asks for a tool; its result comes back.
		textChunk("checking"),
		stepDone(
			aiMessage("", "checking", toolUse("tu-1", "Bash", `{"cmd":"ls"}`)),
			toolResult("tu-1", "file1\nfile2"),
		),
		// Step 2: the final no-tool answer.
		textChunk("all done"),
		stepDone(aiMessage("", "all done")),
		// Lifecycle terminal: no new content (every step already committed via StepDone).
		event.TurnDone{},
	} {
		m = m.ApplyEvent(ev)
	}

	if len(m.testCommitted()) != 3 {
		t.Fatalf("committed = %d, want 3 (step1 assistant, step1 tool, step2 assistant) — NOT merged", len(m.testCommitted()))
	}
	wantKinds := []entryKind{kindAssistant, kindTool, kindAssistant}
	for i, want := range wantKinds {
		if m.testCommitted()[i].Kind != want {
			t.Errorf("committed[%d].Kind = %v, want %v", i, m.testCommitted()[i].Kind, want)
		}
	}
	if got := assistantText(m.testCommitted()[0].Blocks); got != "checking" {
		t.Errorf("step1 assistant text = %q, want %q", got, "checking")
	}
	if c := m.testCommitted()[1].Calls[0]; c.ToolName != "Bash" {
		t.Errorf("step1 tool name = %q, want Bash", c.ToolName)
	}
	if got := assistantText(m.testCommitted()[2].Blocks); got != "all done" {
		t.Errorf("step2 assistant text = %q, want %q", got, "all done")
	}
	// IDs strictly increasing in commit order across both steps.
	if !(m.testCommitted()[0].ID < m.testCommitted()[1].ID && m.testCommitted()[1].ID < m.testCommitted()[2].ID) {
		t.Errorf("IDs not strictly increasing: %d,%d,%d", m.testCommitted()[0].ID, m.testCommitted()[1].ID, m.testCommitted()[2].ID)
	}
	if !m.testLive().empty() || m.testLive().active {
		t.Errorf("live not reset after the turn: %+v", m.testLive())
	}
}

// errBoom is a typed test error whose message is "boom".
type errBoom struct{}

func (errBoom) Error() string { return "boom" }

// TestCommitUser locks the user-message commit: CommitUser appends exactly one
// kindUser entry carrying the submitted blocks with a fresh, nonzero, stable ID;
// a second CommitUser allocates a distinct ID; and empty blocks still commit one
// user entry (the submit path validates emptiness upstream, not here).
func TestCommitUser(t *testing.T) {
	tests := []struct {
		name   string
		blocks [][]content.Block
		want   func(t *testing.T, m transcriptModel)
	}{
		{
			name:   "single user message commits one entry with a nonzero ID",
			blocks: [][]content.Block{{&content.TextBlock{Text: "hello there"}}},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1", len(m.testCommitted()))
				}
				e := m.testCommitted()[0]
				if e.Kind != kindUser {
					t.Errorf("Kind = %v, want kindUser", e.Kind)
				}
				if e.ID == 0 {
					t.Errorf("entry ID = 0, want nonzero stable ID")
				}
				if len(e.Blocks) != 1 || blockText(e.Blocks[0]) != "hello there" {
					t.Errorf("Blocks = %#v, want one TextBlock %q", e.Blocks, "hello there")
				}
			},
		},
		{
			name: "two user messages get distinct stable IDs",
			blocks: [][]content.Block{
				{&content.TextBlock{Text: "first"}},
				{&content.TextBlock{Text: "second"}},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d, want 2", len(m.testCommitted()))
				}
				if m.testCommitted()[0].ID == m.testCommitted()[1].ID {
					t.Errorf("user entry IDs not distinct: both %d", m.testCommitted()[0].ID)
				}
				if m.testCommitted()[0].ID == 0 || m.testCommitted()[1].ID == 0 {
					t.Errorf("user entry IDs must be nonzero: %d, %d", m.testCommitted()[0].ID, m.testCommitted()[1].ID)
				}
			},
		},
		{
			name:   "empty blocks still commit a single user entry",
			blocks: [][]content.Block{{}},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1", len(m.testCommitted()))
				}
				if m.testCommitted()[0].Kind != kindUser {
					t.Errorf("Kind = %v, want kindUser", m.testCommitted()[0].Kind)
				}
				if m.testCommitted()[0].ID == 0 {
					t.Errorf("entry ID = 0, want nonzero stable ID")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, b := range tt.blocks {
				m = m.CommitUser(b)
			}
			tt.want(t, m)
		})
	}
}

// TestCommitUserDoesNotDisturbLive locks that committing a user message neither
// reads nor resets the in-progress live segment: a user message can be queued
// mid-turn (Running) without truncating the streaming assistant output.
func TestCommitUserDoesNotDisturbLive(t *testing.T) {
	t.Parallel()
	var m transcriptModel
	m = m.ApplyEvent(event.TurnStarted{})
	m = m.ApplyEvent(textChunk("streaming so far"))
	m = m.CommitUser([]content.Block{&content.TextBlock{Text: "queued msg"}})
	if m.testLive().Text != "streaming so far" {
		t.Errorf("live.Text = %q, want preserved %q", m.testLive().Text, "streaming so far")
	}
	if !m.testLive().active {
		t.Errorf("live.active = false, want true (CommitUser must not end the turn)")
	}
	if len(m.testCommitted()) != 1 || m.testCommitted()[0].Kind != kindUser {
		t.Fatalf("committed = %#v, want one kindUser entry", m.testCommitted())
	}
}

// TestTranscriptGatePrompts covers the gate-open boundaries in ApplyEvent after the
// §7 rework: a PermissionRequested commits NOTHING (the gate surfaces as a live
// awaiting-approval card composed by Screen, never a committed record), and a
// UserInputRequested commits ONLY its AskUser record. Neither commits the provisional
// live prose — it stays in the live segment to be committed exactly once by the step's
// StepDone (the duplicate-thinking fix). The live segment is NOT reset by either gate.
func TestTranscriptGatePrompts(t *testing.T) {
	tests := []struct {
		name   string
		events []event.Event
		want   func(t *testing.T, m transcriptModel)
	}{
		{
			name: "PermissionRequested commits nothing and leaves provisional prose live",
			events: []event.Event{
				event.TurnStarted{},
				thinkingChunk("planning the command"),
				textChunk("I'll run a command."),
				event.PermissionRequested{
					ToolExecutionID: callID(1),
					Request:         bashPermission("rm -rf build"),
				},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 0 {
					t.Fatalf("committed = %d, want 0 (the gate must not commit)", len(m.testCommitted()))
				}
				if m.testLive().Thinking != "planning the command" {
					t.Errorf("live.Thinking = %q, want the provisional thinking preserved (uncommitted)", m.testLive().Thinking)
				}
				if m.testLive().Text != "I'll run a command." {
					t.Errorf("live.Text = %q, want the provisional prose preserved (uncommitted)", m.testLive().Text)
				}
				if !m.testLive().active {
					t.Errorf("live.active = false, want true (the gate does not end the turn)")
				}
			},
		},
		{
			name: "UserInputRequested commits ONLY the record; provisional prose stays live",
			events: []event.Event{
				event.TurnStarted{},
				textChunk("Need a decision."),
				event.UserInputRequested{
					ToolExecutionID: callID(4),
					Question:        "Which source?",
					Choices:         []string{"alpha", "beta", "gamma"},
				},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (the AskUser record only — no prose commit)", len(m.testCommitted()))
				}
				if m.testLive().Text != "Need a decision." {
					t.Errorf("live.Text = %q, want the provisional prose preserved (uncommitted)", m.testLive().Text)
				}
				rec := m.testCommitted()[0]
				if rec.Kind != kindPromptRecord || rec.Prompt == nil {
					t.Fatalf("committed[0] = %+v, want a kindPromptRecord with Prompt context", rec)
				}
				if rec.Prompt.Question != "Which source?" {
					t.Errorf("Prompt.Question = %q, want %q", rec.Prompt.Question, "Which source?")
				}
				if len(rec.Prompt.Choices) != 3 {
					t.Fatalf("Prompt.Choices = %d, want 3", len(rec.Prompt.Choices))
				}
				// every choice must survive into the rendered scrollback record.
				out := stripANSI(strings.Join(renderEntry(rec, false, 80), "\n"))
				for _, c := range []string{"Which source?", "alpha", "beta", "gamma"} {
					if !strings.Contains(out, c) {
						t.Errorf("rendered AskUser record = %q, want it to contain %q", out, c)
					}
				}
				if !m.testLive().active {
					t.Errorf("live.active = false, want true (the gate does not end the turn)")
				}
			},
		},
		{
			name: "UserInputRequested with no choices records a free-text question",
			events: []event.Event{
				event.TurnStarted{},
				event.UserInputRequested{ToolExecutionID: callID(5), Question: "free answer?", Choices: nil},
			},
			want: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1", len(m.testCommitted()))
				}
				rec := m.testCommitted()[0]
				if rec.Kind != kindPromptRecord || rec.Prompt == nil {
					t.Fatalf("committed[0] = %+v, want a kindPromptRecord", rec)
				}
				if rec.Prompt.Question != "free answer?" || len(rec.Prompt.Choices) != 0 {
					t.Errorf("Prompt = {%q, %d choices}, want {free answer?, 0}", rec.Prompt.Question, len(rec.Prompt.Choices))
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			tt.want(t, m)
		})
	}
}

// TestStepDoneNodePresence covers how a step commits under the node-presence rule (no
// "Multiple actions" umbrella, no promotion): an empty-text SINGLE-tool step commits
// just the one stand-alone tool node; the same step WITH thinking commits a thinking-only
// kindAssistant entry above the tool node; an empty-text MULTI-tool step commits NO
// assistant entry and each tool as its own stand-alone node; and a step WITH narration
// commits the "●" narration entry plus its stand-alone tool nodes.
func TestStepDoneNodePresence(t *testing.T) {
	tests := []struct {
		name   string
		events []event.Event
		check  func(t *testing.T, m transcriptModel)
	}{
		{
			name: "single empty-text tool → one stand-alone tool node, no assistant entry",
			events: []event.Event{
				event.TurnStarted{},
				stepDone(aiMessage("", "", toolUse("tu-1", "Bash", `{}`)), toolResult("tu-1", "out")),
			},
			check: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 1 {
					t.Fatalf("committed = %d, want 1 (just the tool node)", len(m.testCommitted()))
				}
				if e := m.testCommitted()[0]; e.Kind != kindTool {
					t.Errorf("committed[0] = %+v, want a kindTool node", e)
				}
				// The stand-alone node renders as a "○ ToolName" rail node, never a promoted
				// "●" bullet.
				got := stripANSI(strings.Join(renderEntry(m.testCommitted()[0], false, 80), "\n"))
				if !strings.Contains(got, "○ Bash") {
					t.Errorf("rendered node = %q, want a %q rail node", got, "○ Bash")
				}
				if strings.Contains(got, "●") {
					t.Errorf("rendered node = %q, must NOT be a promoted %q bullet", got, "●")
				}
			},
		},
		{
			name: "single empty-text tool WITH thinking → thinking entry then tool node",
			events: []event.Event{
				event.TurnStarted{},
				stepDone(aiMessage("plan it", "", toolUse("tu-1", "Bash", `{}`)), toolResult("tu-1", "out")),
			},
			check: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d, want 2 (thinking entry, tool node)", len(m.testCommitted()))
				}
				if e := m.testCommitted()[0]; e.Kind != kindAssistant || thinkingText(e.Blocks) == "" || textOnly(e.Blocks) != "" {
					t.Errorf("committed[0] = %+v, want a thinking-only kindAssistant", e)
				}
				if e := m.testCommitted()[1]; e.Kind != kindTool {
					t.Errorf("committed[1] = %+v, want the kindTool node", e)
				}
			},
		},
		{
			name: "multi empty-text tools → no assistant entry, two stand-alone tool nodes",
			events: []event.Event{
				event.TurnStarted{},
				stepDone(aiMessage("", "", toolUse("tu-1", "Bash", `{}`), toolUse("tu-2", "Fetch", `{}`)),
					toolResult("tu-1", "a"), toolResult("tu-2", "b")),
			},
			check: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d, want 2 (2 tool nodes, no umbrella)", len(m.testCommitted()))
				}
				for i := 0; i <= 1; i++ {
					if e := m.testCommitted()[i]; e.Kind != kindTool {
						t.Errorf("committed[%d] = %+v, want a kindTool node", i, e)
					}
				}
			},
		},
		{
			name: "text + tool → narration node plus a stand-alone tool node",
			events: []event.Event{
				event.TurnStarted{},
				stepDone(aiMessage("", "reading config", toolUse("tu-1", "Bash", `{}`)), toolResult("tu-1", "out")),
			},
			check: func(t *testing.T, m transcriptModel) {
				if len(m.testCommitted()) != 2 {
					t.Fatalf("committed = %d, want 2 (narration, tool node)", len(m.testCommitted()))
				}
				if e := m.testCommitted()[0]; e.Kind != kindAssistant || textOnly(e.Blocks) == "" {
					t.Errorf("committed[0] = %+v, want a narration kindAssistant", e)
				}
				if e := m.testCommitted()[1]; e.Kind != kindTool {
					t.Errorf("committed[1] = %+v, want a kindTool node", e)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			tt.check(t, m)
		})
	}
}

// userMsg builds a *content.UserMessage carrying one TextBlock, the authoritative
// payload a TurnStarted/TurnFoldedInto event carries for the committed user row.
func userMsg(text string) *content.UserMessage {
	return &content.UserMessage{Message: content.Message{
		Role:   content.RoleUser,
		Blocks: []content.Block{&content.TextBlock{Text: text}},
	}}
}

// userBlocks builds the []content.Block a submit produces, the blocks RecordSubmit
// remembers for the queued affordance.
func userBlocks(text string) []content.Block {
	return []content.Block{&content.TextBlock{Text: text}}
}

// kindUserCount counts the committed kindUser rows in m.
func kindUserCount(m transcriptModel) int {
	n := 0
	for _, e := range m.testCommitted() {
		if e.Kind == kindUser {
			n++
		}
	}
	return n
}

// queuedTexts returns the first-text-block text of each ready queued affordance, in
// order, for assertions.
func queuedTexts(m transcriptModel) []string {
	var out []string
	for _, blocks := range m.QueuedInputs() {
		out = append(out, blockText(blocks[0]))
	}
	return out
}

// TestTranscriptUserRowFromTurnEvent locks the event-driven user row: a TurnStarted /
// TurnFoldedInto with Cause.LoopID == 0 and a Message commits exactly ONE
// kindUser row equal to the Message blocks; a SUBAGENT hand-back (Cause.LoopID
// != 0) commits NO user row; a nil Message commits no row either.
func TestTranscriptUserRowFromTurnEvent(t *testing.T) {
	primary := callID(0)     // genuine user input: the zero (untriggered) loop id
	subagent := callID(0xBB) // a non-zero Cause.LoopID => subagent hand-back

	tests := []struct {
		name     string
		event    event.Event
		wantRows int
		wantText string // checked only when wantRows == 1
	}{
		{
			name:     "TurnStarted genuine user input commits one row",
			event:    event.TurnStarted{Header: event.Header{Cause: identity.Cause{CommandID: callID(1), Coordinates: identity.Coordinates{LoopID: primary}}}, Message: userMsg("hello")},
			wantRows: 1,
			wantText: "hello",
		},
		{
			name:     "TurnFoldedInto genuine user input commits one row",
			event:    event.TurnFoldedInto{Header: event.Header{Cause: identity.Cause{CommandID: callID(1), Coordinates: identity.Coordinates{LoopID: primary}}}, Message: userMsg("folded")},
			wantRows: 1,
			wantText: "folded",
		},
		{
			name:     "TurnStarted subagent hand-back commits no row",
			event:    event.TurnStarted{Header: event.Header{Cause: identity.Cause{CommandID: callID(1), Coordinates: identity.Coordinates{LoopID: subagent}}}, Message: userMsg("handback")},
			wantRows: 0,
		},
		{
			name:     "TurnFoldedInto subagent hand-back commits no row",
			event:    event.TurnFoldedInto{Header: event.Header{Cause: identity.Cause{CommandID: callID(1), Coordinates: identity.Coordinates{LoopID: subagent}}}, Message: userMsg("handback")},
			wantRows: 0,
		},
		{
			name:     "TurnStarted nil message commits no row",
			event:    event.TurnStarted{Header: event.Header{Cause: identity.Cause{CommandID: callID(1), Coordinates: identity.Coordinates{LoopID: primary}}}},
			wantRows: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			m = m.ApplyEvent(tt.event)
			if got := kindUserCount(m); got != tt.wantRows {
				t.Fatalf("kindUser rows = %d, want %d", got, tt.wantRows)
			}
			if tt.wantRows == 1 {
				e := m.testCommitted()[len(m.testCommitted())-1]
				if got := blockText(e.Blocks[0]); got != tt.wantText {
					t.Errorf("committed user row text = %q, want %q", got, tt.wantText)
				}
			}
		})
	}
}

// TestTranscriptUserRowsArePerLoop proves genuine TurnStarted and TurnFoldedInto input
// commits into whichever LoopID produced it, including a delegate loop.
func TestTranscriptUserRowsArePerLoop(t *testing.T) {
	loopA := callID(0xA1)
	subLoop := callID(0xC2) // a different (subagent) loop id

	tests := []struct {
		name     string
		event    event.Event
		wantRows int
	}{
		{
			name:     "TurnStarted on loop A commits a row",
			event:    event.TurnStarted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopA}, Cause: identity.Cause{CommandID: callID(1)}}, Message: userMsg("genuine")},
			wantRows: 1,
		},
		{
			name:     "TurnFoldedInto on loop A commits a row",
			event:    event.TurnFoldedInto{Header: event.Header{Coordinates: identity.Coordinates{LoopID: loopA}, Cause: identity.Cause{CommandID: callID(1)}}, Message: userMsg("folded")},
			wantRows: 1,
		},
		{
			name:     "TurnStarted on a delegate loop commits its task row",
			event:    event.TurnStarted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subLoop}, Cause: identity.Cause{CommandID: callID(1)}}, Message: userMsg("subagent task")},
			wantRows: 1,
		},
		{
			name:     "TurnFoldedInto on a delegate loop commits its row",
			event:    event.TurnFoldedInto{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subLoop}, Cause: identity.Cause{CommandID: callID(1)}}, Message: userMsg("subagent fold")},
			wantRows: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := transcriptModel{}
			m = m.ApplyEvent(tt.event)
			if got := kindUserCount(m); got != tt.wantRows {
				t.Fatalf("kindUser rows = %d, want %d", got, tt.wantRows)
			}
		})
	}
}

// TestTranscriptQueuedAffordance locks the full queued-input lifecycle: RecordSubmit
// then InputQueued shows the affordance; a later TurnStarted promotes it to exactly
// one committed user row (from the event Message) and clears the affordance. It also
// covers the race where InputQueued arrives BEFORE RecordSubmit — the affordance
// stays hidden (blockless) until the blocks land, then shows.
func TestTranscriptQueuedAffordance(t *testing.T) {
	id := callID(0x42)

	t.Run("RecordSubmit then InputQueued then TurnStarted", func(t *testing.T) {
		t.Parallel()
		var m transcriptModel
		m = m.RecordSubmit(id, userBlocks("queued one"))
		// Not shown until InputQueued.
		if got := queuedTexts(m); len(got) != 0 {
			t.Fatalf("queued before InputQueued = %v, want none (not shown yet)", got)
		}
		m = m.ApplyEvent(event.InputQueued{Header: event.Header{Cause: identity.Cause{CommandID: id}}})
		if got := queuedTexts(m); len(got) != 1 || got[0] != "queued one" {
			t.Fatalf("queued after InputQueued = %v, want [queued one]", got)
		}
		// TurnStarted promotes to one committed row and clears the affordance.
		m = m.ApplyEvent(event.TurnStarted{Header: event.Header{Cause: identity.Cause{CommandID: id}}, Message: userMsg("queued one")})
		if got := kindUserCount(m); got != 1 {
			t.Errorf("kindUser rows = %d, want exactly 1 (promoted once)", got)
		}
		if got := queuedTexts(m); len(got) != 0 {
			t.Errorf("queued after TurnStarted = %v, want none (affordance cleared)", got)
		}
	})

	t.Run("InputQueued races ahead of RecordSubmit", func(t *testing.T) {
		t.Parallel()
		var m transcriptModel
		// InputQueued arrives first: a shown-but-blockless placeholder; render skips it.
		m = m.ApplyEvent(event.InputQueued{Header: event.Header{Cause: identity.Cause{CommandID: id}}})
		if got := queuedTexts(m); len(got) != 0 {
			t.Fatalf("queued with no blocks yet = %v, want none (blockless placeholder skipped)", got)
		}
		// RecordSubmit fills the blocks: now it shows.
		m = m.RecordSubmit(id, userBlocks("late blocks"))
		if got := queuedTexts(m); len(got) != 1 || got[0] != "late blocks" {
			t.Errorf("queued after late RecordSubmit = %v, want [late blocks]", got)
		}
	})
}

// queuedFor builds an InputQueued event correlating submit id, produced by loopID —
// the per-loop stamp markQueued records so QueuedInputsFor can scope a loop's queue.
func queuedFor(id, loopID uuid.UUID) event.Event {
	return event.InputQueued{Header: event.Header{
		Coordinates: identity.Coordinates{LoopID: loopID},
		Cause:       identity.Cause{CommandID: id},
	}}
}

// blockTextsOf returns the first-text-block text of each queued message, in order.
func blockTextsOf(msgs [][]content.Block) []string {
	var out []string
	for _, b := range msgs {
		out = append(out, blockText(b[0]))
	}
	return out
}

// TestTranscriptQueuedInputsForLoop locks the per-loop scoping of the queued affordance:
// QueuedInputsFor returns only the inputs queued on the given loop (in submit order), so a
// one loop's queued message never leaks under another loop's view; the unscoped
// QueuedInputs still returns every queued input (scrollback); and when a queued input's turn
// starts it drops from its loop's queue without disturbing another loop's queue.
func TestTranscriptQueuedInputsForLoop(t *testing.T) {
	t.Parallel()

	loopA := callID(1)
	sub := callID(2)
	idA := callID(0x11)
	idB := callID(0x12)
	idC := callID(0x13)

	m := transcriptModel{}
	// Two inputs queued on loop A; one on a subagent loop.
	m = m.RecordSubmit(idA, userBlocks("loop A one"))
	m = m.ApplyEvent(queuedFor(idA, loopA))
	m = m.RecordSubmit(idB, userBlocks("loop A two"))
	m = m.ApplyEvent(queuedFor(idB, loopA))
	m = m.RecordSubmit(idC, userBlocks("sub one"))
	m = m.ApplyEvent(queuedFor(idC, sub))

	joined := func(s []string) string { return strings.Join(s, "|") }

	// The loop A view shows only its own two, in submit order — no subagent leak.
	if got, want := joined(blockTextsOf(m.QueuedInputsFor(loopA))), "loop A one|loop A two"; got != want {
		t.Errorf("QueuedInputsFor(loopA) = %q, want %q", got, want)
	}
	// The subagent view shows only the subagent's one — no loopA leak.
	if got, want := joined(blockTextsOf(m.QueuedInputsFor(sub))), "sub one"; got != want {
		t.Errorf("QueuedInputsFor(sub) = %q, want %q", got, want)
	}
	// Zero is not an alias for another loop.
	if got := joined(blockTextsOf(m.QueuedInputsFor(uuid.UUID{}))); got != "" {
		t.Errorf("QueuedInputsFor(zero) = %q, want empty", got)
	}
	// The unscoped read (scrollback) still returns all three.
	if got := len(m.QueuedInputs()); got != 3 {
		t.Errorf("QueuedInputs() = %d, want 3 (unscoped)", got)
	}

	// Loop A's first queued input's turn starts: it drops from the loop A queue (and
	// commits its real user row once) — no duplicate — and the subagent queue is untouched.
	m = m.ApplyEvent(event.TurnStarted{
		Header:  event.Header{Coordinates: identity.Coordinates{LoopID: loopA}, Cause: identity.Cause{CommandID: idA}},
		Message: userMsg("loop A one"),
	})
	if got, want := joined(blockTextsOf(m.QueuedInputsFor(loopA))), "loop A two"; got != want {
		t.Errorf("QueuedInputsFor(loopA) after dequeue = %q, want %q", got, want)
	}
	if got, want := joined(blockTextsOf(m.QueuedInputsFor(sub))), "sub one"; got != want {
		t.Errorf("QueuedInputsFor(sub) after loopA dequeue = %q, want %q (untouched)", got, want)
	}
	if got := kindUserCount(m); got != 1 {
		t.Errorf("kindUser rows = %d, want exactly 1 (promoted once, no duplicate)", got)
	}
}

// TestTranscriptInputCancelled locks that InputCancelled drops the queued affordance
// and commits NO row — a retracted/returned input simply disappears from the pending
// area.
func TestTranscriptInputCancelled(t *testing.T) {
	t.Parallel()
	id := callID(0x55)

	var m transcriptModel
	m = m.RecordSubmit(id, userBlocks("cancel me"))
	m = m.ApplyEvent(event.InputQueued{Header: event.Header{Cause: identity.Cause{CommandID: id}}})
	if got := queuedTexts(m); len(got) != 1 {
		t.Fatalf("setup: queued = %v, want one", got)
	}
	m = m.ApplyEvent(event.InputCancelled{Header: event.Header{Cause: identity.Cause{CommandID: id}}, Reason: event.CancelClientRetracted, Message: userMsg("cancel me")})
	if got := queuedTexts(m); len(got) != 0 {
		t.Errorf("queued after InputCancelled = %v, want none (affordance dropped)", got)
	}
	if got := kindUserCount(m); got != 0 {
		t.Errorf("kindUser rows = %d, want 0 (cancelled input commits no row)", got)
	}
}

// TestTranscriptTurnRejected locks that TurnRejected drops the affordance AND commits
// an error notice naming the reason — a rejected message must not silently vanish.
func TestTranscriptTurnRejected(t *testing.T) {
	id := callID(0x66)

	tests := []struct {
		name   string
		reason event.RejectReason
		want   string
	}{
		{name: "unspecified zero-value sentinel degrades to refused", reason: event.RejectUnspecified, want: "refused"},
		{name: "queue full", reason: event.RejectQueueFull, want: "queue full"},
		{name: "shutting down", reason: event.RejectShuttingDown, want: "shutting down"},
		{name: "internal", reason: event.RejectInternal, want: "internal error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			m = m.RecordSubmit(id, userBlocks("rejected"))
			m = m.ApplyEvent(event.InputQueued{Header: event.Header{Cause: identity.Cause{CommandID: id}}})
			m = m.ApplyEvent(event.TurnRejected{Header: event.Header{Cause: identity.Cause{CommandID: id}}, Reason: tt.reason})

			if got := queuedTexts(m); len(got) != 0 {
				t.Errorf("queued after TurnRejected = %v, want none (affordance dropped)", got)
			}
			if got := kindUserCount(m); got != 0 {
				t.Errorf("kindUser rows = %d, want 0 (a rejected message is surfaced as a notice, not a user row)", got)
			}
			rec := m.testCommitted()[len(m.testCommitted())-1]
			if rec.Kind != kindNotice || rec.Level != noticeError {
				t.Fatalf("last committed = (kind %d, level %d), want (kindNotice, noticeError)", rec.Kind, rec.Level)
			}
			text := blockText(rec.Blocks[0])
			if !strings.Contains(text, tt.want) {
				t.Errorf("rejection notice = %q, want it to mention %q", text, tt.want)
			}
		})
	}
}

// TestTranscriptRecordSubmitValueCopy locks the value-copy contract on the queued
// slice: RecordSubmit returns a new model whose queue mutation does not alias the
// prior model's backing array — a parent transcriptModel value kept around stays
// unchanged after a child records another submit.
func TestTranscriptRecordSubmitValueCopy(t *testing.T) {
	t.Parallel()

	base := transcriptModel{}.RecordSubmit(callID(1), userBlocks("first"))
	base = base.ApplyEvent(event.InputQueued{Header: event.Header{Cause: identity.Cause{CommandID: callID(1)}}})

	// Branch a child off base, recording a second submit. base must not gain it.
	child := base.RecordSubmit(callID(2), userBlocks("second"))
	child = child.ApplyEvent(event.InputQueued{Header: event.Header{Cause: identity.Cause{CommandID: callID(2)}}})

	if got := queuedTexts(base); len(got) != 1 || got[0] != "first" {
		t.Errorf("base queued = %v, want [first] (child must not mutate base's backing array)", got)
	}
	if got := queuedTexts(child); len(got) != 2 {
		t.Errorf("child queued = %v, want two entries", got)
	}
}

// TestGateDecisionFlow covers the permission verb end-to-end in the transcript: a
// PermissionRequested remembers the gate (committing nothing), ResolveGate records the
// keypress decision, and the step's StepDone bakes it onto the committed card so it
// reads "Approved …" / "Denied …". The step's thinking commits exactly once (the gate
// never commits prose — the duplicate-thinking fix), and the card uses the Tool(args)
// form.
func TestGateDecisionFlow(t *testing.T) {
	tests := []struct {
		name       string
		request    tool.Request
		toolName   string
		summary    string
		decision   gateDecision
		wantVerb   string
		wantHeader string
	}{
		{
			name:       "approved bash",
			request:    bashPermission("date"),
			toolName:   "Bash",
			summary:    "redacted command",
			decision:   gateApproved,
			wantVerb:   "Approved",
			wantHeader: "Bash(date)",
		},
		{
			name:       "denied fetch",
			request:    toolRequest("Fetch", "GET https://google.com/search?q=looprig", requirement("fetch https://google.com")),
			toolName:   "Fetch",
			summary:    "GET google.com",
			decision:   gateDenied,
			wantVerb:   "Denied",
			wantHeader: "Fetch(GET https://google.com/search?q=looprig)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var m transcriptModel
			m = m.ApplyEvent(event.TurnStarted{})
			m = m.ApplyEvent(thinkingChunk("let me run it"))
			m = m.ApplyEvent(event.PermissionRequested{ToolExecutionID: callID(1), Request: tt.request})
			// The gate commits nothing; the thinking stays live (uncommitted).
			if len(m.testCommitted()) != 0 {
				t.Fatalf("committed after gate = %d, want 0", len(m.testCommitted()))
			}
			// Screen records the keypress decision, then the loop runs the tool.
			m = m.ResolveGate(uuid.UUID{}, callID(1), tt.decision)
			m = m.ApplyEvent(toolStarted(callID(1), tt.toolName, tt.summary))
			m = m.ApplyEvent(toolCompleted(callID(1), tt.decision == gateDenied, "out"))
			m = m.ApplyEvent(stepDone(aiMessage("let me run it", "", toolUse("tu-1", tt.toolName, `{}`)), toolResult("tu-1", "out")))

			// committed: a thinking-only entry then the stand-alone tool node carrying the decision.
			if len(m.testCommitted()) != 2 {
				t.Fatalf("committed = %d, want 2 (thinking, tool node)", len(m.testCommitted()))
			}
			thinkCount := 0
			for _, e := range m.testCommitted() {
				if e.Kind == kindAssistant && thinkingText(e.Blocks) != "" {
					thinkCount++
				}
			}
			if thinkCount != 1 {
				t.Errorf("committed thinking entries = %d, want exactly 1 (no gate duplicate)", thinkCount)
			}
			card := m.testCommitted()[1]
			if card.Kind != kindTool {
				t.Fatalf("committed[1] = %+v, want the tool node", card)
			}
			if got := card.Calls[0].Decision; got != tt.decision {
				t.Errorf("card Decision = %v, want %v", got, tt.decision)
			}
			got := stripANSI(strings.Join(renderEntry(card, false, 80), "\n"))
			for _, w := range []string{tt.wantVerb, tt.wantHeader} {
				if !strings.Contains(got, w) {
					t.Errorf("rendered card = %q, want %q", got, w)
				}
			}
		})
	}
}

// TestStoredStepToolCardSummarizesInput covers the durable reconstruction path
// used when live ToolCallStarted summaries are unavailable, including subagent
// nested cards. It must still show the relevant target/command/request.
func TestStoredStepToolCardSummarizesInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		use         content.ToolUseBlock
		wantSummary string
	}{
		{
			name:        "read file path",
			use:         toolUse("read-1", "ReadFile", `{"path":"pkg/tui/render.go"}`),
			wantSummary: "pkg/tui/render.go",
		},
		{
			name:        "write file path and byte count",
			use:         toolUse("write-1", "WriteFile", `{"path":"README.md","content":"hello"}`),
			wantSummary: "README.md (5 bytes)",
		},
		{
			name:        "bash command",
			use:         toolUse("bash-1", "Bash", `{"command":"curl -p https://example.com"}`),
			wantSummary: "curl -p https://example.com",
		},
		{
			name:        "fetch method and url",
			use:         toolUse("fetch-1", "Fetch", `{"method":"GET","url":"https://google.com"}`),
			wantSummary: "GET https://google.com",
		},
		{
			name:        "grep pattern and path",
			use:         toolUse("grep-1", "Grep", `{"pattern":"needle","path":"pkg"}`),
			wantSummary: "needle in pkg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			card := storedStepToolCard(tt.use, nil)
			if card.Summary != tt.wantSummary {
				t.Errorf("storedStepToolCard().Summary = %q, want %q", card.Summary, tt.wantSummary)
			}
			got := stripANSI(strings.Join(renderEntry(entry{Kind: kindTool, Calls: []ToolCallView{card}}, false, 100), "\n"))
			if !strings.Contains(got, tt.use.Name+"("+tt.wantSummary+")") {
				t.Errorf("rendered card = %q, want %s(%s)", got, tt.use.Name, tt.wantSummary)
			}
		})
	}
}

// TestNestedTaskToolSummaryPreservesConcreteName proves the durable nested-card
// path keeps the Task tool name while redacting all task arguments.
func TestNestedTaskToolSummaryPreservesConcreteName(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)
	const (
		subject     = "nested sensitive subject"
		description = "nested sensitive description"
		metadata    = "nested sensitive metadata"
		taskID      = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "builder", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "build it"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("task-create-id", "TaskCreate", `{"subject":"nested sensitive subject","description":"nested sensitive description","metadata":{"secret":"nested sensitive metadata"},"taskId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`)),
		toolResult("task-create-id", "created"),
	))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}})
	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_X", "Subagent", `{"agent":"builder"}`)),
		toolResult("toolu_X", "done"),
	))

	card := findSubagentCard(t, m)
	if len(card.Children) != 1 {
		t.Fatalf("card.Children = %d, want 1 nested TaskCreate row; %+v", len(card.Children), card.Children)
	}
	child := card.Children[0]
	if child.ToolName != "TaskCreate" {
		t.Errorf("nested child ToolName = %q, want TaskCreate", child.ToolName)
	}
	if child.Summary != "" {
		t.Errorf("nested TaskCreate Summary = %q, want empty detail", child.Summary)
	}
	rendered := stripANSI(strings.Join(renderToolNode(child, 1, false, 100, false), "\n"))
	if !strings.Contains(rendered, "TaskCreate") {
		t.Errorf("nested child rendering = %q, want concrete TaskCreate name", rendered)
	}
	for _, secret := range []string{subject, description, metadata, taskID} {
		if strings.Contains(rendered, secret) {
			t.Errorf("nested child rendering = %q contains sensitive value %q", rendered, secret)
		}
	}
}

// TestLoopShortForm covers the loopID fallback label (the 8-hex first group of the
// uuid string), used when a subagent has no/empty agent name.
func TestLoopShortForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   uuid.UUID
		want string
	}{
		{name: "non-zero id yields first hex group", in: callID(0xAB), want: "ab000000"},
		{name: "zero id yields the zero short form", in: uuid.UUID{}, want: "00000000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := loopShortForm(tt.in); got != tt.want {
				t.Errorf("loopShortForm(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// childLoopStarted builds a child loop's LoopStarted: the child loop id + agent name,
// the spawning parent loop/turn/step coordinates on Cause, and the durable provider
// tool-use id of the Subagent call that spawned it. It is the event that records the
// child→parent spawn relationship in the accumulator.
func childLoopStarted(child uuid.UUID, name identity.AgentName, parentLoop, parentTurn, parentStep uuid.UUID, parentToolUseID string) event.Event {
	return event.LoopStarted{
		Header: event.Header{
			Coordinates: identity.Coordinates{LoopID: child},
			AgentName:   name,
			Cause: identity.Cause{
				Coordinates: identity.Coordinates{LoopID: parentLoop, TurnID: parentTurn, StepID: parentStep},
			},
		},
		ParentToolUseID: parentToolUseID,
	}
}

// childTurnStarted builds a child loop's first TurnStarted carrying its task message.
func childTurnStarted(child uuid.UUID, task string) event.Event {
	return event.TurnStarted{
		Header:  event.Header{Coordinates: identity.Coordinates{LoopID: child}},
		Message: &content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: []content.Block{&content.TextBlock{Text: task}}}},
	}
}

// orchestratorStepDone builds the active loop's StepDone stamped with its full
// turn/step coordinates, so a Subagent ToolUseBlock in its group reconciles with the
// child accumulator keyed by {primaryLoop, turn, step, block.ID}.
func orchestratorStepDone(primaryLoop, turn, step uuid.UUID, msgs ...content.Conversation) event.Event {
	return event.StepDone{
		Header:   event.Header{Coordinates: identity.Coordinates{LoopID: primaryLoop, TurnID: turn, StepID: step}},
		Messages: content.AgenticMessages(msgs),
	}
}

// findSubagentCard returns the single committed kindTool entry's Subagent ToolCallView
// (the one whose Agent is set), or fails if there is not exactly one such card.
func findSubagentCard(t *testing.T, m transcriptModel) ToolCallView {
	t.Helper()
	var found []ToolCallView
	for _, e := range m.testCommitted() {
		if e.Kind != kindTool {
			continue
		}
		for _, c := range e.Calls {
			if c.Agent != "" {
				found = append(found, c)
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("found %d Subagent cards, want exactly 1; committed=%+v", len(found), m.testCommitted())
	}
	return found[0]
}

// TestSubagentNestedFromEnduring (Task 6, test 1): the full card paints from ENDURING
// child events only — child LoopStarted (agent), child TurnStarted (task), child
// StepDone (nested tool card via storedStepToolCard), child TurnDone (status) — and
// reconciles at the orchestrator's StepDone keyed by the durable provider tool-use id.
// The child's ToolExecutionID is deliberately DIFFERENT from the provider id, proving
// the match is by provider id (content.ToolUseBlock.ID), not the runner execution id.
func TestSubagentNestedFromEnduring(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "map repo"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("grep-block-id", "Grep", `{"q":"foo"}`)),
		toolResult("grep-block-id", "child grep hit"),
	))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}})

	// The orchestrator's hand-back: a Subagent ToolUseBlock{ID:"toolu_X"} with its
	// result text. The ToolExecutionID namespace never appears here — match is by id.
	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_X", "Subagent", `{"agent":"explorer","message":"map repo"}`)),
		toolResult("toolu_X", "result text"),
	))

	card := findSubagentCard(t, m)
	if card.ToolName != "Subagent" {
		t.Errorf("card.ToolName = %q, want Subagent", card.ToolName)
	}
	if card.Agent != "explorer" {
		t.Errorf("card.Agent = %q, want explorer", card.Agent)
	}
	if card.Task != "map repo" {
		t.Errorf("card.Task = %q, want %q", card.Task, "map repo")
	}
	if card.Steps != 1 {
		t.Errorf("card.Steps = %d, want 1", card.Steps)
	}
	if card.SubStatus != subDone {
		t.Errorf("card.SubStatus = %v, want subDone", card.SubStatus)
	}
	if len(card.Children) != 1 {
		t.Fatalf("card.Children = %d, want 1 (the Grep row); %+v", len(card.Children), card.Children)
	}
	child := card.Children[0]
	if child.ToolName != "Grep" {
		t.Errorf("child.ToolName = %q, want Grep", child.ToolName)
	}
	if got := strings.Join(child.Result, "\n"); got != "child grep hit" {
		t.Errorf("child.Result = %q, want %q", got, "child grep hit")
	}
	// The done summary is the parent Subagent tool-result text, truncated.
	if got := strings.Join(card.Result, "\n"); got != "result text" {
		t.Errorf("done summary (card.Result) = %q, want %q", got, "result text")
	}
	// No stray ▸ subagent line for a parented child.
	for _, e := range m.testCommitted() {
		if e.Kind == kindSubagent {
			t.Errorf("committed a kindSubagent fallback line for a parented child: %+v", e)
		}
	}
}

// TestStartAgentReconcilesStreamingAccumulator covers the current AgentTools spawn name.
// The child accumulator is rendered provisionally while the child runs; once the parent's
// authoritative StepDone commits StartAgent, that provisional card must be consumed instead
// of remaining beside the committed tool card as a duplicate.
func TestStartAgentReconcilesStreamingAccumulator(t *testing.T) {
	t.Parallel()
	if got := nonSubagentCalls([]ToolCallView{{ToolName: "StartAgent", Status: ToolRunning}}, 1); len(got) != 0 {
		t.Fatalf("raw live StartAgent calls after agent-card filtering = %d, want 0", len(got))
	}

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "generic", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "inspect the available tools"))
	m = m.ApplyEvent(stepDoneFrom(sub, aiMessage("", "inspection complete")))
	m = m.ApplyEvent(event.TurnDone{Header: hdr(sub)})

	if got := m.pendingSubagentCardsFor(primary); len(got) != 1 {
		t.Fatalf("pending cards before parent StepDone = %d, want 1", len(got))
	}

	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_X", "StartAgent", `{"agent_type":"generic","instructions":"inspect the available tools"}`)),
		toolResult("toolu_X", `{"state":"completed","response":"inspection complete"}`),
	))

	if got := m.pendingSubagentCardsFor(primary); len(got) != 0 {
		t.Fatalf("pending cards after parent StepDone = %d, want 0 (streaming card reconciled)", len(got))
	}
	var reconciled []ToolCallView
	for _, e := range m.testCommitted() {
		for _, call := range e.Calls {
			if call.Agent != "" {
				reconciled = append(reconciled, call)
			}
		}
	}
	if len(reconciled) != 1 {
		t.Fatalf("reconciled agent cards = %d, want exactly 1; committed=%+v", len(reconciled), m.testCommitted())
	}
	if reconciled[0].ToolName != "StartAgent" || reconciled[0].Agent != "generic" {
		t.Errorf("reconciled card = (%q, %q), want StartAgent(generic)", reconciled[0].ToolName, reconciled[0].Agent)
	}
}

// Parent and child loops publish on independent streams, so the parent's authoritative
// StepDone may reach the presentation before the child's LoopStarted. The later child
// event must promote the already-committed StartAgent card in place instead of adding a
// second provisional agent card below it.
func TestStartAgentReconcilesWhenParentStepArrivesBeforeChild(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)
	const toolUseID = "toolu_parent_first"

	m := transcriptModel{}
	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse(toolUseID, "StartAgent", `{"agent_type":"generic","instructions":"inspect"}`)),
		toolResult(toolUseID, `{"state":"accepted"}`),
	))
	m = m.ApplyEvent(childLoopStarted(sub, "generic", primary, turn, step, toolUseID))

	card := findSubagentCard(t, m)
	if card.ToolName != startAgentToolName || card.Agent != "generic" {
		t.Errorf("promoted card = (%q, %q), want StartAgent(generic)", card.ToolName, card.Agent)
	}
	if got := len(m.pendingSubagentCardsFor(primary)); got != 0 {
		t.Errorf("pending cards after parent-first reconciliation = %d, want 0", got)
	}
}

func TestStartAgentCommittedBeforeChildTerminalUpdatesInPlace(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)
	committedTurn := callID(0xE3)
	committedStep := callID(0xF4)
	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "generic", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "background task"))
	// Deliberately diverge the parent's committed coordinates from LoopStarted.Cause;
	// reconciliation uses the unique parent-loop + provider-id fallback and must still
	// bind later child events to the committed card.
	m = m.ApplyEvent(orchestratorStepDone(primary, committedTurn, committedStep,
		aiMessage("", "", toolUse("toolu_X", "StartAgent", `{"agent_type":"generic","instructions":"background task","wait_for_response":false}`)),
		toolResult("toolu_X", `{"state":"running"}`),
	))
	if got := m.pendingSubagentCardsFor(primary); len(got) != 0 {
		t.Fatalf("pending cards after parent StepDone = %d, want 0 (single committed card)", len(got))
	}
	m = m.ApplyEvent(event.TurnDone{Header: hdr(primary)})
	m = m.ApplyEvent(stepDoneFrom(sub, aiMessage("", "child finished")))
	m = m.ApplyEvent(event.TurnDone{Header: hdr(sub)})

	var cards []ToolCallView
	committed, _ := m.projectionFor(primary)
	for _, e := range committed {
		for _, call := range e.Calls {
			if call.ToolName == "StartAgent" {
				cards = append(cards, call)
			}
		}
	}
	if len(cards) != 1 {
		t.Fatalf("committed StartAgent cards = %d, want exactly 1; committed=%+v", len(cards), committed)
	}
	if cards[0].Agent != "generic" || cards[0].Steps != 1 || cards[0].SubStatus != subDone {
		t.Fatalf("updated committed card = %+v, want generic done with 1 step", cards[0])
	}
}

// TestSubagentMixedBatchSameIndexIsolation (Task 6, test 2 / design test 13): the
// parent step is Bash(idx0)+Subagent(idx1) and the child's FIRST tool is ALSO Bash
// with a DIFFERENT result, while the parent's live Bash sits in m.testLive().Calls[0]. The
// nested child Bash card MUST carry the CHILD's durable result, never the parent's
// live card — proving child reconstruction uses storedStepToolCard (no m.testLive().Calls).
func TestSubagentMixedBatchSameIndexIsolation(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	// Parent's live Bash card at index 0 (as if the orchestrator's own Bash streamed).
	m = m.ApplyEvent(event.TurnStarted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}}})
	m = m.ApplyEvent(toolStarted(callID(0x11), "Bash", "parent bash"))
	m = m.ApplyEvent(toolCompleted(callID(0x11), false, "PARENT bash output"))

	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "investigate"))
	// Child's FIRST tool is also Bash — same index (0) as the parent's live Bash.
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("child-bash-id", "Bash", `{"command":"ls"}`)),
		toolResult("child-bash-id", "CHILD bash output"),
	))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}})

	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "",
			toolUse("parent-bash-block", "Bash", `{"command":"make"}`),
			toolUse("toolu_X", "Subagent", `{"agent":"explorer"}`),
		),
		toolResult("parent-bash-block", "PARENT bash output"),
		toolResult("toolu_X", "subagent result"),
	))

	card := findSubagentCard(t, m)
	if len(card.Children) != 1 {
		t.Fatalf("card.Children = %d, want 1 nested Bash row; %+v", len(card.Children), card.Children)
	}
	child := card.Children[0]
	if child.Summary != "ls" {
		t.Errorf("nested child Bash summary = %q, want command %q", child.Summary, "ls")
	}
	if got := strings.Join(child.Result, "\n"); got != "CHILD bash output" {
		t.Errorf("nested child Bash result = %q, want the CHILD's result %q (not the parent live card)", got, "CHILD bash output")
	}
	// Child tool nodes are no longer rendered, so the same-index isolation is asserted on
	// the card DATA (above) rather than the rendered string. The rendered card must NOT
	// leak the child's Bash command.
	renderedSubagent := stripANSI(renderSubagentCard(card, false, 120))
	if strings.Contains(renderedSubagent, "Bash(ls)") {
		t.Errorf("rendered subagent card = %q, must NOT render the nested Bash command", renderedSubagent)
	}

	var parentBash *entry
	for i := range m.testCommitted() {
		e := &m.testCommitted()[i]
		if e.Kind == kindTool && len(e.Calls) == 1 && e.Calls[0].ToolName == "Bash" && strings.Join(e.Calls[0].Result, "\n") == "PARENT bash output" {
			parentBash = e
			break
		}
	}
	if parentBash == nil {
		t.Fatalf("missing committed parent Bash card; committed=%+v", m.testCommitted())
	}
	renderedParent := stripANSI(strings.Join(renderEntry(*parentBash, false, 120), "\n"))
	if !strings.Contains(renderedParent, "Bash(make)") {
		t.Errorf("rendered parent Bash card = %q, want parent command", renderedParent)
	}
}

// TestSubagentConcurrent (Task 6, test 3): two children (toolu_A/toolu_B) under one
// orchestrator step with two Subagent blocks — each card gets ONLY its own child rows.
func TestSubagentConcurrent(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	subA := callID(0xB2)
	subB := callID(0xB3)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(subA, "explorer", primary, turn, step, "toolu_A"))
	m = m.ApplyEvent(childTurnStarted(subA, "task A"))
	m = m.ApplyEvent(stepDoneFrom(subA,
		aiMessage("", "", toolUse("a-grep", "Grep", `{}`)),
		toolResult("a-grep", "A hit"),
	))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subA}}})

	m = m.ApplyEvent(childLoopStarted(subB, "builder", primary, turn, step, "toolu_B"))
	m = m.ApplyEvent(childTurnStarted(subB, "task B"))
	m = m.ApplyEvent(stepDoneFrom(subB,
		aiMessage("", "", toolUse("b-read", "Read", `{}`)),
		toolResult("b-read", "B content"),
	))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subB}}})

	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "",
			toolUse("toolu_A", "Subagent", `{"agent":"explorer"}`),
			toolUse("toolu_B", "Subagent", `{"agent":"builder"}`),
		),
		toolResult("toolu_A", "A done"),
		toolResult("toolu_B", "B done"),
	))

	var cardA, cardB *ToolCallView
	for _, e := range m.testCommitted() {
		if e.Kind != kindTool {
			continue
		}
		for i := range e.Calls {
			c := e.Calls[i]
			switch c.Agent {
			case "explorer":
				cc := c
				cardA = &cc
			case "builder":
				cc := c
				cardB = &cc
			}
		}
	}
	if cardA == nil || cardB == nil {
		t.Fatalf("missing a Subagent card: explorer=%v builder=%v; committed=%+v", cardA, cardB, m.testCommitted())
	}
	if len(cardA.Children) != 1 || cardA.Children[0].ToolName != "Grep" {
		t.Errorf("explorer card children = %+v, want exactly the Grep row", cardA.Children)
	}
	if len(cardB.Children) != 1 || cardB.Children[0].ToolName != "Read" {
		t.Errorf("builder card children = %+v, want exactly the Read row", cardB.Children)
	}
	if cardA.Task != "task A" || cardB.Task != "task B" {
		t.Errorf("tasks crossed: explorer=%q builder=%q", cardA.Task, cardB.Task)
	}
}

// TestSubagentEmptyParentToolUseIDFallback (Task 6, test 4): a child LoopStarted with
// ParentToolUseID:"" gets NO accumulator; its StepDone commits the EXISTING collapsed
// "▸ explorer: done" fallback line (kindSubagent), the path retained for non-tool
// spawns.
// TestSubagentAllSubagentStepNoUmbrella (Task 7 / design §5): a step whose tool-uses are
// ALL Subagent (and no narration) commits NO "Multiple actions" umbrella — the named
// "●" Subagent cards stack directly. Reuses the concurrent two-subagent setup.
func TestSubagentAllSubagentStepNoUmbrella(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	subA := callID(0xB2)
	subB := callID(0xB3)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(subA, "explorer", primary, turn, step, "toolu_A"))
	m = m.ApplyEvent(childTurnStarted(subA, "task A"))
	m = m.ApplyEvent(stepDoneFrom(subA, aiMessage("", "", toolUse("a-grep", "Grep", `{}`)), toolResult("a-grep", "A hit")))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subA}}})
	m = m.ApplyEvent(childLoopStarted(subB, "builder", primary, turn, step, "toolu_B"))
	m = m.ApplyEvent(childTurnStarted(subB, "task B"))
	m = m.ApplyEvent(stepDoneFrom(subB, aiMessage("", "", toolUse("b-read", "Read", `{}`)), toolResult("b-read", "B content")))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: subB}}})

	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "",
			toolUse("toolu_A", "Subagent", `{"agent":"explorer"}`),
			toolUse("toolu_B", "Subagent", `{"agent":"builder"}`),
		),
		toolResult("toolu_A", "A done"),
		toolResult("toolu_B", "B done"),
	))

	// An all-subagent empty-text step commits NO assistant entry at all (no umbrella): the
	// named "●" Subagent cards stack directly as their own kindTool entries.
	for _, e := range m.testCommitted() {
		if e.Kind == kindAssistant {
			t.Errorf("all-subagent step committed a kindAssistant entry, want none; committed=%+v", m.testCommitted())
		}
	}
	// Two Subagent cards committed as their own kindTool entries (each renders as a "●"
	// Subagent card via Agent != "").
	subCards := 0
	for _, e := range m.testCommitted() {
		if e.Kind != kindTool {
			continue
		}
		for _, c := range e.Calls {
			if c.Agent != "" {
				subCards++
			}
		}
	}
	if subCards != 2 {
		t.Fatalf("committed %d Subagent cards, want 2; committed=%+v", subCards, m.testCommitted())
	}
}

// TestSubagentMixedStepTopology (Task 7 / design §5): a mixed step — narration + an
// ordinary Bash tool + a Subagent call — commits the narration as the "●" assistant
// node, the Bash as its own ordinary tool node, and the Subagent as its OWN "●" card.
// No "Multiple actions" umbrella (there never is one anymore).
func TestSubagentMixedStepTopology(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "investigate"))
	m = m.ApplyEvent(stepDoneFrom(sub, aiMessage("", "", toolUse("c-grep", "Grep", `{}`)), toolResult("c-grep", "child hit")))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}})

	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "looking into it",
			toolUse("p-bash", "Bash", `{"command":"ls"}`),
			toolUse("toolu_X", "Subagent", `{"agent":"explorer"}`),
		),
		toolResult("p-bash", "parent bash out"),
		toolResult("toolu_X", "subagent summary"),
	))

	// Narration node, and NO empty-text umbrella: the only kindAssistant entry carries the
	// narration text.
	sawNarration := false
	for _, e := range m.testCommitted() {
		if e.Kind == kindAssistant && textOnly(e.Blocks) == "looking into it" {
			sawNarration = true
		}
	}
	if !sawNarration {
		t.Errorf("mixed step did not commit the narration node; committed=%+v", m.testCommitted())
	}

	// The ordinary Bash card: a kindTool with no Agent; the Subagent: a kindTool with Agent.
	sawBash, sawSub := false, false
	for _, e := range m.testCommitted() {
		if e.Kind != kindTool {
			continue
		}
		for _, c := range e.Calls {
			switch {
			case c.Agent != "":
				sawSub = true
			case c.ToolName == "Bash":
				sawBash = true
			}
		}
	}
	if !sawBash {
		t.Errorf("mixed step missing the ordinary Bash ⎿ card; committed=%+v", m.testCommitted())
	}
	if !sawSub {
		t.Errorf("mixed step missing the Subagent ● card; committed=%+v", m.testCommitted())
	}
}

// TestSubagentDepth2Nested (Task 7 / design §6): a depth-2 StepDone (a sub-subagent
// spawned by the depth-1 subagent) does NOT render its own card — it increments the
// depth-1 card's Nested counter, attributed by walking the LoopStarted.Cause.LoopID
// ancestry up to the depth-1 loop (the one whose parent is the primary and which has a
// non-empty ParentToolUseID).
func TestSubagentDepth2Nested(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	depth1 := callID(0xB2)
	depth2 := callID(0xB3)
	turn := callID(0xC3)
	step := callID(0xD4)
	d1turn := callID(0xE5)
	d1step := callID(0xF6)

	m := transcriptModel{}
	// depth-1: spawned by the primary via toolu_X.
	m = m.ApplyEvent(childLoopStarted(depth1, "explorer", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(depth1, "map repo"))
	// depth-1 runs a step that itself spawns depth-2 (the Subagent tool-use), and the
	// depth-1 StepDone carries that step's coordinates (d1turn/d1step) on its header.
	m = m.ApplyEvent(event.StepDone{
		Header:   event.Header{Coordinates: identity.Coordinates{LoopID: depth1, TurnID: d1turn, StepID: d1step}},
		Messages: content.AgenticMessages{aiMessage("", "", toolUse("toolu_Y", "Subagent", `{"agent":"deep"}`)), toolResult("toolu_Y", "spawned deep")},
	})
	// depth-2: spawned by depth-1 via toolu_Y (Cause points at the depth-1 loop/turn/step).
	m = m.ApplyEvent(childLoopStarted(depth2, "deep", depth1, d1turn, d1step, "toolu_Y"))
	m = m.ApplyEvent(childTurnStarted(depth2, "deep task"))
	// A depth-2 StepDone — must NOT render its own card; bumps the depth-1 Nested.
	m = m.ApplyEvent(stepDoneFrom(depth2, aiMessage("", "", toolUse("d-grep", "Grep", `{}`)), toolResult("d-grep", "deep hit")))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: depth2}}})
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: depth1}}})

	// The orchestrator hands depth-1 back.
	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_X", "Subagent", `{"agent":"explorer"}`)),
		toolResult("toolu_X", "explorer summary"),
	))

	card := findSubagentCard(t, m)
	if card.Nested != 1 {
		t.Errorf("depth-1 card.Nested = %d, want 1 (one depth-2 StepDone, attributed by ancestry walk)", card.Nested)
	}
	// The depth-2 StepDone must NOT have produced its OWN nested child on the depth-1
	// card (it is collapsed into the counter, not rendered as a child tool card). The
	// depth-1 card's own step (the Subagent spawn) IS a child; the depth-2 Grep is not.
	for _, c := range card.Children {
		if c.ToolName == "Grep" {
			t.Errorf("depth-2 Grep leaked into the depth-1 card children, want it collapsed into Nested: %+v", card.Children)
		}
	}
}

// renderSubagentCardEntry renders the single committed entry holding a populated
// Subagent card (Agent != "") as ANSI-stripped text, or fails if there is not exactly
// one. It is the render-layer twin of findSubagentCard, used by the end-to-end terminal
// tests to assert the reducer's status drives the rendered done line.
func renderSubagentCardEntry(t *testing.T, m transcriptModel, width int) string {
	t.Helper()
	var rendered []string
	for _, e := range m.testCommitted() {
		if e.Kind != kindTool {
			continue
		}
		for _, c := range e.Calls {
			if c.Agent != "" {
				rendered = append(rendered, stripANSI(strings.Join(renderEntry(e, false, width), "\n")))
				break
			}
		}
	}
	if len(rendered) != 1 {
		t.Fatalf("found %d rendered Subagent cards, want exactly 1; committed=%+v", len(rendered), m.testCommitted())
	}
	return rendered[0]
}

// subagentFlowEvents builds the full Enduring event sequence for one tool-spawned
// subagent flow that paints a populated card: child LoopStarted (with the spawning
// parent coordinates + provider tool-use id), child TurnStarted (task), two child
// StepDones (so the card carries multiple nested children + a Steps count > 1), the
// child terminal (TurnDone), then the orchestrator's StepDone whose Subagent block
// reconciles the accumulator. It is shared by the restore-equivalence property test so
// the LIVE per-event fold and the FoldDisplay restore fold consume the IDENTICAL slice.
func subagentFlowEvents(primary, sub, turn, step uuid.UUID, toolUseID string) []event.Event {
	return []event.Event{
		childLoopStarted(sub, "explorer", primary, turn, step, toolUseID),
		childTurnStarted(sub, "map the repo"),
		stepDoneFrom(sub,
			aiMessage("", "", toolUse("c-grep", "Grep", `{"q":"foo"}`)),
			toolResult("c-grep", "grep hit one"),
		),
		stepDoneFrom(sub,
			aiMessage("", "", toolUse("c-read", "Read", `{"path":"x"}`)),
			toolResult("c-read", "file body"),
		),
		event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}},
		orchestratorStepDone(primary, turn, step,
			aiMessage("", "", toolUse(toolUseID, "Subagent", `{"agent":"explorer","message":"map the repo"}`)),
			toolResult(toolUseID, "mapped 12 packages"),
		),
	}
}

// TestSubagentRestoreEquivalence (Task 8, design test 3 — HEADLINE property): the cold-
// restore repaint reproduces the live transcript EXACTLY for a populated Subagent card.
// It folds the SAME Enduring sequence through (a) the LIVE per-event path (transcript +
// interaction ApplyEvent, the way the live session consumes events) and (b) FoldDisplay
// (the restore path restoreBacklogCmd runs), then asserts EqualTranscript — the
// reflect.DeepEqual over the committed transcript. This is the property the defensive
// Children copy in reconcileSubagent protects: a committed card must be structurally
// FROZEN, never aliasing the live subagentAccum backing slice, so the two folds are
// byte-for-byte equal. The card is asserted populated (agent, task, two nested children,
// Steps==2, subDone, summary) so the equality genuinely exercises a filled-in card.
func TestSubagentRestoreEquivalence(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)
	events := subagentFlowEvents(primary, sub, turn, step, "toolu_X")

	// LIVE path: fold every event through the pure reducers per-event, exactly as the
	// live session does, then wrap the resulting reducer state as a DisplayProjection.
	liveTr := transcriptModel{}
	liveIn := newInteractionModel()
	for _, ev := range events {
		liveTr = liveTr.ApplyEvent(ev)
		liveIn = liveIn.ApplyEvent(ev)
	}
	live := DisplayProjection{transcript: liveTr, interaction: liveIn}

	// RESTORE path: fold the SAME slice through FoldDisplay (restoreBacklogCmd's fold).
	restored := FoldDisplay(events)

	// First, prove the card is genuinely POPULATED — otherwise EqualTranscript could pass
	// over two empty transcripts and assert nothing.
	card := findSubagentCard(t, restored.transcript)
	if card.Agent != "explorer" || card.Task != "map the repo" {
		t.Fatalf("restored card not populated: Agent=%q Task=%q", card.Agent, card.Task)
	}
	if card.Steps != 2 {
		t.Fatalf("restored card.Steps = %d, want 2 (two child StepDones)", card.Steps)
	}
	if card.SubStatus != subDone {
		t.Fatalf("restored card.SubStatus = %v, want subDone", card.SubStatus)
	}
	if len(card.Children) != 2 {
		t.Fatalf("restored card.Children = %d, want 2 (Grep + Read); %+v", len(card.Children), card.Children)
	}
	if got := strings.Join(card.Result, "\n"); got != "mapped 12 packages" {
		t.Fatalf("restored done summary = %q, want %q", got, "mapped 12 packages")
	}

	// HEADLINE: the restored transcript EqualTranscript the live transcript.
	if !restored.EqualTranscript(live) {
		t.Errorf("restore != live for a populated Subagent card\n live committed = %+v\n restored committed = %+v",
			live.transcript.testCommitted(), restored.transcript.testCommitted())
	}

	// The committed card must be FROZEN — never aliasing the live subagentAccum backing
	// slice (the defensive append-copy in reconcileSubagent, design §3 freeze). Mutate the
	// live model's accumulator IN PLACE (overwrite an existing child, so no realloc masks
	// an alias) AFTER commit and confirm the committed card is unaffected: had the card
	// aliased acc.children, this write would leak into the committed transcript. This is
	// the regression the Task 6 defensive copy guards.
	key := spawnKey{primary, turn, step, "toolu_X"}
	acc, ok := liveTr.subagentAccum[key]
	if !ok {
		t.Fatalf("no live accumulator for key %+v; subagentAccum=%+v", key, liveTr.subagentAccum)
	}
	if len(acc.children) == 0 {
		t.Fatalf("accumulator carries no children to mutate; acc=%+v", acc)
	}
	acc.children[0] = ToolCallView{ToolName: "LEAK", Result: []string{"should not appear"}}
	frozen := findSubagentCard(t, liveTr)
	if frozen.Children[0].ToolName != "Grep" {
		t.Errorf("committed card aliases the live accumulator: in-place mutation leaked into the frozen card (Children[0]=%+v, want the original Grep)", frozen.Children[0])
	}
}

// TestSubagentFailureBeforeChildLoop (Task 8, design test 5): the Subagent spawn FAILS
// before any child LoopStarted (unknown agent / spawn error → the tool returns an
// "error: ..." text result, no child loop, no accumulator). The orchestrator's StepDone
// then carries a Subagent tool-use whose paired ToolResultMessage is that error text.
// With no accumulator, reconcileSubagent leaves the card unchanged: Agent=="" so it is
// NOT promoted to a "●" Subagent card — it commits as an ORDINARY tool card whose body
// IS the error text, with NO children and NO done child.
func TestSubagentFailureBeforeChildLoop(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	turn := callID(0xC3)
	step := callID(0xD4)

	const errText = "error: subagent failed: unknown agent"

	m := transcriptModel{}
	// NO child LoopStarted is ever fed — the spawn failed before a child loop existed.
	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_X", "Subagent", `{"agent":"unknown","message":"go"}`)),
		toolResult("toolu_X", errText),
	))

	// No populated Subagent card (Agent never set) — confirm there is none.
	for _, e := range m.testCommitted() {
		for _, c := range e.Calls {
			if c.Agent != "" {
				t.Fatalf("spawn failure produced a populated Subagent card, want a plain error tool card: %+v", c)
			}
		}
	}

	// The Subagent tool-use commits as an ordinary tool card whose body is the error text.
	var found *ToolCallView
	for _, e := range m.testCommitted() {
		if e.Kind != kindTool {
			continue
		}
		for i := range e.Calls {
			if e.Calls[i].ToolName == "Subagent" {
				cc := e.Calls[i]
				found = &cc
			}
		}
	}
	if found == nil {
		t.Fatalf("no Subagent tool card committed; committed=%+v", m.testCommitted())
	}
	if found.Agent != "" {
		t.Errorf("error card Agent = %q, want empty (a plain tool card, not a nested ● card)", found.Agent)
	}
	if len(found.Children) != 0 {
		t.Errorf("error card has %d children, want 0 (no child loop ran): %+v", len(found.Children), found.Children)
	}
	if got := strings.Join(found.Result, "\n"); got != errText {
		t.Errorf("error card body = %q, want the error result %q", got, errText)
	}
	// The rendered card must show the error text as an ordinary tool-card body, NOT a
	// Subagent "done · N steps" done child or a "Subagent(<agent>)" header.
	for _, e := range m.testCommitted() {
		got := stripANSI(strings.Join(renderEntry(e, true, 80), "\n"))
		if strings.Contains(got, " steps") || strings.Contains(got, "Subagent(") {
			t.Errorf("error card rendered as a Subagent card: %q", got)
		}
	}
}

// TestSubagentInterruption (Task 8, design test 6): a child loop is INTERRUPTED
// (TurnInterrupted) after its StepDone. End to end (reducer → render): the committed
// card carries SubStatus == subInterrupted and renders the "interrupted" done line with
// NO summary (design §4: interrupted omits the summary).
func TestSubagentInterruption(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "investigate"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("c-grep", "Grep", `{}`)),
		toolResult("c-grep", "partial hit"),
	))
	// The child is interrupted mid-flight (no TurnDone).
	m = m.ApplyEvent(event.TurnInterrupted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}})

	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_X", "Subagent", `{"agent":"explorer"}`)),
		toolResult("toolu_X", "stopped early"),
	))

	card := findSubagentCard(t, m)
	if card.SubStatus != subInterrupted {
		t.Fatalf("card.SubStatus = %v, want subInterrupted", card.SubStatus)
	}
	if card.Steps != 1 {
		t.Errorf("card.Steps = %d, want 1", card.Steps)
	}

	// Render the committed card: the done line reads "interrupted" and OMITS the summary.
	rendered := renderSubagentCardEntry(t, m, 100)
	if !strings.Contains(rendered, "interrupted") {
		t.Errorf("rendered card = %q, want the \"interrupted\" done line", rendered)
	}
	if strings.Contains(rendered, "stopped early") {
		t.Errorf("rendered card = %q, must NOT show a summary for an interrupted child", rendered)
	}
}

// TestSubagentAccumClearedOnParentTurnDone reproduces the "extra subagent card after
// turn done" bug: a child fills its accumulator, but the orchestrator's StepDone builds a
// spawnKey that MISSES it (here the tool-use id differs), so the accumulator is never
// reconciled. Once the PARENT turn ends, the stale accumulator must NOT keep returning
// from pendingSubagentCards — otherwise the live tail renders it perpetually, below the
// committed "turn ran" line.
func TestSubagentAccumClearedOnParentTurnDone(t *testing.T) {
	t.Parallel()
	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, turn, step, "toolu_spawn"))
	m = m.ApplyEvent(childTurnStarted(sub, "map the repo"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("c-grep", "Grep", `{}`)),
		toolResult("c-grep", "hit"),
	))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}})
	// Orchestrator StepDone whose tool-use id does NOT match the spawn id → reconcile miss.
	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_OTHER", "Subagent", `{"agent":"explorer"}`)),
		toolResult("toolu_OTHER", "done"),
	))
	if got := len(m.pendingSubagentCardsFor(primary)); got == 0 {
		t.Fatalf("precondition: expected an unreconciled pending card before turn end, got 0")
	}
	// Parent turn ends: the stale accumulator must be cleared.
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary, TurnID: turn}}})
	if got := len(m.pendingSubagentCardsFor(primary)); got != 0 {
		t.Errorf("pending cards after parent TurnDone = %d, want 0 (stale accumulator must not linger)", got)
	}
}

// TestSubagentAccumClearedDespiteTurnMismatch reproduces the REAL-WORLD failure: the
// accumulator's spawn coordinates (from the child's Cause) do NOT match the parent's own
// turn id — the same divergence that defeats reconciliation. Cleanup must still release
// the stale accumulator by parent LOOP (the one coordinate that provably matches, since
// pendingSubagentCards found it under the parent loop), so the card cannot linger after the
// parent turn ends.
func TestSubagentAccumClearedDespiteTurnMismatch(t *testing.T) {
	t.Parallel()
	primary := callID(0xA1)
	sub := callID(0xB2)
	spawnTurn := callID(0xC3) // turn recorded in the accumulator's spawnKey
	step := callID(0xD4)
	doneTurn := callID(0xEE) // DIFFERENT turn id the parent's TurnDone reports

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, spawnTurn, step, "toolu_spawn"))
	m = m.ApplyEvent(childTurnStarted(sub, "map the repo"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("c-grep", "Grep", `{}`)),
		toolResult("c-grep", "hit"),
	))
	if got := len(m.pendingSubagentCardsFor(primary)); got == 0 {
		t.Fatalf("precondition: expected a pending card, got 0")
	}
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary, TurnID: doneTurn}}})
	if got := len(m.pendingSubagentCardsFor(primary)); got != 0 {
		t.Errorf("pending cards after parent TurnDone (turn-id mismatch) = %d, want 0 (must clear by parent loop)", got)
	}
}

// TestSubagentReconcilesUniqueToolUseIDAcrossCoordinateMismatch reproduces the duplicate
// render from the live TUI: LoopStarted records stale/different parent turn+step coordinates,
// while the parent's committed StepDone carries the same durable provider tool-use id. The
// unique pending child card must replace the raw Subagent tool call at that committed position,
// leaving no second pending card below it.
func TestSubagentReconcilesUniqueToolUseIDAcrossCoordinateMismatch(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	spawnTurn := callID(0xC3)
	spawnStep := callID(0xD4)
	commitTurn := callID(0xE5)
	commitStep := callID(0xF6)
	const toolUseID = "toolu_same"

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "operator", primary, spawnTurn, spawnStep, toolUseID))
	m = m.ApplyEvent(childTurnStarted(sub, "inspect the repository"))
	m = m.ApplyEvent(stepDoneFrom(sub, aiMessage("", "found the version")))
	m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}})
	m = m.ApplyEvent(orchestratorStepDone(primary, commitTurn, commitStep,
		aiMessage("", "", toolUse(toolUseID, "Subagent", `{"agent":"operator"}`)),
		toolResult(toolUseID, "v0.6.1"),
	))

	card := findSubagentCard(t, m)
	if card.Agent != "operator" || card.Task != "inspect the repository" || card.SubStatus != subDone {
		t.Errorf("reconciled card = %+v, want operator task with done status", card)
	}
	if got := len(m.pendingSubagentCardsFor(primary)); got != 0 {
		t.Errorf("pending cards after fallback reconciliation = %d, want 0 (single in-place render)", got)
	}
}

// TestSubagentCoordinateFallbackFailsClosedWhenAmbiguous pins the safety boundary for the
// looser correlation: duplicate provider ids under one parent are malformed, so reconciliation
// must not guess which child owns the committed call.
func TestSubagentCoordinateFallbackFailsClosedWhenAmbiguous(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	const toolUseID = "toolu_duplicate"
	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(callID(0xB1), "one", primary, callID(0xC1), callID(0xD1), toolUseID))
	m = m.ApplyEvent(childLoopStarted(callID(0xB2), "two", primary, callID(0xC2), callID(0xD2), toolUseID))
	m = m.ApplyEvent(orchestratorStepDone(primary, callID(0xE5), callID(0xF6),
		aiMessage("", "", toolUse(toolUseID, "Subagent", `{}`)),
		toolResult(toolUseID, "ambiguous"),
	))

	for _, e := range m.testCommitted() {
		for _, card := range e.Calls {
			if card.ToolName == subagentToolName && card.Agent != "" {
				t.Fatalf("ambiguous fallback guessed a child: %+v", card)
			}
		}
	}
	if got := len(m.pendingSubagentCardsFor(primary)); got != 2 {
		t.Errorf("pending cards after ambiguous fallback = %d, want 2 unchanged", got)
	}
}

// TestLateChildTerminalDoesNotResurrectClearedSubagent reproduces the all-loop fan-in
// ordering that made a completed Subagent card reappear below the parent's "turn ran"
// notice: the parent terminal clears the pending accumulator, then an already-published
// child terminal is delivered late. Once the parent has retired that spawn, no child
// terminal kind may recreate its accumulator.
func TestLateChildTerminalDoesNotResurrectClearedSubagent(t *testing.T) {
	t.Parallel()
	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	terminals := []struct {
		name string
		ev   event.Event
	}{
		{name: "done", ev: event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}}},
		{name: "failed", ev: event.TurnFailed{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}}},
		{name: "interrupted", ev: event.TurnInterrupted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: sub}}}},
	}

	for _, tt := range terminals {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := transcriptModel{}
			m = m.ApplyEvent(childLoopStarted(sub, "operator", primary, turn, step, "toolu_spawn"))
			m = m.ApplyEvent(childTurnStarted(sub, "inspect the repository"))
			if got := len(m.pendingSubagentCardsFor(primary)); got != 1 {
				t.Fatalf("pending cards before parent terminal = %d, want 1", got)
			}

			m = m.ApplyEvent(event.TurnDone{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary, TurnID: turn}}})
			if got := len(m.pendingSubagentCardsFor(primary)); got != 0 {
				t.Fatalf("pending cards after parent terminal = %d, want 0", got)
			}

			m = m.ApplyEvent(tt.ev)
			if got := len(m.pendingSubagentCardsFor(primary)); got != 0 {
				t.Errorf("pending cards after late child %s = %d, want 0 (retired spawn must stay retired)", tt.name, got)
			}
		})
	}
}

// TestSubagentAccumClearedOnParentInterrupt: a child fills its accumulator, then the
// PARENT turn is interrupted before the orchestrator reconciles it. The stale accumulator
// must be cleared so it does not leak into the next turn as a lingering pending card.
func TestSubagentAccumClearedOnParentInterrupt(t *testing.T) {
	t.Parallel()
	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, turn, step, "toolu_spawn"))
	m = m.ApplyEvent(childTurnStarted(sub, "map the repo"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("c-grep", "Grep", `{}`)),
		toolResult("c-grep", "hit"),
	))
	if got := len(m.pendingSubagentCardsFor(primary)); got == 0 {
		t.Fatalf("precondition: expected a pending card while the child streams, got 0")
	}
	// Parent turn interrupted before any orchestrator StepDone reconciles the child.
	m = m.ApplyEvent(event.TurnInterrupted{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary, TurnID: turn}}})
	if got := len(m.pendingSubagentCardsFor(primary)); got != 0 {
		t.Errorf("pending cards after parent TurnInterrupted = %d, want 0 (stale accumulator must not leak)", got)
	}
}

// TestPendingSubagentCards (live-tail card path): WHILE a subagent streams — its
// accumulator filled by ENDURING child events but the orchestrator's StepDone NOT yet
// seen — pendingSubagentCards exposes the in-flight card so the live tail can render the
// SAME nested card it will later commit. Once the orchestrator's StepDone reconciles the
// accumulator into the committed card, the accumulator is marked reconciled and drops out
// of pendingSubagentCards (it moved to scrollback — no double render).
func TestPendingSubagentCards(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", primary, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "map repo"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "", toolUse("c-grep", "Grep", `{"q":"foo"}`)),
		toolResult("c-grep", "grep hit"),
	))

	// In-flight: the orchestrator's StepDone has NOT arrived, so the card is pending.
	pending := m.pendingSubagentCards()
	if len(pending) != 1 {
		t.Fatalf("pendingSubagentCards() = %d, want 1 in-flight card", len(pending))
	}
	c := pending[0]
	if c.ToolName != startAgentToolName {
		t.Errorf("pending card.ToolName = %q, want %q", c.ToolName, startAgentToolName)
	}
	if c.Agent != "explorer" {
		t.Errorf("pending card.Agent = %q, want explorer", c.Agent)
	}
	if c.Task != "map repo" {
		t.Errorf("pending card.Task = %q, want %q", c.Task, "map repo")
	}
	if len(c.Children) != 1 {
		t.Fatalf("pending card.Children = %d, want 1 (Grep); %+v", len(c.Children), c.Children)
	}
	if c.Children[0].ToolName != "Grep" {
		t.Errorf("pending child.ToolName = %q, want Grep", c.Children[0].ToolName)
	}
	if c.Steps != 1 {
		t.Errorf("pending card.Steps = %d, want 1", c.Steps)
	}
	if c.SubStatus != subRunning {
		t.Errorf("pending card.SubStatus = %v, want subRunning", c.SubStatus)
	}

	// Reconcile: the orchestrator's StepDone moves the card to the committed transcript and
	// marks the accumulator reconciled — it drops out of the pending set.
	m = m.ApplyEvent(orchestratorStepDone(primary, turn, step,
		aiMessage("", "", toolUse("toolu_X", "Subagent", `{"agent":"explorer","message":"map repo"}`)),
		toolResult("toolu_X", "result text"),
	))

	key := spawnKey{primary, turn, step, "toolu_X"}
	acc, ok := m.subagentAccum[key]
	if !ok {
		t.Fatalf("no accumulator for key %+v after reconcile", key)
	}
	if !acc.reconciled {
		t.Errorf("accumulator.reconciled = false after orchestrator StepDone, want true")
	}
	if got := m.pendingSubagentCards(); len(got) != 0 {
		t.Errorf("pendingSubagentCards() = %d after reconcile, want 0 (moved to committed card); %+v", len(got), got)
	}
}

// TestPendingSubagentCardsCapsChildren verifies the LIVE subagent card shows only the most
// recent liveCallCap children — a subagent that runs many tools can't grow the live tail to
// fill the screen — while the total Steps count is preserved (the full children commit at
// reconcile).
func TestPendingSubagentCardsCapsChildren(t *testing.T) {
	t.Parallel()

	key := spawnKey{toolUseID: "toolu_many"}
	children := make([]ToolCallView, 0, liveCallCap+4)
	for i := 0; i < liveCallCap+4; i++ {
		children = append(children, ToolCallView{ToolName: "Bash", Status: ToolOK})
	}
	children[0].Summary = "OLDEST"
	children[len(children)-1].Summary = "NEWEST"

	m := transcriptModel{
		accumOrder: []spawnKey{key},
		subagentAccum: map[spawnKey]*subagentAccumulator{
			key: {agent: "operator", steps: 9, status: subRunning, children: children},
		},
	}

	cards := m.pendingSubagentCards()
	if len(cards) != 1 {
		t.Fatalf("pendingSubagentCards() = %d, want 1", len(cards))
	}
	if got := len(cards[0].Children); got != liveCallCap {
		t.Errorf("live card children = %d, want capped to liveCallCap (%d)", got, liveCallCap)
	}
	if cards[0].Children[len(cards[0].Children)-1].Summary != "NEWEST" {
		t.Errorf("most recent child must be kept; got %+v", cards[0].Children)
	}
	for _, c := range cards[0].Children {
		if c.Summary == "OLDEST" {
			t.Errorf("oldest child must be elided from the live card; got %+v", cards[0].Children)
		}
	}
	if cards[0].Steps != 9 {
		t.Errorf("Steps total must be preserved; got %d, want 9", cards[0].Steps)
	}
}

// TestProjectionForInitialLoopUsesUniformStorage proves the initial loop is stored and
// rendered through the same per-loop projection map as every later loop.
func TestProjectionForInitialLoopUsesUniformStorage(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	m := transcriptModel{}
	m = m.ApplyEvent(event.TurnStarted{
		Header:  event.Header{Coordinates: identity.Coordinates{LoopID: primary}, Cause: identity.Cause{CommandID: callID(1)}},
		Message: userMsg("hello"),
	})
	m = m.ApplyEvent(event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}}, Chunk: &content.TextChunk{Text: "streaming so far"}})

	committed, live := m.projectionFor(primary)
	if len(committed) != 1 || committed[0].Kind != kindUser {
		t.Fatalf("initial projection committed = %+v, want one user row", committed)
	}
	if blockText(committed[0].Blocks[0]) != "hello" {
		t.Errorf("row text = %q, want hello", blockText(committed[0].Blocks[0]))
	}
	if live.Text != "streaming so far" {
		t.Errorf("live.Text = %q, want streaming so far", live.Text)
	}
	if _, ok := m.projections[primary]; !ok {
		t.Error("initial loop missing from uniform projections")
	}
}

func TestUniformProjectionsIsolateLiveToolsAndGates(t *testing.T) {
	t.Parallel()

	initial := callID(0xA1)
	delegate := callID(0xB2)
	initialCall := callID(0x11)
	delegateCall := callID(0x22)
	m := transcriptModel{}
	m = m.ApplyEvent(event.TurnStarted{Header: hdr(initial), Message: userMsg("initial task")})
	m = m.ApplyEvent(event.TurnStarted{Header: hdr(delegate), Message: userMsg("delegate task")})
	m = m.ApplyEvent(loopTextChunk(initial, "initial text"))
	m = m.ApplyEvent(loopTextChunk(delegate, "delegate text"))
	m = m.ApplyEvent(loopToolStarted(initial, initialCall, "Read", "initial read"))
	m = m.ApplyEvent(loopToolStarted(delegate, delegateCall, "Bash", "delegate command"))
	m = m.ApplyEvent(event.PermissionRequested{Header: hdr(delegate), ToolExecutionID: delegateCall, Request: bashPermission("pwd")})
	m = m.ResolveGate(delegate, delegateCall, gateApproved)

	_, initialLive := m.projectionFor(initial)
	_, delegateLive := m.projectionFor(delegate)
	if initialLive.Text != "initial text" || len(initialLive.Calls) != 1 || initialLive.Calls[0].ToolExecutionID != initialCall {
		t.Fatalf("initial live projection = %+v, want only initial text/tool", initialLive)
	}
	if delegateLive.Text != "delegate text" || len(delegateLive.Calls) != 1 || delegateLive.Calls[0].ToolExecutionID != delegateCall {
		t.Fatalf("delegate live projection = %+v, want only delegate text/tool", delegateLive)
	}
	if _, leaked := initialLive.gateDecisions[delegateCall]; leaked {
		t.Fatal("delegate gate leaked into initial projection")
	}
	if got := delegateLive.gateDecisions[delegateCall]; got != gateApproved {
		t.Fatalf("delegate gate decision = %v, want approved", got)
	}
}

func TestDelegateWithoutToolParentUsesUniformProjection(t *testing.T) {
	t.Parallel()

	primer := callID(0xA1)
	delegate := callID(0xB2)
	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(delegate, "reviewer", primer, callID(0xC3), callID(0xD4), ""))
	m = m.ApplyEvent(childTurnStarted(delegate, "review this"))
	m = m.ApplyEvent(stepDoneFrom(delegate, aiMessage("", "review complete", toolUse("r", "Read", `{}`)), toolResult("r", "ok")))
	m = m.ApplyEvent(event.UserInputRequested{Header: hdr(delegate), Question: "Proceed?", Choices: []string{"yes", "no"}})

	committed, _ := m.projectionFor(delegate)
	if len(committed) != 4 {
		t.Fatalf("delegate projection entries = %d, want task, assistant, tool, prompt: %+v", len(committed), committed)
	}
	wantKinds := []entryKind{kindUser, kindAssistant, kindTool, kindPromptRecord}
	for i, want := range wantKinds {
		if committed[i].Kind != want {
			t.Errorf("entry %d kind = %v, want %v", i, committed[i].Kind, want)
		}
	}
	if _, recordedParent := m.loopParent[delegate]; recordedParent {
		t.Fatal("empty ParentToolUseID unexpectedly recorded a tool parent")
	}
}

// TestProjectionDelegateBuildsOwnStream locks that a non-active loop's events build that
// loop's own projection (task row → assistant prose → tool card) without creating another
// loop's projection.
func TestProjectionDelegateBuildsOwnStream(t *testing.T) {
	t.Parallel()

	initial := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", initial, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "map repo"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "looking around", toolUse("g", "Grep", `{"q":"x"}`)),
		toolResult("g", "child grep hit"),
	))

	// The subagent's OWN stream lives in ITS projection: task row, assistant prose, tool.
	pc, _ := m.projectionFor(sub)
	if len(pc) != 3 {
		t.Fatalf("projectionFor(sub) committed = %d, want 3 (task row, assistant, tool); %+v", len(pc), pc)
	}
	if pc[0].Kind != kindUser || blockText(pc[0].Blocks[0]) != "map repo" {
		t.Errorf("projection[0] = %+v, want the subagent's own task user row %q", pc[0], "map repo")
	}
	if pc[1].Kind != kindAssistant || assistantText(pc[1].Blocks) != "looking around" {
		t.Errorf("projection[1] = %+v, want the subagent assistant prose", pc[1])
	}
	if pc[2].Kind != kindTool || pc[2].Calls[0].ToolName != "Grep" {
		t.Errorf("projection[2] = %+v, want the subagent Grep tool card", pc[2])
	}
	if got := strings.Join(pc[2].Calls[0].Result, "\n"); got != "child grep hit" {
		t.Errorf("projection tool result = %q, want the child's %q", got, "child grep hit")
	}
	if _, ok := m.projections[initial]; ok {
		t.Fatal("delegate events unexpectedly created an initial-loop projection")
	}
}

// TestProjectionZeroLoopIDUsesUniformKey locks legacy zero-LoopID events to the same
// projection model as every non-zero loop ID.
func TestProjectionZeroLoopIDUsesUniformKey(t *testing.T) {
	t.Parallel()

	m := transcriptModel{}

	m = m.ApplyEvent(event.SessionIdle{})
	m = m.ApplyEvent(event.TurnStarted{Message: userMsg("zero-loop turn")}) // zero LoopID
	m = m.ApplyEvent(textChunk("zero-loop chunk"))
	m = m.ApplyEvent(stepDone(aiMessage("", "zero-loop answer")))
	m = m.ApplyEvent(event.TurnDone{})

	if len(m.projections) != 1 {
		t.Errorf("len(m.projections) = %d, want one uniform zero-key projection", len(m.projections))
	}
}

// TestProjectionGloballyUniqueIDs locks the single-nextID allocator: every entry across
// the initial-loop and subagent projections draws from the one m.nextID, so no two
// entries share a displayID (a later collapse map keyed by displayID cannot collide).
func TestProjectionGloballyUniqueIDs(t *testing.T) {
	t.Parallel()

	initial := callID(0xA1)
	sub := callID(0xB2)
	turn := callID(0xC3)
	step := callID(0xD4)

	m := transcriptModel{}
	// Initial-loop stream: a genuine user row + a finalized assistant step.
	m = m.ApplyEvent(event.TurnStarted{
		Header:  event.Header{Coordinates: identity.Coordinates{LoopID: initial}, Cause: identity.Cause{CommandID: callID(1)}},
		Message: userMsg("initial question"),
	})
	m = m.ApplyEvent(stepDoneFrom(initial, aiMessage("", "initial answer")))
	// Subagent stream folded into its own projection.
	m = m.ApplyEvent(childLoopStarted(sub, "explorer", initial, turn, step, "toolu_X"))
	m = m.ApplyEvent(childTurnStarted(sub, "sub task"))
	m = m.ApplyEvent(stepDoneFrom(sub,
		aiMessage("", "sub work", toolUse("g", "Grep", `{}`)),
		toolResult("g", "hit"),
	))

	initialEntries, _ := m.projectionFor(initial)
	pc, _ := m.projectionFor(sub)
	if len(initialEntries) == 0 || len(pc) == 0 {
		t.Fatalf("initial=%d, delegate=%d; want both non-empty so uniqueness is meaningful", len(initialEntries), len(pc))
	}
	seen := make(map[displayID]bool)
	for _, group := range [][]entry{initialEntries, pc} {
		for _, e := range group {
			if e.ID == 0 {
				t.Errorf("entry has a zero ID: %+v", e)
			}
			if seen[e.ID] {
				t.Errorf("displayID %d collides across loop projections (single nextID violated)", e.ID)
			}
			seen[e.ID] = true
		}
	}
}

// TestTranscriptLoopLiveness covers the bi-state (live | idle) liveness the loop table
// tracks: LoopStarted / TurnStarted mark a loop live, LoopIdle marks it idle, the state
// toggles freely (no "done"/"exited"), and an unseen loop is absent from loops().
func TestTranscriptLoopLiveness(t *testing.T) {
	t.Parallel()

	a := callID(0xB1)
	b := callID(0xB2)

	tests := []struct {
		name     string
		events   []event.Event
		loopID   uuid.UUID
		wantSeen bool
		wantLive bool
	}{
		{
			name:     "LoopStarted marks the loop live",
			events:   []event.Event{loopStarted(a, "explorer")},
			loopID:   a,
			wantSeen: true,
			wantLive: true,
		},
		{
			name:     "LoopIdle marks the loop idle",
			events:   []event.Event{loopStarted(a, "explorer"), event.LoopIdle{Header: loopHdr(a)}},
			loopID:   a,
			wantSeen: true,
			wantLive: false,
		},
		{
			name:     "TurnStarted marks the loop live (activity implies live)",
			events:   []event.Event{loopStarted(a, "explorer"), event.LoopIdle{Header: loopHdr(a)}, event.TurnStarted{Header: loopHdr(a)}},
			loopID:   a,
			wantSeen: true,
			wantLive: true,
		},
		{
			name: "bi-state: idle again after re-live (no done state)",
			events: []event.Event{
				loopStarted(a, "explorer"),
				event.TurnStarted{Header: loopHdr(a)},
				event.LoopIdle{Header: loopHdr(a)},
			},
			loopID:   a,
			wantSeen: true,
			wantLive: false,
		},
		{
			name:     "TurnStarted alone (no LoopStarted) still seeds the loop live",
			events:   []event.Event{event.TurnStarted{Header: loopHdr(b)}},
			loopID:   b,
			wantSeen: true,
			wantLive: true,
		},
		{
			name:     "unseen loop is absent",
			events:   nil,
			loopID:   a,
			wantSeen: false,
			wantLive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := transcriptModel{}
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			li, ok := findLoop(m.loops(), tt.loopID)
			if ok != tt.wantSeen {
				t.Fatalf("loop %v seen = %v, want %v (loops=%+v)", tt.loopID, ok, tt.wantSeen, m.loops())
			}
			if tt.wantSeen && li.Live != tt.wantLive {
				t.Errorf("loop %v Live = %v, want %v", tt.loopID, li.Live, tt.wantLive)
			}
		})
	}
}

// TestTranscriptLoopsOrderAndNames covers the ordered accessor: loops() lists loops in stable
// creation order (later activity never reorders), resolves each Name via agentLabel (agent
// name, else loopID short form for an empty/unknown name), and reports each loop's liveness.
func TestTranscriptLoopsOrderAndNames(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	l1 := callID(0x11)
	l2 := callID(0x22)
	l3 := callID(0x33)

	m := transcriptModel{}
	m = m.ApplyEvent(loopStarted(primary, identity.AgentName("orchestrator")))
	m = m.ApplyEvent(loopStarted(l1, identity.AgentName("explorer")))
	m = m.ApplyEvent(loopStarted(l2, identity.AgentName(""))) // empty name → short-form fallback
	// Re-activity on already-seen loops must NOT reorder the table.
	m = m.ApplyEvent(event.TurnStarted{Header: loopHdr(primary)})
	m = m.ApplyEvent(event.LoopIdle{Header: loopHdr(l1)})
	m = m.ApplyEvent(loopStarted(l3, identity.AgentName("tester")))

	got := m.loops()
	want := []loopInfo{
		{ID: primary, Name: "orchestrator", Live: true}, // TurnStarted → live
		{ID: l1, Name: "explorer", Live: false},         // LoopIdle → idle
		{ID: l2, Name: loopShortForm(l2), Live: true},   // empty name → short form; LoopStarted → live
		{ID: l3, Name: "tester", Live: true},            // LoopStarted → live
	}
	if len(got) != len(want) {
		t.Fatalf("loops() len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("loops()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestTranscriptLoopLivenessCloneOnWrite proves the loop table honors the value-copy
// contract: applying a lifecycle event to a model copy never mutates the prior model's
// loopLive map or loopOrder slice.
func TestTranscriptLoopLivenessCloneOnWrite(t *testing.T) {
	t.Parallel()

	l1 := callID(0x11)
	l2 := callID(0x22)

	base := transcriptModel{}
	base = base.ApplyEvent(loopStarted(l1, identity.AgentName("explorer")))

	// A copy that appends a new loop must not grow the base's order.
	grown := base.ApplyEvent(loopStarted(l2, identity.AgentName("builder")))
	if got := len(base.loops()); got != 1 {
		t.Errorf("base loops after applying to a copy = %d, want 1 (order slice aliased across copies)", got)
	}
	if got := len(grown.loops()); got != 2 {
		t.Errorf("grown loops = %d, want 2", got)
	}

	// A copy that toggles l1 idle must not flip the base's live bit.
	idled := base.ApplyEvent(event.LoopIdle{Header: loopHdr(l1)})
	if li, _ := findLoop(base.loops(), l1); !li.Live {
		t.Error("base l1 liveness flipped to idle by a copy (loopLive map aliased across copies)")
	}
	if li, _ := findLoop(idled.loops(), l1); li.Live {
		t.Error("idled l1 Live = true, want false")
	}
}

// TestTranscriptLoopLivenessFoldDeterministic guards the restore-equivalence path: because
// LoopStarted / TurnStarted / LoopIdle are all Enduring, two folds of the same lifecycle
// sequence produce byte-identical transcript state (the new loopLive map + loopOrder slice
// fold identically), so a restored session still EqualTranscript the live one.
func TestTranscriptLoopLivenessFoldDeterministic(t *testing.T) {
	t.Parallel()

	primary := callID(0xA1)
	l1 := callID(0x11)
	l2 := callID(0x22)

	events := []event.Event{
		loopStarted(primary, identity.AgentName("orchestrator")),
		loopStarted(l1, identity.AgentName("explorer")),
		event.TurnStarted{Header: loopHdr(l1)},
		event.LoopIdle{Header: loopHdr(l1)},
		loopStarted(l2, identity.AgentName("builder")),
	}
	a := FoldDisplay(events)
	b := FoldDisplay(events)
	if !a.EqualTranscript(b) {
		t.Error("EqualTranscript on two folds of the same lifecycle sequence = false, want true (liveness must fold deterministically)")
	}
}

// firstThinkingAssistant returns the thinkDur of the FIRST committed kindAssistant entry
// that carries a ThinkingBlock, and whether such an entry exists — the seam the
// thinking-duration tests assert the captured span through.
func firstThinkingAssistant(m transcriptModel) (time.Duration, bool) {
	for _, e := range m.testCommitted() {
		if e.Kind != kindAssistant {
			continue
		}
		for _, b := range e.Blocks {
			if _, ok := b.(*content.ThinkingBlock); ok {
				return e.thinkDur, true
			}
		}
	}
	return 0, false
}

// TestThinkingDurationCapture covers measuring a step's thinking span from streaming
// TokenDelta timestamps (ev.CreatedAt) and attaching it to the committed assistant entry:
// thinking-then-text seals the end at the FIRST text chunk after thinking; thinking-then-
// tool (no narration streamed) falls back to the LAST thinking chunk; a step with no
// thinking carries no duration; and a stream with no timestamps (a cold restore / backlog)
// yields a zero duration (the bare "thought" fallback).
func TestThinkingDurationCapture(t *testing.T) {
	t.Parallel()

	primary := callID(0x51)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// thinkAt / textAt build a TokenDelta stamped at base+offset on the active loop.
	thinkAt := func(s string, off time.Duration) event.Event {
		return event.TokenDelta{
			Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}, CreatedAt: base.Add(off)},
			Chunk:  &content.ThinkingChunk{Thinking: s},
		}
	}
	textAt := func(s string, off time.Duration) event.Event {
		return event.TokenDelta{
			Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}, CreatedAt: base.Add(off)},
			Chunk:  &content.TextChunk{Text: s},
		}
	}
	// thinkNoStamp builds a thinking TokenDelta with NO CreatedAt (the Ephemeral,
	// journal-less shape a cold restore never sees at all — used here to prove a
	// timestamp-less stream captures no duration).
	thinkNoStamp := func(s string) event.Event {
		return event.TokenDelta{
			Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}},
			Chunk:  &content.ThinkingChunk{Thinking: s},
		}
	}

	tests := []struct {
		name         string
		events       []event.Event
		wantThinking bool          // a committed assistant entry carries a ThinkingBlock
		wantDur      time.Duration // its captured thinkDur
	}{
		{
			// Thinking streams from +0 to +8s, then narration begins at +10s: the end is
			// SEALED at the first text chunk (+10s), so the span is 10s.
			name: "thinking then text seals end at first text chunk",
			events: []event.Event{
				event.TurnStarted{Header: hdr(primary)},
				thinkAt("weigh ", 0),
				thinkAt("options", 8*time.Second),
				textAt("here is ", 10*time.Second),
				textAt("the plan", 11*time.Second),
				stepDoneFrom(primary, aiMessage("weigh options", "here is the plan")),
			},
			wantThinking: true,
			wantDur:      10 * time.Second,
		},
		{
			// Thinking streams from +0 to +10s and NO narration follows (the step runs a
			// tool): the end falls back to the LAST thinking chunk (+10s), a 10s span.
			name: "thinking then tool falls back to last thinking chunk",
			events: []event.Event{
				event.TurnStarted{Header: hdr(primary)},
				thinkAt("mulling ", 0),
				thinkAt("it over", 10*time.Second),
				stepDoneFrom(primary, aiMessage("mulling it over", "", toolUse("t1", "Bash", `{"command":"ls"}`)), toolResult("t1", "ok")),
			},
			wantThinking: true,
			wantDur:      10 * time.Second,
		},
		{
			// A step that streamed only narration has no thinking block committed, so there
			// is no thinking entry to carry a duration.
			name: "no thinking step carries no duration entry",
			events: []event.Event{
				event.TurnStarted{Header: hdr(primary)},
				textAt("just answering", 0),
				stepDoneFrom(primary, aiMessage("", "just answering")),
			},
			wantThinking: false,
		},
		{
			// Timestamp-less thinking chunks (the restore/backlog shape) never seed a start,
			// so the committed entry has a zero duration → the bare "thought" fallback.
			name: "timestampless thinking yields zero duration",
			events: []event.Event{
				event.TurnStarted{Header: hdr(primary)},
				thinkNoStamp("reasoning without stamps"),
				stepDoneFrom(primary, aiMessage("reasoning without stamps", "done")),
			},
			wantThinking: true,
			wantDur:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := transcriptModel{}
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			gotDur, gotThinking := firstThinkingAssistant(m)
			if gotThinking != tt.wantThinking {
				t.Fatalf("committed thinking entry present = %v, want %v (committed=%+v)", gotThinking, tt.wantThinking, m.testCommitted())
			}
			if tt.wantThinking && gotDur != tt.wantDur {
				t.Errorf("captured thinkDur = %v, want %v", gotDur, tt.wantDur)
			}
		})
	}
}

// TestInterruptedProseCarriesThinkDuration covers the provisional-prose path
// (commitProse): a turn INTERRUPTED mid-step — after thinking streamed but before its
// StepDone — still commits the real "thought for Nsec" it spent, because the live timing is
// intact when the interrupt flushes the provisional prose. It matches a completed step's
// affordance rather than degrading to the bare "thought".
func TestInterruptedProseCarriesThinkDuration(t *testing.T) {
	t.Parallel()

	primary := callID(0x61)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	thinkAt := func(s string, off time.Duration) event.Event {
		return event.TokenDelta{
			Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}, CreatedAt: base.Add(off)},
			Chunk:  &content.ThinkingChunk{Thinking: s},
		}
	}

	m := transcriptModel{}
	// Thinking streams from +0 to +10s, then the turn is interrupted BEFORE any StepDone:
	// turnInterrupted → commitProse flushes the provisional thinking with its measured span.
	for _, ev := range []event.Event{
		event.TurnStarted{Header: hdr(primary)},
		thinkAt("half a ", 0),
		thinkAt("thought", 10*time.Second),
		event.TurnInterrupted{Header: hdr(primary)},
	} {
		m = m.ApplyEvent(ev)
	}

	gotDur, gotThinking := firstThinkingAssistant(m)
	if !gotThinking {
		t.Fatalf("interrupt path committed no thinking entry; committed=%+v", m.testCommitted())
	}
	if gotDur != 10*time.Second {
		t.Errorf("interrupted prose thinkDur = %v, want 10s (the real span it spent)", gotDur)
	}
}

// TestEqualTranscriptIgnoresThinkDuration is the restore-equivalence guard: a LIVE fold
// (with streaming TokenDelta timestamps) captures a non-zero thinking duration, while a
// RESTORE fold of the SAME step WITHOUT those Ephemeral, never-journaled deltas captures
// zero — yet the two displayed transcripts must still compare EQUAL, because the duration
// is a live-display enhancement normalized out of EqualTranscript. The restored row
// correctly shows "│ thought" with no number; that is the accepted behavior, not a bug.
func TestEqualTranscriptIgnoresThinkDuration(t *testing.T) {
	t.Parallel()

	primary := callID(0x71)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step := stepDoneFrom(primary, aiMessage("thinking hard", "the answer"))

	// LIVE: the same finalized step, PRECEDED by streaming thinking TokenDeltas carrying
	// real timestamps — a 10s thinking span the live fold captures.
	liveEvents := []event.Event{
		event.TurnStarted{Header: hdr(primary)},
		event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}, CreatedAt: base}, Chunk: &content.ThinkingChunk{Thinking: "thinking "}},
		event.TokenDelta{Header: event.Header{Coordinates: identity.Coordinates{LoopID: primary}, CreatedAt: base.Add(10 * time.Second)}, Chunk: &content.ThinkingChunk{Thinking: "hard"}},
		step,
		event.TurnDone{Header: hdr(primary)},
	}
	// RESTORE: only the persisted Enduring events (no Ephemeral TokenDeltas) — the shape a
	// cold restore's ReplayBacklog yields.
	restoreEvents := []event.Event{
		event.TurnStarted{Header: hdr(primary)},
		step,
		event.TurnDone{Header: hdr(primary)},
	}

	live := FoldDisplay(liveEvents)
	restored := FoldDisplay(restoreEvents)

	// Prove the durations genuinely DIVERGE (otherwise the equality would be trivial).
	liveDur, ok := firstThinkingAssistant(live.transcript)
	if !ok || liveDur <= 0 {
		t.Fatalf("live fold captured no thinking duration (dur=%v, present=%v); the test cannot prove exclusion", liveDur, ok)
	}
	restoredDur, ok := firstThinkingAssistant(restored.transcript)
	if !ok || restoredDur != 0 {
		t.Fatalf("restore fold thinkDur = %v (present=%v), want 0 (no streaming timestamps)", restoredDur, ok)
	}

	// HEADLINE: despite the diverging duration, the displayed transcripts compare EQUAL.
	if !live.EqualTranscript(restored) {
		t.Errorf("EqualTranscript = false across a live/restore duration divergence, want true (duration must be excluded)")
	}
	// And a plain reflect.DeepEqual over the raw models WOULD differ — confirming the
	// exclusion is doing real work, not masking already-equal state.
	if live.EqualTranscript(restored) && liveDur == restoredDur {
		t.Fatal("durations did not diverge; exclusion is untested")
	}
}

// subTokenText builds a subagent-scoped *content.TextChunk TokenDelta.
func loopTextChunk(loopID uuid.UUID, s string) event.Event {
	return event.TokenDelta{Header: hdr(loopID), Chunk: &content.TextChunk{Text: s}}
}

// loopThinkingChunk builds a loop-scoped *content.ThinkingChunk TokenDelta.
func loopThinkingChunk(loopID uuid.UUID, s string) event.Event {
	return event.TokenDelta{Header: hdr(loopID), Chunk: &content.ThinkingChunk{Thinking: s}}
}

// loopToolStarted builds a loop-scoped ToolCallStarted.
func loopToolStarted(loopID, id uuid.UUID, name, summary string) event.Event {
	return event.ToolCallStarted{Header: hdr(loopID), ToolExecutionID: id, ToolName: name, Summary: summary}
}

// TestTranscriptProjectionFoldsEphemeral covers both non-zero and legacy zero LoopID keys.
func TestTranscriptProjectionFoldsEphemeral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		loopID uuid.UUID
	}{
		{name: "non-zero loop", loopID: callID(0xA1)},
		{name: "zero loop id", loopID: uuid.UUID{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := transcriptModel{}
			m = m.ApplyEvent(event.TurnStarted{Header: hdr(tt.loopID)})
			m = m.ApplyEvent(loopThinkingChunk(tt.loopID, "reason"))
			m = m.ApplyEvent(loopTextChunk(tt.loopID, "narration"))
			m = m.ApplyEvent(loopToolStarted(tt.loopID, callID(0x21), "Bash", "cmd"))

			if m.testLive().Text != "narration" {
				t.Errorf("live.Text = %q, want %q", m.testLive().Text, "narration")
			}
			if m.testLive().Thinking != "reason" {
				t.Errorf("live.Thinking = %q, want %q", m.testLive().Thinking, "reason")
			}
			if len(m.testLive().Calls) != 1 {
				t.Fatalf("live.Calls = %d, want 1", len(m.testLive().Calls))
			}
			if m.testLive().Calls[0].ToolName != "Bash" {
				t.Errorf("live card ToolName = %q, want Bash", m.testLive().Calls[0].ToolName)
			}
		})
	}
}

func TestTranscriptCompactionCompletion(t *testing.T) {
	t.Parallel()

	loopA := callID(0x91)
	loopB := callID(0x92)
	eventA := callID(0x93)
	eventB := callID(0x94)
	attempt := event.CompactAttemptID(callID(0x95))
	committed := func(loopID, eventID uuid.UUID, duration time.Duration) event.CompactionCommitted {
		h := hdr(loopID)
		h.EventID = eventID
		return event.CompactionCommitted{Header: h, AttemptID: attempt, Duration: duration}
	}
	rejected := func(loopID, eventID uuid.UUID) event.CompactionRejected {
		h := hdr(loopID)
		h.EventID = eventID
		return event.CompactionRejected{Header: h, AttemptID: attempt, Duration: 3 * time.Second}
	}

	tests := []struct {
		name      string
		events    []event.Event
		wantA     []string
		wantB     []string
		wantTotal int
	}{
		{
			name:      "committed event appends exact duration payload",
			events:    []event.Event{committed(loopA, eventA, 25*time.Second)},
			wantA:     []string{"conversation compacted in 25s"},
			wantTotal: 1,
		},
		{
			name:      "duplicate event id appends once",
			events:    []event.Event{committed(loopA, eventA, 25*time.Second), committed(loopA, eventA, 25*time.Second)},
			wantA:     []string{"conversation compacted in 25s"},
			wantTotal: 1,
		},
		{
			name:      "zero event id appends nothing",
			events:    []event.Event{committed(loopA, uuid.UUID{}, 25*time.Second)},
			wantTotal: 0,
		},
		{
			name:      "rejection appends no success row",
			events:    []event.Event{rejected(loopA, eventA)},
			wantTotal: 0,
		},
		{
			name:      "completion is isolated to its loop",
			events:    []event.Event{committed(loopB, eventB, 61*time.Second)},
			wantB:     []string{"conversation compacted in 1m 1s"},
			wantTotal: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := transcriptModel{}
			for _, ev := range tt.events {
				m = m.ApplyEvent(ev)
			}
			if got := harnessTexts(m, loopA); !reflect.DeepEqual(got, tt.wantA) {
				t.Errorf("loop A harness rows = %q, want %q", got, tt.wantA)
			}
			if got := harnessTexts(m, loopB); !reflect.DeepEqual(got, tt.wantB) {
				t.Errorf("loop B harness rows = %q, want %q", got, tt.wantB)
			}
			if got := m.committedLen(); got != tt.wantTotal {
				t.Errorf("committedLen = %d, want %d", got, tt.wantTotal)
			}
		})
	}
}

func TestTranscriptCompactionCompletionIgnoresRetainedHistory(t *testing.T) {
	t.Parallel()

	loopID := callID(0x96)
	sessionID := callID(0x97)
	preContext := validContextMeasurement(content.TokenCount(82), content.TokenCount(100), 1, callID(0x98), 0x82)
	postContext := validContextMeasurement(content.TokenCount(17), content.TokenCount(100), 2, callID(0x99), 0x17)
	header := hdr(loopID)
	header.Coordinates.SessionID = sessionID
	header.EventID = callID(0x9a)
	compaction := event.CompactionCommitted{
		Header:           header,
		AttemptID:        event.CompactAttemptID(callID(0x9b)),
		WaiterCommandIDs: []uuid.UUID{callID(0x9c)},
		Reason:           event.CompactionReasonManual,
		Basis:            preContext.Basis,
		Summary:          userMsg("retained summary"),
		Retained: content.AgenticMessages{
			userMsg("retained user turn"),
			aiMessage("thinking", "retained assistant", toolUse("retained-tool", "Bash", `{"command":"true"}`)),
			toolResult("retained-tool", "retained tool result"),
		},
		PostContext: postContext,
		Duration:    7 * time.Second,
	}
	if err := event.ValidateEvent(compaction); err != nil {
		t.Fatalf("retained compaction fixture is invalid: %v", err)
	}

	withRetained := FoldDisplay([]event.Event{compaction})
	summaryOnly := compaction
	summaryOnly.Retained = nil
	withoutRetained := FoldDisplay([]event.Event{summaryOnly})

	// FoldDisplay is the public live/restore projection seam. Retained is provider
	// history, not TUI scrollback: adding it must not change the displayed transcript.
	if !withRetained.EqualTranscript(withoutRetained) {
		t.Fatal("retained history changed the displayed transcript")
	}
	if got, want := withRetained.CommittedLen(), 1; got != want {
		t.Fatalf("retained compaction committed length = %d, want one completion row", got)
	}
	if got, want := harnessTexts(withRetained.transcript, loopID), []string{"conversation compacted in 7s"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("compaction harness rows = %q, want exactly %q", got, want)
	}
	if got := kindUserCount(withRetained.transcript); got != 0 {
		t.Fatalf("retained compaction added %d user rows, want none", got)
	}
}

func TestTranscriptCompactionCompletionDetachesTargetProjection(t *testing.T) {
	t.Parallel()

	existingLoop := callID(0x81)
	otherLoop := callID(0x82)
	attemptID := event.CompactAttemptID(callID(0x83))
	tests := []struct {
		name     string
		base     transcriptModel
		loopID   uuid.UUID
		eventID  uuid.UUID
		wantRows int
	}{
		{
			name: "existing target projection remains unchanged",
			base: transcriptModel{}.ApplyEvent(event.TurnStarted{
				Header:  hdr(existingLoop),
				Message: userMsg("existing row"),
			}),
			loopID:   existingLoop,
			eventID:  callID(0x84),
			wantRows: 2,
		},
		{
			name: "absent target projection is created on next value only",
			base: transcriptModel{}.ApplyEvent(event.TurnStarted{
				Header:  hdr(otherLoop),
				Message: userMsg("other loop row"),
			}),
			loopID:   existingLoop,
			eventID:  callID(0x85),
			wantRows: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			priorRows, _ := tt.base.projectionFor(tt.loopID)
			priorRows = append([]entry(nil), priorRows...)
			priorNextID := tt.base.nextID

			h := hdr(tt.loopID)
			h.EventID = tt.eventID
			next := tt.base.ApplyEvent(event.CompactionCommitted{
				Header:    h,
				AttemptID: attemptID,
				Duration:  4 * time.Second,
			})

			gotPriorRows, _ := tt.base.projectionFor(tt.loopID)
			if !reflect.DeepEqual(gotPriorRows, priorRows) {
				t.Errorf("prior projection changed: got %+v, want %+v", gotPriorRows, priorRows)
			}
			if tt.base.nextID != priorNextID {
				t.Errorf("prior nextID = %d, want unchanged %d", tt.base.nextID, priorNextID)
			}
			if _, mutated := tt.base.compactionCompletions[tt.eventID]; mutated {
				t.Errorf("prior dedup contains event %v, want unchanged", tt.eventID)
			}

			nextRows, _ := next.projectionFor(tt.loopID)
			if len(nextRows) != tt.wantRows {
				t.Fatalf("next rows = %d, want %d", len(nextRows), tt.wantRows)
			}
			if got := harnessTexts(next, tt.loopID); !reflect.DeepEqual(got, []string{"conversation compacted in 4s"}) {
				t.Errorf("next completion rows = %q, want one exact row", got)
			}
			if next.nextID != priorNextID+1 {
				t.Errorf("next nextID = %d, want %d", next.nextID, priorNextID+1)
			}
			if _, recorded := next.compactionCompletions[tt.eventID]; !recorded {
				t.Errorf("next dedup missing event %v", tt.eventID)
			}
		})
	}
}

func harnessTexts(m transcriptModel, loopID uuid.UUID) []string {
	committed, _ := m.projectionFor(loopID)
	var texts []string
	for _, entry := range committed {
		if entry.Kind == kindHarness {
			texts = append(texts, committedText(entry))
		}
	}
	return texts
}
