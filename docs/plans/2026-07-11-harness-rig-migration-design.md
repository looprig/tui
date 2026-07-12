# Design: CLI migration to harness rig lifecycle

**Status:** ready to implement after the harness rig branch is released
**Date:** 2026-07-11
**Owner:** `github.com/looprig/cli`
**Harness source of truth:** `harness/docs/plans/2026-07-10-rig-lifecycle-workspace-snapshots-design.md` and the compiled APIs on the harness `feat/rig-lifecycle-workspace-snapshots` branch

## Purpose

Migrate CLI's harness-facing presentation contract to the final rig/session vocabulary without turning CLI into a second composition root. CLI remains a TUI library: the executable that embeds it constructs a `rig.Rig`, creates or restores a session, and adapts that live session to `tui.Agent`.

This is a deliberately small migration. The inventory found no manual session factory, workspace wiring, checkpoint watcher, serve adapter, or tool construction in CLI production code. Those concerns must not be introduced merely because the harness API now owns them.

## Inventory

The required search and broader import/call-site searches found these sites outside `vendor/`:

| Area | CLI sites | Finding |
|---|---|---|
| Harness version | `go.mod:16`, `go.mod:101`; `vendor/github.com/looprig/harness/**`; `vendor/modules.txt` | CLI requires harness `v0.5.2`, uses a local `replace`, and commits a vendored copy. The dependency and vendor tree must move together. |
| Presentation adapter | `tui/agent.go:12-127` | `Agent` is a CLI-owned narrow interface. It adds image capability, restored-backlog, gate convenience, and UI shutdown methods that are intentionally not identical to `session.Session`. |
| Event subscription | `tui/commands.go:37-79`; `tui/sessioncore.go:96-101`; `tui/fixtures_test.go:128-194` | CLI opens one whole-session subscription and continuously unwraps `event.Delivery.Event`. The current screen already requests all-loop ephemeral and enduring events. |
| Loop identity/routing | `tui/agent.go:22-36`; `tui/sessioncore.go:34-170`; `tui/screen.go:51-61,152-161,654-667,1041-1080`; `tui/transcript.go:368-378` | The public adapter and internal projection still conflate “primary” with both a stable transcript root and the mutable active loop. Final harness has a mutable active loop and no primary-loop accessor. Focused-loop submit already uses `SubmitToLoop`. |
| Active-loops bar | `tui/loopbar.go:14-202`; `tui/screen.go:1041-1080` | The bar keeps the immutable “primary” visible under its cap. It must retain stable root, current active, and focus independently, updating active from `ActiveLoopChanged`. |
| Restore repaint | `tui/restore.go:131-183`; `tui/screen.go:196-207` | `ReplayBacklog` is a consumer-specific adapter capability. The initial active loop remains the stable root of one repaint; later `ActiveLoopChanged` events update runtime selection, not historical attribution. |
| Gate replies | `tui/agent.go:63-77`; `tui/commands.go:131-162`; `tui/interaction.go:292-325` | CLI keeps ergonomic `Approve`, `Deny`, and `ProvideAnswer`. The embedding adapter translates them to `Session.RespondGate`; CLI does not need the session controller. |
| Teardown | `tui/commands.go:165-185`; `tui/sessioncore.go:231-266`; `cli/run.go:216-227` | `Agent.Close` is the UI ownership seam and remains idempotent. An adapter that owns a `SessionController` implements it with `Shutdown`. |
| Session ID | No current read site | Final IDs come from `sess.SessionID()`, not a second return value from construction. CLI does not add an unused `SessionID` method until presentation needs it. |
| Loop mode/model control | No current mutation site | `sess.Loop(id)` exposes `Mode`/`Model`; `sess.LoopController(id)` exposes `SetMode`/`Change`. These trusted controls are not added to the ordinary TUI adapter in this migration. |
| Image capability | `tui/agent.go:39-41`; `tui/sessioncore.go:311-346`; `tui/blocks.go:53-79`; fakes in `tui/fixtures_test.go:126` and `cli/run_test.go:36` | The current session-wide `AcceptsImages()` is stale when loops use heterogeneous models or a controller changes one loop's model. Capability must be queried for the actual submission target. |
| Lifecycle construction | None | No `session.Compile`, `session.Runner`, public `session.New/Restore`, `Runner.Run/Restore`, or manual attach logic exists in CLI. |
| Workspace/checkpoints | None | No workspace placement, `CheckpointWorkspace`, snapshot watcher, or `SessionIdle` checkpoint reaction exists in CLI. Nothing is added or deleted. |
| Serve | None | CLI does not call `serve.Handler` or implement `serve.Runner`. No serve code changes are required here. |
| Tool construction | None | CLI imports `pkg/tool` only for permission request/value types. It does not define or bind tools. |

