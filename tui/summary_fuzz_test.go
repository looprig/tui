package tui

import (
	"strings"
	"testing"
)

// FuzzToolRunSummary exercises the collapsed tool-run summary builder for robustness:
// arbitrary tool names, agent names, call counts, and status selectors must never panic
// and must always yield a "<n> tool(s) · …" label whose anyFailed flag agrees with the
// presence of a failed call. It builds the []ToolCallView from fuzzed primitives (the
// fuzzer can't synthesize a slice directly).
func FuzzToolRunSummary(f *testing.F) {
	f.Add("Read", "", uint8(0), uint8(1))
	f.Add("Bash", "", uint8(2), uint8(3))
	f.Add("Subagent", "explore", uint8(0), uint8(2))
	f.Add("", "", uint8(255), uint8(0))
	f.Add(strings.Repeat("x", 4096), "agent\x00name", uint8(1), uint8(7))

	f.Fuzz(func(t *testing.T, name, agent string, statusSel, count uint8) {
		n := int(count)%8 + 1 // 1..8 calls
		calls := make([]ToolCallView, n)
		for i := range calls {
			c := ToolCallView{ToolName: name}
			// Vary each call's status/agent by index so a run mixes ok/failed/subagent.
			switch (int(statusSel) + i) % 5 {
			case 0:
				c.Status = ToolOK
			case 1:
				c.Status = ToolError
			case 2:
				c.Status = ToolCancelled
			case 3:
				c.Agent = agent
				c.SubStatus = subDone
			case 4:
				c.Agent = agent
				c.SubStatus = subFailed
			}
			calls[i] = c
		}

		text, anyFailed := toolRunSummary(calls)
		if text == "" {
			t.Fatalf("toolRunSummary returned empty text for %d calls", n)
		}
		if !strings.Contains(text, "tool") {
			t.Fatalf("summary %q missing the tool count", text)
		}
		// anyFailed must agree with toolCallFailed over the run.
		wantFailed := false
		for _, c := range calls {
			if toolCallFailed(c) {
				wantFailed = true
				break
			}
		}
		if anyFailed != wantFailed {
			t.Fatalf("anyFailed = %v, want %v for calls %+v", anyFailed, wantFailed, calls)
		}
	})
}
