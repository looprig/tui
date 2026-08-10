# TUI Session Adapter Specification

## Purpose

The TUI needs a reusable adapter between a Harness `session.SessionController` and the `tui.Agent` interface. Today that adapter lives in CodeRig, even though none of its behavior is specific to software engineering.

The adapter belongs in `github.com/looprig/tui/sessionadapter`. CodeRig and future terminal applications should only assemble a Rig, open a Session, and hand the controller to this adapter.

## Current problem

The current CodeRig adapter owns generic behavior:

- forwarding input to the active or selected Loop
- interruption and compaction
- model capability lookup for image input
- whole-session event subscription
- cold replay for restored sessions
- tracking open gates by Loop ID and tool execution ID
- translating approve, deny, and answer actions into gate responses
- bounded cleanup after failed restore initialization
- idempotent session shutdown

This is presentation infrastructure. Keeping it in CodeRig makes every consumer reimplement persistence replay, gate correlation, and lifecycle behavior.

## Public package

Add `github.com/looprig/tui/sessionadapter`.

The package exposes a concrete `Adapter` that satisfies `tui.Agent` and provides two explicit constructors:

```go
func New(controller session.SessionController) *Adapter

func Restore(
    ctx context.Context,
    controller session.SessionController,
    replay ReplayOpener,
) (*Adapter, error)
```

`New` wraps a newly created session and does not read history. `Restore` requires a replay source and rebuilds the presentation state before returning. This keeps the fresh and restored contracts visible without duplicating the session construction path.

`ReplayOpener` is the narrow interface already satisfied by `*sessionstore.Store`:

```go
type ReplayOpener interface {
    OpenEventReplayer(
        uuid.UUID,
        sessionstore.ReplayRequest,
    ) (journal.EventReplayer, error)
}
```

The concrete adapter may also expose `SessionID()` for application composition. That method is outside `tui.Agent` and must not be added to the TUI interface.

## Event behavior

The adapter opens one live session subscription for each TUI subscription. It applies `event.ShouldDeliver` before updating internal projections or forwarding an event.

For restored sessions, `Restore` reads public enduring events from the beginning of the session. It must:

1. honor context cancellation
2. reject replay corruption or read failure
3. fold visible gate events before returning
4. materialize visible enduring events in session order
5. shut down the controller if initialization fails

Fresh sessions return no replay backlog.

## Gate correlation

The TUI addresses a pending interaction by `(LoopID, ToolExecutionID)`. The harness resolves it by `gate.ID`. The adapter owns the translation index:

```text
(LoopID, ToolExecutionID) -> GateID
```

`GateOpened` adds both forward and reverse entries. `GateResolved` removes both. The index must be safe for concurrent cold replay, live subscription forwarding, and user responses.

An unmatched response fails closed with a typed error owned by `sessionadapter`, not by CodeRig.

## Lifecycle

`Close` calls `SessionController.Shutdown` exactly once. Concurrent callers observe the same result.

If restored-session initialization fails after the controller has been opened, the adapter performs a bounded shutdown with a context detached from the failed caller context. The initialization error and cleanup error are joined.

Closing a wrapped subscription must stop its forwarding goroutine and close the underlying session subscription.

## Package boundaries

`sessionadapter` may import:

- `github.com/looprig/tui`
- harness session, event, gate, journal, sessionstore, and tool contracts
- core content and UUID contracts

It must not import CodeRig, fsstore, sandbox, the optional tools module, or a model provider.

## Migration

CodeRig deletes its local session adapter and uses:

```go
controller, err := assembly.NewSession(ctx)
adapter := sessionadapter.New(controller)
```

or:

```go
controller, err := assembly.RestoreSession(ctx, sessionID)
adapter, err := sessionadapter.Restore(ctx, controller, stores.Session)
```

The CodeRig TUI entry point continues to receive a `tui.Agent`. No product-specific adapter remains.

## Verification

The TUI module must test:

- interface satisfaction
- fresh session behavior
- ordered cold replay
- visibility filtering
- gate insertion and removal
- duplicate tool execution IDs in different Loops
- approve, deny, and answer encoding
- replay failure cleanup
- idempotent concurrent close
- subscription teardown without goroutine leaks