The only `SessionIdle` production concern is ordinary event rendering/status behavior. It is not a checkpoint trigger.

## Verified final harness surface

The migration was checked against the feature worktree's compiled public declarations, not the stale `v0.5.2` vendor copy:

```go
type Session interface {
	SessionID() uuid.UUID
	ActiveLoop() loop.Handle
	Loop(uuid.UUID) (loop.Handle, bool)
	Submit(context.Context, []content.Block) (uuid.UUID, error)
	SubmitToLoop(context.Context, uuid.UUID, []content.Block) (uuid.UUID, error)
	SubscribeEvents(event.EventFilter) (event.Subscription, error)
	RespondGate(context.Context, gate.GateResponse) error
	Interrupt(context.Context) (bool, error)
}

type SessionController interface {
	Session
	SetActiveLoop(context.Context, uuid.UUID) error
	LoopController(uuid.UUID) (loop.Controller, bool)
	SetSecurityCeiling(context.Context, ceiling.Level) error
	CheckpointWorkspace(context.Context) (workspacestore.Ref, error)
	RestoreWorkspace(context.Context, workspacestore.Ref) error
	Shutdown(context.Context) error
}

func (r *rig.Rig) NewSession(context.Context, ...rig.SessionOption) (session.SessionController, error)
func (r *rig.Rig) RestoreSession(context.Context, uuid.UUID) (session.SessionController, error)

type Rig[S LiveSession, O any] interface {
	NewSession(context.Context, ...O) (S, error)
	RestoreSession(context.Context, uuid.UUID) (S, error)
}
```

`loop.Handle` supplies `ID`, `Mode`, and `Model`. `loop.Controller` adds `SetMode`, atomic `Change` (including `ChangeModel` and `ChangeEffort`), and subtree `Interrupt`.

## Old-to-final API map

This table is the rule for embedding applications. Rows marked “not in CLI” are documented to prevent migration code from being invented in this module.

| Old concept/call | Final harness call | CLI decision |
|---|---|---|
| `session.Compile(...)` / compile options | `loop.Define(...)`, then `rig.Define(rig.WithLoops(...), rig.WithPrimers(...), rig.WithActivePrimer(...), rig.WithSessionStore(...), ...)` | Not in CLI. Composition belongs to the embedding executable. |
| `runner.Run(ctx)` | `sess, err := r.NewSession(ctx)` | Not in CLI. The adapter receives the returned controller. |
| `runner.Restore(ctx, id)` | `sess, err := r.RestoreSession(ctx, id)` | Not in CLI. The adapter also receives whatever replay reader it needs for `ReplayBacklog`. |
| separately returned session ID / exported field | `sess.SessionID()` | No new CLI interface member; the embedding layer reads it when needed. |
| immutable `PrimaryLoopID()` | stable adapter `RootLoopID()` plus current `sess.ActiveLoop().ID()` / durable `event.ActiveLoopChanged` | Replace the conflated method with separate root and active queries. A restored adapter derives the root from the first root `LoopStarted`; it does not pretend the final active loop was always root. |
| default input target | `sess.Submit(...)` | CLI's focused-loop composer continues using `SubmitToLoop`; an embedding adapter may map CLI's generic `Submit` convenience to `sess.Submit`. |
| explicit loop input | `sess.SubmitToLoop(...)` | Direct adapter mapping. |
| session subscription wrapper | `sess.SubscribeEvents(...)` | Keep CLI's shorter `Subscribe` adapter name. |
| approval/answer trio | `sess.RespondGate(...)` | Keep UI verbs and translate inside the adapter. |
| session interrupt | `sess.Interrupt(...)` | Direct adapter mapping. |
| session close | `controller.Shutdown(...)` | Keep `Agent.Close` because CLI owns replacement/teardown timing. The owning adapter maps it to `Shutdown`; a borrowed-session adapter must not claim lifecycle ownership. |
| current mode/model query | `sess.Loop(id)` then `handle.Mode()` / `handle.Model()` | No TUI control added. This remains available to a future status feature without exposing the controller. |
| mode/model/effort mutation | `sess.LoopController(id)` then `SetMode` / `Change` | Trusted control plane; not exposed through `tui.Agent` in this migration. |
| active-loop mutation | `controller.SetActiveLoop(ctx, id)` | Trusted control plane; the TUI observes `ActiveLoopChanged` but does not gain mutation authority. |
| session-wide image flag | `sess.Loop(id).Model().Caps.AcceptsImages` | Replace `AcceptsImages()` with `AcceptsImages(loopID)`. Unknown loops return false. Query at submit time so mode/model changes are visible. |
| `serve.Runner` | `serve.Rig[session.SessionController, rig.SessionOption]` | No CLI site. `serve.Handler(r, reads, ...)` infers both type arguments at a real composition root. |
| manual checkpoint watcher | native rig snapshot policy | No CLI site to remove. Never add a `SessionIdle` watcher here. |

