package components

import (
	"strings"
	"testing"

	"github.com/looprig/tui/styles"
)

// selectedBandOpen is the SGR sequence that opens the shared selection band.
//
// It is read back off a real banded row instead of being derived from a named color,
// because the fill is styles' private business: a test that spelled the shade here would
// have to be edited in lockstep with styles, which is precisely the per-surface drift
// styles.SelectedRow exists to prevent. FillLineBackgroundWith always emits the fill's
// opening escape first, and that escape is one SGR sequence, so it ends at the first "m".
func selectedBandOpen(t *testing.T) string {
	t.Helper()
	band := styles.SelectedRow("x", 1)
	end := strings.IndexByte(band, 'm')
	if !strings.HasPrefix(band, "\x1b[") || end < 0 {
		t.Fatalf("styles.SelectedRow no longer opens with an SGR sequence: %q", band)
	}
	return band[:end+1]
}

// trayGolden is the exact byte output of renderCompletionTray for the two rows and the
// width used by TestTrayRenderIsStable. It is a CHARACTERISATION golden: its job is to make
// every unintended pixel change loud, not to assert that this particular escape soup is
// beautiful. Regenerate it only alongside a deliberate, explained visual change.
//
// It has moved three times, all deliberately: the selected row went through
// styles.SelectedRow (its fill and near-black text replaced the tray's own private fill and
// the bold blue label), "/clear" gained two spaces so its description starts at the same
// column as "/compact"'s, and the selected row's RAIL is now drawn in the band's own color
// instead of the near-black the rest of that row is stripped to.
const trayGolden = "\x1b[48;2;36;37;39m\x1b[38;2;80;80;80m▌\x1b[m\x1b[48;2;36;37;39m /clear    \x1b[2mstart a new conversation\x1b[m\x1b[48;2;36;37;39m        \x1b[m\n\x1b[48;2;162;210;255m\x1b[38;2;162;210;255m▌\x1b[m\x1b[48;2;162;210;255m\x1b[38;2;16;16;16m /compact  compact the current conversation\x1b[m\x1b[48;2;162;210;255m\x1b[m"

func TestTrayRenderIsStable(t *testing.T) {
	rows := []completionTrayRow{
		{primary: "/clear", secondary: "start a new conversation"},
		{primary: "/compact", secondary: "compact the current conversation"},
	}
	got := renderCompletionTray(rows, 1, 44)
	if want := trayGolden; got != want {
		t.Errorf("tray render changed:\n got %q\nwant %q", got, want)
	}
}

// TestDescriptionsAlignToCommonColumn pins the tray's second column: every description in a
// render starts at the same screen column, so the list reads as two columns rather than as
// ragged text trailing each command.
func TestDescriptionsAlignToCommonColumn(t *testing.T) {
	rows := []completionTrayRow{
		{primary: "/clear", secondary: "aaa"},
		{primary: "/sandbox", secondary: "bbb"},
	}
	got := strings.Split(renderCompletionTray(rows, 0, 60), "\n")
	a := strings.Index(stripANSI(got[0]), "aaa")
	b := strings.Index(stripANSI(got[1]), "bbb")
	if a != b {
		t.Errorf("secondaries start at columns %d and %d, want the same", a, b)
	}
}

// TestDescriptionColumnIgnoresRowsWithoutDescriptions pins the deliberate exclusion: a row
// with no secondary paints nothing at the description column, so a long bare primary must
// not push that column right and waste the width for every row that does have one.
func TestDescriptionColumnIgnoresRowsWithoutDescriptions(t *testing.T) {
	withBare := []completionTrayRow{
		{primary: "/clear", secondary: "aaa"},
		{primary: "/a-very-long-bare-primary"},
	}
	withoutBare := []completionTrayRow{
		{primary: "/clear", secondary: "aaa"},
	}
	got := strings.Index(stripANSI(strings.Split(renderCompletionTray(withBare, 0, 60), "\n")[0]), "aaa")
	want := strings.Index(stripANSI(renderCompletionTray(withoutBare, 0, 60)), "aaa")
	if got != want {
		t.Errorf("bare primary moved the description column to %d, want it left at %d", got, want)
	}
}

// TestNaturalWidthFitsTheAlignedDescriptions guards the seam between measuring a tray and
// drawing it. View renders at completionTrayNaturalWidth, and the renderer truncates to the
// width it is given, so a natural width that forgot the alignment padding would silently cut
// the tail off the longest description.
func TestNaturalWidthFitsTheAlignedDescriptions(t *testing.T) {
	// The SHORTEST primary carries the LONGEST description on purpose: that is the row
	// whose total width is dominated by the alignment padding, so it is the row a natural
	// width that forgot the padding would cut.
	rows := []completionTrayRow{
		{primary: "/a-much-longer-command", secondary: "short"},
		{primary: "/x", secondary: "a description long enough to need the padded width"},
	}
	view := renderCompletionTray(rows, 0, completionTrayNaturalWidth(rows))
	for i, row := range rows {
		if line := stripANSI(strings.Split(view, "\n")[i]); !strings.HasSuffix(strings.TrimRight(line, " "), row.secondary) {
			t.Errorf("row %d = %q, want it to end with the whole description %q", i, line, row.secondary)
		}
	}
}
