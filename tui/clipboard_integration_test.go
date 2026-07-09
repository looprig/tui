//go:build integration

package tui

import (
	"os/exec"
	"testing"
)

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
