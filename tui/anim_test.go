package tui

import (
	"strings"
	"testing"

	"github.com/looprig/cli/tui/styles"
	"github.com/looprig/core/content"
)

// TestAnimStateAdvance covers the per-tick animation step: blink toggles each call
// and frame increments. ticking is intentionally untouched (start/stop is Screen's
// concern, not the animState's).
func TestAnimStateAdvance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		in        animState
		wantBlink bool
		wantFrame uint
	}{
		{name: "zero advances to blink on, frame 1", in: animState{}, wantBlink: true, wantFrame: 1},
		{name: "blink on advances to off, frame increments", in: animState{blink: true, frame: 1}, wantBlink: false, wantFrame: 2},
		{name: "ticking preserved through advance", in: animState{ticking: true, frame: 7}, wantBlink: true, wantFrame: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.in.advance()
			if got.blink != tt.wantBlink {
				t.Errorf("advance().blink = %v, want %v", got.blink, tt.wantBlink)
			}
			if got.frame != tt.wantFrame {
				t.Errorf("advance().frame = %d, want %d", got.frame, tt.wantFrame)
			}
			if got.ticking != tt.in.ticking {
				t.Errorf("advance() changed ticking to %v, want %v (unchanged)", got.ticking, tt.in.ticking)
			}
		})
	}
}

// TestAnimStateReset covers the idle reset: every field returns to its zero value so
// no animation lingers and a fresh turn starts a clean tick loop.
func TestAnimStateReset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   animState
	}{
		{name: "zero stays zero", in: animState{}},
		{name: "blinking ticking state resets", in: animState{blink: true, frame: 42, ticking: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.in.reset(); got != (animState{}) {
				t.Errorf("reset() = %+v, want zero animState", got)
			}
		})
	}
}

// TestSpinnerGlyph covers the running-tool spinner cell selection: it returns the
// frame-indexed cell and wraps modulo the frame count so any (unbounded) frame value
// is in range and never panics.
func TestSpinnerGlyph(t *testing.T) {
	t.Parallel()

	n := uint(len(spinnerFrames))
	tests := []struct {
		name  string
		frame uint
		want  string
	}{
		{name: "frame 0", frame: 0, want: spinnerFrames[0]},
		{name: "frame 1", frame: 1, want: spinnerFrames[1]},
		{name: "last frame", frame: n - 1, want: spinnerFrames[n-1]},
		{name: "wraps at count", frame: n, want: spinnerFrames[0]},
		{name: "wraps far past count", frame: n*3 + 2, want: spinnerFrames[2]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := spinnerGlyph(tt.frame); got != tt.want {
				t.Errorf("spinnerGlyph(%d) = %q, want %q", tt.frame, got, tt.want)
			}
		})
	}
}

// TestWorkingWord covers the live working-word selection for an empty-text tool step
// (design §3 rule 4): the list is non-empty, frame 0 returns the first word, and the
// counter wraps modulo the word count so any (unbounded) live frame is in range and
// never panics. It mirrors TestSpinnerGlyph — the word is a purely live affordance.
func TestWorkingWord(t *testing.T) {
	t.Parallel()

	if len(workingWords) == 0 {
		t.Fatal("workingWords is empty; an empty-text tool step needs at least one live word")
	}

	n := uint(len(workingWords))
	tests := []struct {
		name  string
		frame uint
		want  string
	}{
		{name: "frame 0", frame: 0, want: workingWords[0]},
		{name: "last frame", frame: n - 1, want: workingWords[n-1]},
		{name: "wraps at count", frame: n, want: workingWords[0]},
		{name: "wraps far past count", frame: n*4 + 1, want: workingWords[1]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := workingWord(tt.frame); got != tt.want {
				t.Errorf("workingWord(%d) = %q, want %q", tt.frame, got, tt.want)
			}
		})
	}
}

// TestLiveDot covers the blink phase of the live assistant bullet: lit when blink is
// off, dimmed when on, and both 2 columns wide so narration alignment is unchanged.
func TestLiveDot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		blink bool
		want  string
	}{
		{name: "blink off renders lit dot", blink: false, want: liveDotLit},
		{name: "blink on renders dimmed dot", blink: true, want: liveDotDim},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := liveDot(tt.blink)
			if got != tt.want {
				t.Errorf("liveDot(%v) = %q, want %q", tt.blink, got, tt.want)
			}
			// Display width (ANSI color is zero-width) must stay dotWidth for alignment;
			// count runes AFTER stripping the lit dot's color codes.
			if w := len([]rune(stripANSI(got))); w != dotWidth {
				t.Errorf("liveDot(%v) width = %d visible runes, want %d (alignment)", tt.blink, w, dotWidth)
			}
		})
	}
}

// TestLiveDotPhasesDiffer pins that the two blink phases are visually distinct — the
// whole point of the blink is that the live dot changes between frames.
func TestLiveDotPhasesDiffer(t *testing.T) {
	t.Parallel()

	if liveDot(true) == liveDot(false) {
		t.Errorf("liveDot blink phases identical (%q); they must differ to animate", liveDot(true))
	}
}

