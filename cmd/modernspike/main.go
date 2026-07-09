// Command modernspike is a throwaway feasibility spike (NOT production code and
// NOT wired into the real CLI). Its sole purpose is to let a human confirm that,
// inside a Bubble Tea v2 alt-screen viewport, mouse-wheel scroll and
// drag-to-select-and-copy can coexist — specifically in Apple Terminal and tmux
// on local macOS.
//
// Run it with: go run ./cmd/modernspike
package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/atotto/clipboard"
)

// pos is a coordinate in content space: row is an index into model.lines
// (offset-independent), col is a rune column within that line.
type pos struct {
	row int
	col int
}

type model struct {
	lines  []string
	offset int
	width  int
	height int

	selecting bool
	hasSel    bool
	anchor    pos
	cursor    pos

	status string // transient hint-line message (e.g. "copied 42 chars")
}

var selStyle = lipgloss.NewStyle().Reverse(true)

func (m model) Init() tea.Cmd { return nil }

func (m model) maxOffset() int {
	visible := m.height - 1
	if visible < 1 {
		visible = 1
	}
	max := len(m.lines) - visible
	if max < 0 {
		max = 0
	}
	return max
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (m *model) scroll(delta int) {
	m.offset = clamp(m.offset+delta, 0, m.maxOffset())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.offset = clamp(m.offset, 0, m.maxOffset())

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up":
			m.scroll(-1)
		case "down":
			m.scroll(1)
		case "pgup":
			m.scroll(-(m.height - 1))
		case "pgdown":
			m.scroll(m.height - 1)
		case "g":
			m.offset = 0
		case "G":
			m.offset = m.maxOffset()
		}

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			m.scroll(-3)
		case tea.MouseWheelDown:
			m.scroll(3)
		}

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft && m.inContent(msg.Y) {
			m.anchor = pos{row: m.offset + msg.Y, col: msg.X}
			m.cursor = m.anchor
			m.selecting = true
			m.hasSel = true
			m.status = ""
		}

	case tea.MouseMotionMsg:
		if m.selecting && msg.Button == tea.MouseLeft {
			row := clamp(m.offset+msg.Y, 0, max0(len(m.lines)-1))
			m.cursor = pos{row: row, col: msg.X}
		}

	case tea.MouseReleaseMsg:
		if m.selecting {
			m.selecting = false
			text := m.selectedText()
			if text != "" {
				m.copy(text)
				m.status = fmt.Sprintf("copied %d chars", len([]rune(text)))
			}
		}
	}

	return m, nil
}

