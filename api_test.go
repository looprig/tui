package tui

import (
	"os"
	"strings"
	"testing"
)

// This compile-time reference pins the module-root presentation contract.
var (
	_ Agent
	_ EventStream
	_ OpenAgent
	_ AgentBanner
	_ AgentHolder
	_ TerminalErrorHolder
	_ HandoffFinalizer
	_ Screen
	_ DisplayProjection
	_ RestoreBacklogError
	_ ToolCallView
	_ Status     = StatusIdle
	_ Status     = StatusRunning
	_ Status     = StatusInterrupting
	_ Status     = StatusResetting
	_ ToolStatus = ToolRunning
	_ ToolStatus = ToolOK
	_ ToolStatus = ToolError
	_ ToolStatus = ToolCancelled
	_            = New
	_            = FoldDisplay
	_            = AllLoopsEventFilter
	_            = RenderStatusLine
	_ error      = (*EmptyInputError)(nil)
	_ error      = (*UnsupportedAttachmentError)(nil)
	_ error      = (*BinaryAttachmentError)(nil)
	_ error      = (*ImageUnsupportedError)(nil)
	_ error      = (*DeniedAttachmentError)(nil)
	_ error      = (*AttachmentTooLargeError)(nil)
	_ error      = (*AttachmentNotFoundError)(nil)
	_ error      = (*AttachmentReadError)(nil)
)

func TestRootContainsOnlyFacadeFiles(t *testing.T) {
	t.Parallel()

	allowed := map[string]struct{}{
		"api.go":      {},
		"api_test.go": {},
		"errors.go":   {},
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if _, ok := allowed[entry.Name()]; !ok {
			t.Errorf("root implementation file %q belongs in an owned package", entry.Name())
		}
	}
}
