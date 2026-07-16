package presentation

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/looprig/core/uuid"
	"github.com/looprig/tui/components"
)

type ModeID string
type ModelID string
type EffortID string
type AccessID string

type ModeOption struct {
	ID          ModeID
	Label       string
	Description string
	Aliases     []string
}

type ModelOption struct {
	ID          ModelID
	Label       string
	Description string
	Aliases     []string
}

type EffortOption struct {
	ID          EffortID
	Label       string
	Description string
	Aliases     []string
}

type AccessOption struct {
	ID          AccessID
	Label       string
	Description string
	Aliases     []string
}

// LoopRuntimeOptions contains available choices only. Current mode, model, and
// effort are authoritative event projections and deliberately do not live here.
type LoopRuntimeOptions struct {
	Modes   []ModeOption
	Models  []ModelOption
	Efforts []EffortOption
}

// AccessOptions contains the session-scoped available access choices and the
// display-only workspace root. Current access is projected from enduring events.
type AccessOptions struct {
	Root    string
	Choices []AccessOption
}

// RuntimeCatalog is the optional read-only runtime-choice capability of an Agent.
type RuntimeCatalog interface {
	LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error)
	AccessOptions(context.Context) (AccessOptions, error)
}

// RuntimeController is the optional typed runtime-mutation capability of an Agent.
type RuntimeController interface {
	SetMode(context.Context, uuid.UUID, ModeID) error
	SetModel(context.Context, uuid.UUID, ModelID) error
	SetEffort(context.Context, uuid.UUID, EffortID) error
	SetAccess(context.Context, AccessID) error
}

type runtimeTrayKind uint8

const (
	runtimeTrayNone runtimeTrayKind = iota
	runtimeTrayMode
	runtimeTrayModel
	runtimeTrayEffort
	runtimeTrayAccess
)

type runtimeChoicesMsg struct {
	kind   runtimeTrayKind
	loopID uuid.UUID
	items  []components.ValueItem
	root   string
	err    error
}

type runtimeMutationMsg struct {
	kind runtimeTrayKind
	err  error
}

type accessMetadataMsg struct {
	options AccessOptions
	err     error
}

const runtimeControlTimeout = 5 * time.Second

func queryAccessMetadata(ctx context.Context, catalog RuntimeCatalog) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, runtimeControlTimeout)
		defer cancel()
		options, err := catalog.AccessOptions(c)
		return accessMetadataMsg{options: options, err: err}
	}
}

func queryRuntimeChoices(ctx context.Context, catalog RuntimeCatalog, kind runtimeTrayKind, loopID uuid.UUID) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, runtimeControlTimeout)
		defer cancel()
		msg := runtimeChoicesMsg{kind: kind, loopID: loopID}
		if kind == runtimeTrayAccess {
			options, err := catalog.AccessOptions(c)
			msg.err, msg.root = err, options.Root
			for _, option := range options.Choices {
				msg.items = append(msg.items, components.ValueItem{ID: string(option.ID), Label: option.Label, Description: option.Description, Aliases: option.Aliases})
			}
			return msg
		}
		options, err := catalog.LoopRuntimeOptions(c, loopID)
		msg.err = err
		switch kind {
		case runtimeTrayMode:
			for _, option := range options.Modes {
				msg.items = append(msg.items, components.ValueItem{ID: string(option.ID), Label: option.Label, Description: option.Description, Aliases: option.Aliases})
			}
		case runtimeTrayModel:
			for _, option := range options.Models {
				msg.items = append(msg.items, components.ValueItem{ID: string(option.ID), Label: option.Label, Description: option.Description, Aliases: option.Aliases})
			}
		case runtimeTrayEffort:
			for _, option := range options.Efforts {
				msg.items = append(msg.items, components.ValueItem{ID: string(option.ID), Label: option.Label, Description: option.Description, Aliases: option.Aliases})
			}
		}
		return msg
	}
}

func mutateRuntime(ctx context.Context, controller RuntimeController, kind runtimeTrayKind, loopID uuid.UUID, id string) tea.Cmd {
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, runtimeControlTimeout)
		defer cancel()
		var err error
		switch kind {
		case runtimeTrayMode:
			err = controller.SetMode(c, loopID, ModeID(id))
		case runtimeTrayModel:
			err = controller.SetModel(c, loopID, ModelID(id))
		case runtimeTrayEffort:
			err = controller.SetEffort(c, loopID, EffortID(id))
		case runtimeTrayAccess:
			err = controller.SetAccess(c, AccessID(id))
		default:
			err = fmt.Errorf("unknown runtime control")
		}
		return runtimeMutationMsg{kind: kind, err: err}
	}
}