// inContent reports whether a screen row y falls within the scrollable content
// region (every row except the bottom hint line).
func (m model) inContent(y int) bool {
	return y >= 0 && y < m.height-1
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

// normSel returns the selection endpoints ordered so start <= end (by row, then
// col), or false if there is nothing to render/extract.
func (m model) normSel() (start, end pos, ok bool) {
	if !m.hasSel {
		return pos{}, pos{}, false
	}
	a, b := m.anchor, m.cursor
	if a.row > b.row || (a.row == b.row && a.col > b.col) {
		a, b = b, a
	}
	if a == b {
		return pos{}, pos{}, false
	}
	return a, b, true
}

// selectedText extracts the highlighted span, clamping columns to each line's
// rune length and joining multi-row selections with "\n".
func (m model) selectedText() string {
	start, end, ok := m.normSel()
	if !ok {
		return ""
	}
	if start.row == end.row {
		r := []rune(m.lines[start.row])
		lo := clamp(start.col, 0, len(r))
		hi := clamp(end.col, 0, len(r))
		return string(r[lo:hi])
	}
	var b strings.Builder
	for row := start.row; row <= end.row; row++ {
		r := []rune(m.lines[row])
		switch row {
		case start.row:
			lo := clamp(start.col, 0, len(r))
			b.WriteString(string(r[lo:]))
		case end.row:
			hi := clamp(end.col, 0, len(r))
			b.WriteString(string(r[:hi]))
		default:
			b.WriteString(string(r))
		}
		if row != end.row {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// copy writes to the macOS clipboard via pbcopy (the reliable local path) and,
// best-effort, emits an OSC 52 sequence for remote/SSH sessions.
func (m model) copy(text string) {
	_ = clipboard.WriteAll(text)

	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	if os.Getenv("TMUX") != "" {
		seq = "\x1bPtmux;\x1b" + seq + "\x1b\\"
	}
	_, _ = os.Stdout.WriteString(seq)
}

func (m model) View() tea.View {
	if m.width == 0 || m.height < 2 {
		return tea.NewView("")
	}

	visible := m.height - 1
	start := m.offset
	end := clamp(start+visible, 0, len(m.lines))

	start2, end2, hasSel := m.normSel()

	rows := make([]string, 0, visible)
	for i := start; i < end; i++ {
		line := m.lines[i]
		if hasSel && i >= start2.row && i <= end2.row {
			line = m.highlight(line, i, start2, end2)
		}
		rows = append(rows, line)
	}
	// Pad to a full region so the hint line stays pinned to the bottom row.
	for len(rows) < visible {
		rows = append(rows, "")
	}

	rows = append(rows, m.hintLine())
	v := tea.NewView(strings.Join(rows, "\n"))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// highlight wraps the selected column span of one visible line in reverse video.
func (m model) highlight(line string, row int, start, end pos) string {
	r := []rune(line)
	n := len(r)

	lo := 0
	hi := n
	if row == start.row {
		lo = clamp(start.col, 0, n)
	}
	if row == end.row {
		hi = clamp(end.col, 0, n)
	}
	if lo >= hi {
		return line
	}
	return string(r[:lo]) + selStyle.Render(string(r[lo:hi])) + string(r[hi:])
}

func (m model) hintLine() string {
	base := "drag: select+copy · wheel/↑↓/PgUp/PgDn: scroll · Option-drag: native select · q: quit"
	if m.status != "" {
		base = m.status + "  ·  " + base
	}
	return base
}

func main() {
	if _, err := tea.NewProgram(model{lines: sampleLines()}).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "modernspike:", err)
		os.Exit(1)
	}
}

// sampleLines builds ~200 lines of varied transcript-shaped content. Each line
// is numbered so a human can tell exactly what got copied.
func sampleLines() []string {
	var raw []string
	add := func(s string) { raw = append(raw, s) }

	add("=== modernspike transcript sample ===")
	add("Drag across any text below, release, then paste elsewhere to verify the copy.")
	add("")

	add("│ thinking")
	add("│ The user asked whether wheel-scroll and drag-select can coexist inside a")
	add("│ Bubble Tea v2 alt-screen. The wrinkle: alt-screen apps that grab the mouse")
	add("│ normally steal drag events from the terminal, so the terminal's own native")
	add("│ selection never fires. The plan is to draw the selection ourselves and copy")
	add("│ via pbcopy, while leaving Option-drag as the native escape hatch.")
	add("│ Let's confirm the columns line up with multi-byte runes like → and ✓ too.")
	add("")

	add("⎿ ReadFile internal/render/scroll.go ✓")
	add("    12  func (v *Viewport) visible() []string {")
	add("    13      lo := v.offset")
	add("    14      hi := lo + v.height - 1")
	add("    15      return v.lines[lo:hi]")
	add("    16  }")
	add("    17")
	add("    18  // clamp keeps the offset inside the scrollable range.")
	add("")

	add("⎿ Bash go test ./... ✓")
	add("    ok   github.com/looprig/cli/internal/render   0.184s")
	add("    ok   github.com/looprig/cli/internal/select   0.092s")
	add("    ?    github.com/looprig/cli/cmd/modernspike    [no test files]")
	add("")

	paras := []string{
		"Selection highlighting is drawn per-cell using a reverse-video Lipgloss style. Because we index by []rune rather than bytes, multi-byte glyphs keep their columns aligned and the highlighted span matches exactly what lands on the clipboard.",
		"Scrolling adjusts an integer offset into the backing slice; the wheel moves it by three rows, arrow keys by one, and PgUp/PgDn by a full page. The offset is always clamped so you can never scroll past the ends of the buffer.",
		"The crux for Apple Terminal and tmux is the dual write: pbcopy handles the local clipboard unconditionally, and an OSC 52 escape (tmux-wrapped when $TMUX is set) covers remote sessions where only the far end can reach the clipboard.",
		"If the app-drawn selection ever feels wrong, hold Option (Alt) and drag: Apple Terminal falls back to its own native, terminal-level selection because Option-drag bypasses the mouse reporting the alt-screen app has enabled.",
	}
	for pi, p := range paras {
		add(fmt.Sprintf("Paragraph %d:", pi+1))
		for _, chunk := range wrap(p, 76) {
			add("  " + chunk)
		}
		add("")
	}

	// Fill out to a comfortable, obviously-numbered length for scroll testing.
	for i := 1; len(raw) < 190; i++ {
		add(fmt.Sprintf("filler line %-3d  the quick brown fox jumps over the lazy dog  →  end", i))
	}
	add("")
	add("=== end of sample — scroll back up with the wheel or PgUp ===")

	// Prefix every line with a stable 1-based line number for easy verification.
	out := make([]string, len(raw))
	for i, s := range raw {
		out[i] = fmt.Sprintf("%3d  %s", i+1, s)
	}
	return out
}

// wrap does naive word-wrapping at the given width (spike-grade, no hyphenation).
func wrap(s string, width int) []string {
	words := strings.Fields(s)
	var lines []string
	var cur strings.Builder
	for _, w := range words {
		if cur.Len() > 0 && cur.Len()+1+len(w) > width {
			lines = append(lines, cur.String())
			cur.Reset()
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
	}
	if cur.Len() > 0 {
		lines = append(lines, cur.String())
	}
	return lines
}
