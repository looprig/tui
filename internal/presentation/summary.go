package presentation

import "strings"

// toolRunSummary builds the collapsed label for a run of tool calls: "N tools · Name,
// Name ✗, Name". A failed call (tool error/cancelled, or subagent failed/interrupted) is
// marked " ✗"; successes are unmarked. A subagent call names its agent — "Subagent(agent)".
// The second return is true when ANY call failed (so the caller tints the node red).
func toolRunSummary(calls []ToolCallView) (string, bool) {
	anyFailed := false
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		name := c.ToolName
		if c.Agent != "" {
			name = c.ToolName + "(" + c.Agent + ")"
		}
		if toolCallFailed(c) {
			anyFailed = true
			name += " ✗"
		}
		names = append(names, name)
	}
	return plural(len(calls), "tool") + hintSeparator + strings.Join(names, ", "), anyFailed
}

// toolCallFailed reports whether a committed call (tool or subagent) ended in failure.
func toolCallFailed(c ToolCallView) bool {
	if c.Agent != "" {
		return c.SubStatus == subFailed || c.SubStatus == subInterrupted
	}
	return c.Status == ToolError || c.Status == ToolCancelled
}