// TestRenderLiveAssistantBlink covers the live-only animation threading: the live
// assistant bullet differs between blink phases, while the committed renderAssistant
// is UNCHANGED by any animation state (it never takes an anim — it always renders the
// static lit dot).
func TestRenderLiveAssistantBlink(t *testing.T) {
	t.Parallel()

	const text = "working on it"

	lit := renderLiveAssistant("", text, nil, nil, false, 80, animState{blink: false})
	dim := renderLiveAssistant("", text, nil, nil, false, 80, animState{blink: true})

	if lit == dim {
		t.Fatalf("live assistant identical across blink phases (%q); the live dot must blink", stripANSI(lit))
	}
	// The lit live phase matches the static committed render (same lit dot).
	committed := renderAssistant("", text, "", false, 80, formatThought(0))
	if stripANSI(lit) != stripANSI(committed) {
		t.Errorf("lit live render %q != committed render %q; lit phase must equal the static dot",
			stripANSI(lit), stripANSI(committed))
	}
	// The dimmed phase carries the dimmed bullet, not the lit "●".
	if !strings.Contains(stripANSI(dim), strings.TrimRight(liveDotDim, " ")) {
		t.Errorf("dimmed live render %q missing the dimmed bullet %q", stripANSI(dim), liveDotDim)
	}
}

// TestRenderLiveAssistantSpinner covers the running tool card showing the spinner
// frame for the current animState.frame, while a RESOLVED card keeps its static
// glyph regardless of frame.
func TestRenderLiveAssistantSpinner(t *testing.T) {
	t.Parallel()

	t.Run("running card shows spinner frame", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{{ToolName: "Bash", Summary: "ls", Status: ToolRunning}}
		for _, frame := range []uint{0, 1, 5} {
			got := stripANSI(renderLiveAssistant("", "checking", calls, nil, false, 80, animState{frame: frame}))
			if !strings.Contains(got, spinnerGlyph(frame)) {
				t.Errorf("frame %d: live render %q missing spinner glyph %q", frame, got, spinnerGlyph(frame))
			}
			if strings.Contains(got, glyphRunning) {
				t.Errorf("frame %d: live render %q still shows the static running glyph %q, want the spinner",
					frame, got, glyphRunning)
			}
		}
	})

	t.Run("resolved card keeps static glyph", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{{ToolName: "Bash", Summary: "ls", Status: ToolOK, Result: []string{"a.go"}}}
		got := stripANSI(renderLiveAssistant("", "done", calls, nil, false, 80, animState{frame: 3}))
		if !strings.Contains(got, glyphOK) {
			t.Errorf("resolved card live render %q missing static OK glyph %q", got, glyphOK)
		}
		if strings.Contains(got, spinnerGlyph(3)) {
			t.Errorf("resolved card live render %q animated the static OK glyph", got)
		}
	})

	t.Run("card-only segment blinks bare bullet", func(t *testing.T) {
		t.Parallel()

		calls := []ToolCallView{{ToolName: "Bash", Status: ToolRunning}}
		lit := stripANSI(renderLiveAssistant("", "", calls, nil, false, 80, animState{blink: false}))
		dim := stripANSI(renderLiveAssistant("", "", calls, nil, false, 80, animState{blink: true, frame: 1}))
		if lit == dim {
			t.Errorf("card-only live segment identical across blink phases (%q); the bare bullet must blink", lit)
		}
	})
}

// TestRenderEntryStaticUnderAnimation pins the committed path's immutability: the
// committed renderEntry has NO animation parameter, so a committed assistant entry and
// a committed (resolved) tool entry render identically no matter what — there is no
// way for animation state to reach them. Asserting the rendered lines are stable
// proves the frozen-scrollback invariant at the render boundary.
func TestRenderEntryStaticUnderAnimation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		e    entry
	}{
		{
			name: "committed assistant entry",
			e: entry{Kind: kindAssistant, Blocks: []content.Block{
				&content.TextBlock{Text: "final answer"},
			}},
		},
		{
			name: "committed resolved tool entry",
			e: entry{Kind: kindTool, Calls: []ToolCallView{
				{ToolName: "Bash", Summary: "ls", Status: ToolOK, Result: []string{"a.go"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// renderEntry takes only (entry, expand, width) — no anim. Two calls with
			// identical inputs must be byte-identical: nothing can animate a committed row.
			a := renderEntry(tt.e, false, 80)
			b := renderEntry(tt.e, false, 80)
			if strings.Join(a, "\n") != strings.Join(b, "\n") {
				t.Errorf("renderEntry is not deterministic across calls:\n a=%q\n b=%q", a, b)
			}
			// A committed tool entry must NEVER carry a spinner glyph (frozen → static ⋯/✓/✗).
			joined := stripANSI(strings.Join(a, "\n"))
			for _, sf := range spinnerFrames {
				if strings.Contains(joined, sf) {
					t.Errorf("committed renderEntry %q contains spinner frame %q; committed rows must be static", joined, sf)
				}
			}
		})
	}
}

// TestCommittedAssistantNeverDimmed pins that the committed assistant bullet is the
// lit "●" — the dimmed live bullet must never leak into scrollback.
func TestCommittedAssistantNeverDimmed(t *testing.T) {
	t.Parallel()

	e := entry{Kind: kindAssistant, Blocks: []content.Block{&content.TextBlock{Text: "hello"}}}
	got := stripANSI(strings.Join(renderEntry(e, false, 80), "\n"))
	if strings.Contains(got, strings.TrimRight(liveDotDim, " ")) {
		t.Errorf("committed assistant render %q contains the dimmed live bullet; committed dot must be lit", got)
	}
	if !strings.Contains(got, strings.TrimRight(styles.Dot, " ")) {
		t.Errorf("committed assistant render %q missing the lit dot %q", got, styles.Dot)
	}
}

// NOTE: the old scrollback Screen's blink/tick tests (TestHandleBlinkWhileRunning,
// TestHandleBlinkWhileIdle, TestStartBlinkGuard, TestTurnStartTicking,
// TestBlinkDoesNotFlushScrollback) were removed when that shell was archived. The
// modern shell's equivalent shimmer-tick behavior is covered by TestModernTickLifecycle
// and TestModernAnimTick in modern_test.go.
