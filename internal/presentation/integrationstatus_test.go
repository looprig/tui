package presentation

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/looprig/harness/pkg/event"
)

// integrationStatus fabricates an IntegrationStatus for one binding.
func integrationStatus(source, name string, state event.IntegrationState, detail string) event.IntegrationStatus {
	return event.IntegrationStatus{Source: source, Name: name, State: state, Detail: detail}
}

// TestIntegrationProjectionFoldsLatestStatusPerBinding covers the fold contract:
// the newest status supersedes the previous one for the same (Source, Name), two
// bindings sharing a name in different sources stay distinct, first-seen order is
// stable, and an unrelated event is a no-op.
func TestIntegrationProjectionFoldsLatestStatusPerBinding(t *testing.T) {
	t.Parallel()

	projection := newIntegrationProjection()
	projection = projection.ApplyEvent(integrationStatus("mcp", "github", event.IntegrationStarting, ""))
	projection = projection.ApplyEvent(integrationStatus("lsp", "github", event.IntegrationFailed, "no binary"))
	// Supersedes the starting status above rather than appending beside it.
	projection = projection.ApplyEvent(integrationStatus("mcp", "github", event.IntegrationReady, ""))
	// An event this projection does not own must not disturb it.
	projection = projection.ApplyEvent(event.TurnDone{})

	if got, want := len(projection.order), 2; got != want {
		t.Fatalf("order length = %d, want %d", got, want)
	}
	if got, want := projection.order[0], (integrationKey{source: "mcp", name: "github"}); got != want {
		t.Errorf("order[0] = %+v, want %+v (first-seen order)", got, want)
	}

	mcp := projection.states[integrationKey{source: "mcp", name: "github"}]
	if got, want := mcp.state, event.IntegrationReady; got != want {
		t.Errorf("mcp:github state = %v, want %v (latest status wins)", got, want)
	}
	lsp := projection.states[integrationKey{source: "lsp", name: "github"}]
	if got, want := lsp.state, event.IntegrationFailed; got != want {
		t.Errorf("lsp:github state = %v, want %v (source namespaces the name)", got, want)
	}
	if got, want := lsp.detail, "no binary"; got != want {
		t.Errorf("lsp:github detail = %q, want %q", got, want)
	}
}

// TestIntegrationProjectionApplyEventIsCopyOnWrite proves a fold never writes
// through a map an earlier projection value still holds — the projection is
// driven by value, so a shared backing map would let one frame mutate another's
// state.
func TestIntegrationProjectionApplyEventIsCopyOnWrite(t *testing.T) {
	t.Parallel()

	before := newIntegrationProjection().ApplyEvent(integrationStatus("mcp", "github", event.IntegrationReady, ""))
	after := before.ApplyEvent(integrationStatus("mcp", "github", event.IntegrationFailed, "refused"))

	key := integrationKey{source: "mcp", name: "github"}
	if got, want := before.states[key].state, event.IntegrationReady; got != want {
		t.Errorf("prior projection state = %v, want %v (fold must not mutate a shared map)", got, want)
	}
	if got, want := after.states[key].state, event.IntegrationFailed; got != want {
		t.Errorf("folded projection state = %v, want %v", got, want)
	}
}

// TestIntegrationProjectionDropsInvalidState covers the fail-closed edge: a
// status carrying an undeclared State is dropped, leaving the last known-good
// reading rather than recording an "unknown" one.
func TestIntegrationProjectionDropsInvalidState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state event.IntegrationState
	}{
		{name: "zero value is not a state", state: 0},
		{name: "past the declared set", state: event.IntegrationClosed + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			projection := newIntegrationProjection().
				ApplyEvent(integrationStatus("mcp", "github", event.IntegrationReady, "")).
				ApplyEvent(integrationStatus("mcp", "github", tt.state, "garbage"))

			key := integrationKey{source: "mcp", name: "github"}
			if got, want := projection.states[key].state, event.IntegrationReady; got != want {
				t.Errorf("state = %v, want %v (an invalid status is dropped)", got, want)
			}
			if got, want := len(projection.order), 1; got != want {
				t.Errorf("order length = %d, want %d (an invalid status adds no entry)", got, want)
			}
		})
	}
}

