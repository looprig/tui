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
const trayGolden = "\x1b[48;2;48;48;48m\x1b[38;2;115;115;115m▌\x1b[m\x1b[48;2;48;48;48m /clear  \x1b[2mstart a new conversation\x1b[m\x1b[48;2;48;48;48m          \x1b[m\n\x1b[48;2;162;210;255m\x1b[38;2;16;16;16m▌ /compact  compact the current conversation\x1b[m\x1b[48;2;162;210;255m\x1b[m"

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
