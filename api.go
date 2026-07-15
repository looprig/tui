// Package tui exposes the reusable Looprig terminal interface.
package tui

import (
	"context"

	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/tui/internal/presentation"
)

// Public presentation contracts retain their original type identity through aliases.
type (
	EventStream         = presentation.EventStream
	Agent               = presentation.Agent
	OpenAgent           = presentation.OpenAgent
	AgentBanner         = presentation.AgentBanner
	AgentHolder         = presentation.AgentHolder
	TerminalErrorHolder = presentation.TerminalErrorHolder
	HandoffFinalizer    = presentation.HandoffFinalizer
	Screen              = presentation.Screen
	DisplayProjection   = presentation.DisplayProjection
	RestoreBacklogError = presentation.RestoreBacklogError
	Status              = presentation.Status
	ToolStatus          = presentation.ToolStatus
	ToolCallView        = presentation.ToolCallView
)

const (
	StatusIdle         = presentation.StatusIdle
	StatusRunning      = presentation.StatusRunning
	StatusInterrupting = presentation.StatusInterrupting
	StatusResetting    = presentation.StatusResetting

	ToolRunning   = presentation.ToolRunning
	ToolOK        = presentation.ToolOK
	ToolError     = presentation.ToolError
	ToolCancelled = presentation.ToolCancelled
)

// New constructs the interactive terminal screen.
func New(ctx context.Context, agent Agent, open OpenAgent, banner AgentBanner) Screen {
	return presentation.New(ctx, agent, open, banner)
}

// FoldDisplay rebuilds the display projection from session events.
func FoldDisplay(events []event.Event) DisplayProjection {
	return presentation.FoldDisplay(events)
}

// AllLoopsEventFilter subscribes to enduring and ephemeral events from every loop.
func AllLoopsEventFilter() event.EventFilter {
	return presentation.AllLoopsEventFilter()
}

// RenderStatusLine renders a standalone status indicator.
func RenderStatusLine(status Status) string {
	return presentation.RenderStatusLine(status)
}
