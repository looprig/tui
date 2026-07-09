package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// swapRunner installs fn as the package-level exec seam and returns a restore
// func. Callers must run sequentially (no t.Parallel) — the seam is a shared
// mutable package var, so racing swaps would be a data race under -race.
func swapRunner(fn func(argv []string, stdin string) error) func() {
	prev := runClipboardCmd
	runClipboardCmd = fn
	return func() { runClipboardCmd = prev }
}

// hostArgv is the local clipboard command for the test host (nil on an
// unsupported OS), used to skip seam-reaching assertions where none applies.
func hostArgv() []string {
	return localClipboardArgv(runtime.GOOS, os.Getenv("WAYLAND_DISPLAY") != "")
}

func TestLocalClipboardArgv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		goos    string
		wayland bool
		want    []string
	}{
		{name: "darwin uses pbcopy", goos: "darwin", wayland: false, want: []string{"pbcopy"}},
		{name: "darwin ignores the wayland flag", goos: "darwin", wayland: true, want: []string{"pbcopy"}},
		{name: "linux wayland uses wl-copy", goos: "linux", wayland: true, want: []string{"wl-copy"}},
		{name: "linux x11 uses xclip", goos: "linux", wayland: false, want: []string{"xclip", "-selection", "clipboard"}},
		{name: "unknown os yields nil", goos: "plan9", wayland: false, want: nil},
		{name: "empty goos yields nil", goos: "", wayland: false, want: nil},
		{name: "windows unsupported yields nil", goos: "windows", wayland: true, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := localClipboardArgv(tt.goos, tt.wayland)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("localClipboardArgv(%q, %v) = %v, want %v", tt.goos, tt.wayland, got, tt.want)
			}
		})
	}
}

func TestCopyCmd(t *testing.T) {
	// Subtests swap the package-level runClipboardCmd seam, so they run
	// sequentially (no t.Parallel) — a shared mutable seam cannot be raced.
	tests := []struct {
		name      string
		text      string
		wantBatch bool // non-empty text batches SetClipboard + the local write
		wantN     int
		wantExec  bool // whether the local exec seam is reached
	}{
		{name: "non-empty text copies both ways", text: "hi", wantBatch: true, wantN: 2, wantExec: true},
		{name: "multibyte text counts runes not bytes", text: "héllo→", wantBatch: true, wantN: 6, wantExec: true},
		{name: "empty text is a no-op", text: "", wantBatch: false, wantN: 0, wantExec: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotStdin string
			var called bool
			restore := swapRunner(func(argv []string, stdin string) error {
				called = true
				gotStdin = stdin
				return nil
			})
			defer restore()

			cmd := copyCmd(tt.text)
			if cmd == nil {
				t.Fatal("copyCmd returned a nil tea.Cmd")
			}
			msg := cmd()

			if tt.wantBatch {
				batch, ok := msg.(tea.BatchMsg)
				if !ok {
					t.Fatalf("copyCmd msg = %T, want tea.BatchMsg", msg)
				}
				if len(batch) != 2 {
					t.Fatalf("batch has %d cmds, want 2 (SetClipboard + local write)", len(batch))
				}
				var gotCopied, gotOSC bool
				var copied clipboardCopiedMsg
				var oscText string
				for _, c := range batch {
					switch m := c().(type) {
					case clipboardCopiedMsg:
						gotCopied = true
						copied = m
					default:
						// SetClipboard's message type is unexported in tea; it is
						// a string underneath, so its %v form is the payload text.
						gotOSC = true
						oscText = fmt.Sprintf("%v", m)
					}
				}
				if !gotCopied {
					t.Error("batch is missing the local-write command (clipboardCopiedMsg)")
				}
				if !gotOSC {
					t.Error("batch is missing the SetClipboard (OSC 52) command")
				}
				if oscText != tt.text {
					t.Errorf("SetClipboard text = %q, want %q", oscText, tt.text)
				}
				if copied.n != tt.wantN {
					t.Errorf("copied.n = %d, want %d", copied.n, tt.wantN)
				}
				if copied.err != nil {
					t.Errorf("copied.err = %v, want nil", copied.err)
				}
			} else {
				copied, ok := msg.(clipboardCopiedMsg)
				if !ok {
					t.Fatalf("copyCmd msg = %T, want clipboardCopiedMsg", msg)
				}
				if copied.n != tt.wantN || copied.err != nil {
					t.Errorf("empty copy = %+v, want {n:0 err:nil}", copied)
				}
			}

			if called != tt.wantExec {
				t.Errorf("local exec seam called = %v, want %v", called, tt.wantExec)
			}
			if tt.wantExec && gotStdin != tt.text {
				t.Errorf("local exec stdin = %q, want %q", gotStdin, tt.text)
			}
		})
	}
}

func TestWriteLocalClipboard(t *testing.T) {
	// Swaps the seam; runs sequentially.
	cause := errors.New("exec failed")
	tests := []struct {
		name    string
		seamErr error
		wantErr bool
	}{
		{name: "success returns nil", seamErr: nil, wantErr: false},
		{name: "failure is wrapped in a typed clipboardError", seamErr: cause, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr && hostArgv() == nil {
				t.Skip("host has no local clipboard binary; seam is never reached")
			}
			restore := swapRunner(func(argv []string, stdin string) error { return tt.seamErr })
			defer restore()

			err := writeLocalClipboard("payload")
			if (err != nil) != tt.wantErr {
				t.Fatalf("writeLocalClipboard err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr {
				return
			}
			var ce *clipboardError
			if !errors.As(err, &ce) {
				t.Fatalf("error is %T, want *clipboardError", err)
			}
			if !errors.Is(err, cause) {
				t.Error("clipboardError does not unwrap to the exec cause")
			}
			if len(ce.Argv) == 0 {
				t.Error("clipboardError.Argv is empty, want the clipboard command")
			}
		})
	}
}

func TestRunClipboardCmdDefault(t *testing.T) {
	// Exercises the REAL os/exec seam (no mock) to prove the runner actually
	// pipes text to the child's stdin and reports failures. Uses sh+cat so it
	// never touches the real system clipboard.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	tests := []struct {
		name    string
		argv    []string
		stdin   string
		wantErr bool
	}{
		{name: "stdin is piped to the child", argv: []string{"sh", "-c", `[ "$(cat)" = "ping" ]`}, stdin: "ping", wantErr: false},
		{name: "wrong stdin fails the child", argv: []string{"sh", "-c", `[ "$(cat)" = "ping" ]`}, stdin: "pong", wantErr: true},
		{name: "non-zero exit is an error", argv: []string{"sh", "-c", "exit 1"}, stdin: "", wantErr: true},
		{name: "missing binary is an error", argv: []string{"looprig-no-such-clipboard-bin"}, stdin: "x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runClipboardCmd(tt.argv, tt.stdin)
			if (err != nil) != tt.wantErr {
				t.Errorf("runClipboardCmd(%v) err = %v, wantErr %v", tt.argv, err, tt.wantErr)
			}
		})
	}
}