## Selected design

### Preserve the UI adapter

`tui.Agent` remains the dependency-inverted boundary. It deliberately does not become an alias of `session.Session` or `session.SessionController`: CLI needs image acceptance and restored-backlog projection, while it does not need workspace rewind, security ceilings, modes, models, or active-loop mutation. This keeps trusted controls out of presentation code.

Replace the one conflated public query:

```go
PrimaryLoopID() uuid.UUID
```

with two narrow read queries:

```go
// RootLoopID returns the stable root used for transcript attribution.
RootLoopID() uuid.UUID

// ActiveLoopID returns the session's current default input target.
ActiveLoopID() uuid.UUID

// AcceptsImages reports the current model capability for one submission target.
AcceptsImages(loopID uuid.UUID) bool
```

The adapter implements `ActiveLoopID` as `sess.ActiveLoop().ID()`. For a fresh
session, `RootLoopID` is the initial active primer. For restore, it is the first root
`LoopStarted` (zero parent cause) in durable history; it must not be recomputed from
the latest active selection. This is valid because harness durably constructs the
active primer first and restore uses that first root as the default selection before
folding later `ActiveLoopChanged` events. The concrete adapter migration must lock that
cross-module assumption with a new-session and switched-then-restored test; put those
tests in the sibling `tests` repository if importing the composition root would reverse
CLI's dependency direction.

`AcceptsImages(id)` performs a fresh `sess.Loop(id)` lookup and returns
`handle.Model().Caps.AcceptsImages`; an unknown/non-live loop returns false. It does not
cache the initial model because `loop.Controller.SetMode` and `Change` can change the
selected model at a turn boundary.

### Separate transcript root, current active loop, and focus

These are different concepts and must not alias one mutable field:

- **Transcript root:** the session's original active primer, supplied by `Agent.RootLoopID`. It is a stable attribution anchor for the existing root transcript/replay projection. Rename internal `primaryLoopID` terminology to `rootLoopID`; do not rewrite historical rows when active selection changes.
- **Active loop:** the session's current durable default target. Establish its authoritative baseline from `Agent.ActiveLoopID()` only after subscribing, then reconcile every `event.ActiveLoopChanged` from the all-loop stream.
- **Focused loop:** the projection currently visible and the target of CLI's explicit `SubmitToLoop`. Focus does not silently change when another controller selects an active loop.

The active-loops bar admits the root, current active loop, focused loop, and live
loops, then applies cap priority in that order: focused, active, root, live, recent
idle. This keeps the root reachable without falsely using it as the current default
target. A future visual marker for active selection is presentation work, not required
by this migration.

### Subscription and ordering

The screen keeps one all-loop subscription for its entire session. That is essential: an active-only ephemeral filter would miss tokens after `ActiveLoopChanged`, while closing and reopening the bounded live subscription creates a loss window. The existing all-loop filter already receives the enduring selection event and every loop's ephemeral stream.

Startup and reopen use a **subscribe-before-baseline handshake**. `New` may query
`ActiveLoopID` only to initialize focus. The authoritative session-core baseline is read
in `applySubscribed`, after `Subscribe` has returned a live stream, so every later
selection is either in that stream or reflected by the baseline query. Successful
`/clear` swaps the Agent and resets the stable root, but leaves the current-active
baseline pending until the fresh subscription is installed.

The stream can already contain selection events when the baseline is read. Reconcile an
`ActiveLoopChanged{PreviousLoopID: p, ActiveLoopID: a}` against current `c` causally:

- `c == p`: apply `a`;
- `c == a`: the baseline already includes this transition, so no-op;
- otherwise: ignore the event as older than the authoritative baseline (a later queued
  event can still chain from `c`).

This handles baseline-before-transition, baseline-after-transition, and multiple queued
transitions without regressing to a stale active loop. A zero/unknown current selection
fails closed until a valid baseline is available.

`sessionCore` also folds a per-loop running map for **every** loop:

- `TurnStarted(loop)` sets `running[loop] = true`;
- `TurnDone`, `TurnFailed`, and `TurnInterrupted` set it false;
- `ActiveLoopChanged` changes selection only.

After each of those events, displayed active status is derived from
`running[activeLoopID]`, rather than directly flipping on whichever terminal arrived.
Therefore switching from a running loop to an idle loop displays idle, switching from an
idle loop to an already-running loop displays running, and a late terminal from the old
active loop cannot make the new active loop appear idle. Focused-loop rendering remains
independent and uses its per-loop projection.

`Screen.New` initializes `focusedLoopID` from `Agent.ActiveLoopID`. Successful `/clear`
does the same with the replacement Agent. Later `ActiveLoopChanged` events update active
status and bar retention but never steal focus. On `/clear`, close the old subscription,
swap Agent, reset `RootLoopID`, per-loop running state, and active state, then subscribe
to the new session and establish its authoritative active baseline in `applySubscribed`.
Old-session events cannot mutate the new selection because the stale subscription is
closed before the swap.

### Target-specific attachment capability

Every block build receives the intended loop ID. `submit` uses the reconciled current
active loop; `submitToLoop` uses its explicit loop ID (the focused loop in `Screen`). Both
call `Agent.AcceptsImages(target)` immediately before parsing attachments. Tests use two
loops with different capabilities and mutate one fake loop's model capability between
submissions, proving the query is target-specific and dynamic.

### Version and vendor

The rig lifecycle is the next breaking harness release after `v0.9.0`; the CLI plan targets `github.com/looprig/harness v0.10.0`. Do not merge the dependency bump until that tag contains the reviewed rig branch. Local execution may use CLI's existing `replace github.com/looprig/harness => ../harness`, but `go.mod` still records `v0.10.0` as the release contract.

Because CLI commits `vendor/`, `go mod tidy` and `go mod vendor` are mandatory. Verification must inspect both source and vendor: a green build against an old vendor copy is not evidence that the migration matches the new harness.

## Testing

- Table-drive the subscription/baseline handshake: transition before baseline, transition after baseline, same-active queued event, multiple queued transitions, and the `/clear` setup window.
- Table-drive per-loop running state and active display: running→idle selection, idle→running selection, and a terminal from the old active loop.
- Prove the transcript root remains stable while `activeLoopID` changes.
- Prove turn status responds to the active loop and ignores another loop.
- Prove the all-loop filter survives active selection changes without re-subscribing.
- Prove bar capping retains focused, active, and root loops in priority order.
- Prove `New` and successful `/clear` focus the then-active loop, while later selection does not move focus.
- Prove image acceptance is loop-targeted, fails closed for an unknown loop, and observes a model capability change.
- Update all CLI fake Agents to satisfy `RootLoopID` and `ActiveLoopID`; remove `PrimaryLoopID` from non-vendor Go source.
- Refresh vendor, then run unit, race, integration-tagged, security, and static build gates.

CLI unit tests cover only its adapter contract with fakes. The fresh-session and
switched-then-restored tests that prove `RootLoopID` derives from harness's durably first
zero-parent `LoopStarted` are **embedding-adapter acceptance tests** and belong in the
sibling `tests` repository (or the embedding application's migration), not in CLI. This
avoids importing a composition root into the presentation module.

## Non-goals

- Constructing a rig, loops, tools, stores, or workspaces in CLI.
- Adding snapshot controls or reacting to `SessionIdle` for persistence.
- Adding mode/model/effort controls to the TUI.
- Adding serve endpoints or an attachment registry.
- Implementing the SWE adapter or any SWE lifecycle migration.
- Redesigning replay/backlog ownership.

## Removal checks

After implementation, non-vendor CLI source must have no `PrimaryLoopID`, `session.Compile`, `session.Runner`, public `session.New/Restore`, `serve.Runner`, `CheckpointWorkspace`, `WithWorkspace*`, or checkpoint watcher. Vendor must likewise contain no removed harness lifecycle surface after the `v0.10.0` refresh.
