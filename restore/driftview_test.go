package restore

import (
	"strings"
	"testing"

	"github.com/looprig/harness/pkg/event"
)

func TestFormatChange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change event.DriftChange
		want   string
	}{
		{
			name:   "warn with old and new",
			change: event.DriftChange{Category: event.DriftWorkspace, Old: "/a", New: "/b", Severity: event.DriftWarn},
			want:   `! workspace: "/a" → "/b"`,
		},
		{
			name:   "info with category and field",
			change: event.DriftChange{Category: event.DriftTool, Field: "bash", Old: "r1", New: "r2", Severity: event.DriftInfo},
			want:   `- tool.bash: "r1" → "r2"`,
		},
		{
			name:   "added value has empty old",
			change: event.DriftChange{Category: event.DriftTool, Field: "grep", New: "r9", Severity: event.DriftInfo},
			want:   `- tool.grep: added "r9"`,
		},
		{
			name:   "removed value has empty new",
			change: event.DriftChange{Category: event.DriftTool, Field: "grep", Old: "r9", Severity: event.DriftInfo},
			want:   `- tool.grep: removed "r9"`,
		},
		{
			name:   "no old or new renders changed",
			change: event.DriftChange{Category: event.DriftPermission, Severity: event.DriftWarn},
			want:   `! permission: changed`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatChange(tt.change); got != tt.want {
				t.Errorf("formatChange() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatChangesPreservesOrder(t *testing.T) {
	t.Parallel()
	got := formatChanges([]event.DriftChange{
		{Category: event.DriftModel, Old: "m1", New: "m2", Severity: event.DriftInfo},
		{Category: event.DriftWorkspace, Old: "/a", New: "/b", Severity: event.DriftWarn},
	})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if !strings.HasPrefix(got[0], "- model:") || !strings.HasPrefix(got[1], "! workspace:") {
		t.Errorf("order not preserved: %#v", got)
	}
}