// TestIntegrationStatusSegment drives the render: which states earn a place on
// the shared status row, how each is worded, and when Detail appears.
func TestIntegrationStatusSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		statuses []event.IntegrationStatus
		want     string
	}{
		{
			name: "nothing reported renders nothing",
			want: "",
		},
		{
			name:     "a ready integration costs no row space",
			statuses: []event.IntegrationStatus{integrationStatus("mcp", "github", event.IntegrationReady, "")},
			want:     "",
		},
		{
			name:     "starting is shown without detail",
			statuses: []event.IntegrationStatus{integrationStatus("mcp", "github", event.IntegrationStarting, "dialling")},
			want:     "mcp:github starting",
		},
		{
			name:     "a failed integration names its failure",
			statuses: []event.IntegrationStatus{integrationStatus("mcp", "github", event.IntegrationFailed, "handshake refused")},
			want:     "mcp:github failed — handshake refused",
		},
		{
			name:     "a degraded integration names its fault",
			statuses: []event.IntegrationStatus{integrationStatus("mcp", "linear", event.IntegrationDegraded, "reconnecting")},
			want:     "mcp:linear degraded — reconnecting",
		},
		{
			name:     "closed is terminal and still reported",
			statuses: []event.IntegrationStatus{integrationStatus("mcp", "github", event.IntegrationClosed, "shut down")},
			want:     "mcp:github closed",
		},
		{
			name: "only the integrations needing attention appear, in first-seen order",
			statuses: []event.IntegrationStatus{
				integrationStatus("mcp", "github", event.IntegrationStarting, ""),
				integrationStatus("mcp", "linear", event.IntegrationReady, ""),
				integrationStatus("mcp", "slack", event.IntegrationFailed, "no token"),
				integrationStatus("mcp", "github", event.IntegrationReady, ""),
			},
			want: "mcp:slack failed — no token",
		},
		{
			name: "several faults join on one row",
			statuses: []event.IntegrationStatus{
				integrationStatus("mcp", "github", event.IntegrationDegraded, "retrying"),
				integrationStatus("lsp", "gopls", event.IntegrationFailed, "crashed"),
			},
			want: "mcp:github degraded — retrying · lsp:gopls failed — crashed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			projection := newIntegrationProjection()
			for _, status := range tt.statuses {
				projection = projection.ApplyEvent(status)
			}
			if got := ansi.Strip(projection.statusSegment()); got != tt.want {
				t.Errorf("statusSegment() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestIntegrationStatusSegmentClipsDetail covers the boundary: Detail may be up
// to event.MaxIntegrationDetailBytes, far more than a shared row can carry, so a
// long one is clipped rather than evicting the metadata beside it.
func TestIntegrationStatusSegmentClipsDetail(t *testing.T) {
	t.Parallel()

	detail := strings.Repeat("x", event.MaxIntegrationDetailBytes)
	projection := newIntegrationProjection().
		ApplyEvent(integrationStatus("mcp", "github", event.IntegrationFailed, detail))

	got := ansi.Strip(projection.statusSegment())
	if strings.Contains(got, detail) {
		t.Fatalf("statusSegment() = %q, want the over-long detail clipped", got)
	}
	clipped := strings.Repeat("x", maxIntegrationDetailCells-1) + "…"
	if want := "mcp:github failed — " + clipped; got != want {
		t.Errorf("statusSegment() = %q, want %q", got, want)
	}
}

// TestScreenStatusLineCarriesIntegrationStatus is the end-to-end render-driving
// case: an IntegrationStatus arriving on the ordinary event stream reaches the
// status line, trailing the focused loop's own metadata, and leaves again once
// the integration recovers. It is what proves the surface is wired to the stream
// the TUI already reads rather than to a side channel.
func TestScreenStatusLineCarriesIntegrationStatus(t *testing.T) {
	t.Parallel()

	loopID := callID(0xA1)
	m := newScreenSized(t, &fakeAgent{activeLoopID: loopID}, 120, 24)

	if got := stripANSI(m.statusLine()); strings.Contains(got, "mcp:") {
		t.Fatalf("statusLine = %q, want no integration segment before any status arrives", got)
	}

	m.handleEvent(integrationStatus("mcp", "github", event.IntegrationFailed, "handshake refused"))
	got := stripANSI(m.statusLine())
	if want := "mcp:github failed — handshake refused"; !strings.Contains(got, want) {
		t.Errorf("statusLine = %q, want it to contain %q", got, want)
	}
	if !strings.HasPrefix(got, "○ idle") {
		t.Errorf("statusLine = %q, want the turn label to stay first", got)
	}

	// A self-healing status supersedes the fault: the row goes quiet again.
	m.handleEvent(integrationStatus("mcp", "github", event.IntegrationReady, ""))
	if got := stripANSI(m.statusLine()); strings.Contains(got, "mcp:") {
		t.Errorf("statusLine = %q, want the segment gone once the integration is ready", got)
	}
}

// TestScreenStatusLineIntegrationStatusIsSessionScoped pins the delivery
// contract this surface depends on: an IntegrationStatus carries no LoopID, so
// it must fold regardless of which loop the user is focused on. A projection
// that skipped zero-LoopID events (as the loop-keyed runtime projection does)
// would render nothing here.
func TestScreenStatusLineIntegrationStatusIsSessionScoped(t *testing.T) {
	t.Parallel()

	focused := callID(0xB1)
	m := newScreenSized(t, &fakeAgent{activeLoopID: focused}, 120, 24)
	m.focusedLoopID = focused

	status := integrationStatus("mcp", "github", event.IntegrationDegraded, "retrying")
	if !status.EventHeader().LoopID.IsZero() {
		t.Fatal("fixture carries a LoopID; an IntegrationStatus is session-scoped")
	}
	m.handleEvent(status)

	if got, want := stripANSI(m.statusLine()), "mcp:github degraded — retrying"; !strings.Contains(got, want) {
		t.Errorf("statusLine = %q, want it to contain %q", got, want)
	}
}

// TestIntegrationStatusSegmentStylesBySeverity proves the segment reuses the
// module's existing severity vocabulary: a failed integration must not render as
// quietly as a starting one.
func TestIntegrationStatusSegmentStylesBySeverity(t *testing.T) {
	t.Parallel()

	failed := newIntegrationProjection().
		ApplyEvent(integrationStatus("mcp", "github", event.IntegrationFailed, "")).
		statusSegment()
	starting := newIntegrationProjection().
		ApplyEvent(integrationStatus("mcp", "github", event.IntegrationStarting, "")).
		statusSegment()

	if failed == starting {
		t.Fatal("a failed integration renders identically to a starting one; want distinct severity styling")
	}
	if ansi.Strip(failed) == failed {
		t.Error("a failed integration rendered unstyled, want the error severity style")
	}
}
