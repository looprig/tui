package restore

import (
	"fmt"
	"strings"

	"github.com/looprig/harness/pkg/event"
)

// severityMarker is the leading glyph that distinguishes a warn change from an
// informational one in rendered output. It is intentionally ASCII so the lines are
// safe to print to any terminal or log.
func severityMarker(s event.DriftSeverity) string {
	if s == event.DriftWarn {
		return "!"
	}
	return "-"
}

// formatChange renders one DriftChange as a single human-readable line: a severity
// marker, the category (and field, when present), and the old→new transition. It is a
// pure function — no Bubble Tea, no I/O — so it is fully unit-testable.
func formatChange(c event.DriftChange) string {
	name := string(c.Category)
	if c.Field != "" {
		name = fmt.Sprintf("%s.%s", name, c.Field)
	}

	var transition string
	switch {
	case c.Old == "" && c.New == "":
		transition = "changed"
	case c.Old == "":
		transition = fmt.Sprintf("added %q", c.New)
	case c.New == "":
		transition = fmt.Sprintf("removed %q", c.Old)
	default:
		transition = fmt.Sprintf("%q → %q", c.Old, c.New)
	}

	return fmt.Sprintf("%s %s: %s", severityMarker(c.Severity), name, transition)
}

// formatChanges renders each change as one line via formatChange, preserving order.
func formatChanges(cs []event.DriftChange) []string {
	lines := make([]string, 0, len(cs))
	for _, c := range cs {
		lines = append(lines, formatChange(c))
	}
	return lines
}

// joinChanges renders changes as a newline-joined block for direct display.
func joinChanges(cs []event.DriftChange) string {
	return strings.Join(formatChanges(cs), "\n")
}
