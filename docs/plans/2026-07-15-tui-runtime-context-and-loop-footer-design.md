# TUI runtime status, controls, session browser, and loop footer design

Date: 2026-07-15  
Status: approved design  
Scope: `harness` + `tui` + `coderig`; no `confinement`, `sandbox`, or `inference` API change

## Goal

Show the runtime that actually governs the focused loop, keep every primer and relevant delegate
discoverable, let users change the choices their rig exposes, and let them resume previous sessions
without leaving the TUI.

The design remains useful for rigs other than CodeRig. The TUI does not hard-code CodeRig topology,
model names, access ordinals, or the assumption that every rig supports runtime mutation or session
history.

The surface is organized by ownership:

- The status line shows live turn state, elapsed time, the focused loop's effective model and
  effort, and its latest measured context occupancy.
- The composer shows the focused loop's mode on its bottom padding row.
- The footer shows the rig name, access level, workspace root, primers, and relevant delegates.
- Runtime mutation uses typed optional capabilities and the existing completion tray.
- Session discovery and resume use an optional process-scoped browser because the catalog and store
  outlive any individual agent.
- Presentation focus belongs to the TUI. It never changes the Harness active loop merely because a
  user views another loop.

The name "runtime status" is used instead of "runtime context" where practical because CodeRig
already uses runtime context to mean prompt material such as the date, working directory, and Git
state.

## Final layout

```text
● streaming… (25s · gpt-5.4 · high)                         ███░░░░░ 42%

│
│  Fix the flaky session reset test
│                                                               Plan/Build

│  CodeRig · Trusted · ~/code/looprig · ● planner (a1b2) · ○ builder (c3d4)
```

The composer has a three-row minimum: one top padding row, a one-row editor, and one bottom
padding row. Newlines grow only the editor region. The existing editor cap remains in force; after
the cap, the textarea scrolls internally.

The footer wraps at loop-segment boundaries. Continuation rows retain the accent rail and align
with the first loop instead of repeating the rig, access, and workspace:

```text
│  CodeRig · Trusted · ~/code/looprig · ● planner (a1b2) · ○ builder (c3d4)
│                                          ○ reviewer (e5f6) · ○ operator (g7h8)
```

Workspace roots replace the user's home with `~`, then middle-elide. Loop display names truncate
before their four-character IDs. Layout computes every physical row explicitly so Bubble Tea's
inline renderer keeps an exact height.

## Visual and interaction language

- Active status keeps the existing lime-to-blue animation and filled `●`; idle keeps `○`.
- The focused loop uses `●`; every other loop uses `○`.
- Loop segments use the established semantic-link hover: a slow blue glow and text-only underline.
- Clicking a loop changes only the TUI's focused transcript and submission target. It does not call
  `SessionController.SetActiveLoop`.
- Model, effort, mode, and access use link hover only when at least two valid choices exist. A
  single fixed value is muted and non-interactive.
- Context occupancy is display-only.
- The footer uses middle dots as separators and no selected-row background because focus is already
  encoded by `●` and hover.

Clicking an editable runtime value opens its corresponding slash-value tray. If the composer
contains a draft, the interaction model saves it, temporarily presents the command, and restores
the draft after apply or Escape.

## Access terminology

The user-facing concept is **access**, not environment. The selectable value changes the
session's security limit; it does not change the workspace, host, or execution provider.

CodeRig maps its confinement ladder as follows:

| Internal value | User-facing label |
|---|---|
| `ZeroTrust` | `Untrusted` |
| `ReadOnly` | `Read Only` |
| `Write` | `Writable` |
| `Trusted` | `Trusted` |
| `Unconfined` | `Unconfined` |

The existing internal `Write` name remains unchanged. Configuration parsing may accept both
`write` and `writable`; the TUI always renders `Writable`.

Access is session-scoped. CodeRig's launch-time security limit is the maximum runtime value. The
adapter exposes every permitted rung at or below that cap. A session capped at `Writable` offers
`Untrusted`, `Read Only`, and `Writable`, but not `Trusted`. `Unconfined` is returned only when the
composition root explicitly permits it. Per-role and parent clamps may remain more restrictive;
raising session access never bypasses them.

## Optional capabilities

The required `tui.Agent` interface remains unchanged. Runtime controls and session browsing are
optional capabilities beside it, so existing agents, fakes, and third-party adapters continue to
compile and rigs without choices keep a clean surface.

Following the package-boundary design, the canonical types live with the state machine in
`internal/presentation` and are aliased from the root `tui` facade. No new public package is
introduced, and `internal/presentation` never imports the root package.

Conceptually, the runtime capabilities are separated by responsibility:

```go
type RuntimeCatalog interface {
    LoopRuntimeOptions(context.Context, uuid.UUID) (LoopRuntimeOptions, error)
    AccessOptions(context.Context) (AccessOptions, error)
}

type RuntimeController interface {
    SetMode(context.Context, uuid.UUID, ModeID) error
    SetModel(context.Context, uuid.UUID, ModelID) error
    SetEffort(context.Context, uuid.UUID, EffortID) error
    SetAccess(context.Context, AccessID) error
}
```

An adapter may implement discovery without mutation. Choice count determines presentation:

- Zero choices: hide the field and command.
- One choice: show the fixed value where useful, but provide no command, hover, or click.
- Two or more choices plus mutation capability: show the interactive value and command.
- Two or more choices without mutation capability: show the current event-derived value as
  read-only.

Available choices and current state have different authorities. Catalog snapshots contain only
available options and their typed IDs. They do not contain `CurrentMode`, `CurrentModel`,
`CurrentEffort`, or current access. Current values are folded exclusively from enduring events.

Options are queried when a value tray opens and refreshed after focus or a committed runtime
change. A stale option is revalidated by the adapter and Harness. The TUI never applies an
optimistic runtime value.

### Modes

Harness retains every bound mode, but `loop.Handle` currently exposes only the effective mode and
model. Add a separate read-only `loop.ModeCatalog` capability implemented by native live handles.
It returns a defensive copy of selectable names from the bound definition and exposes no tools,
prompts, or permission internals.

The unnamed base mode is labeled `Default`. A definition with no named modes presents no mode
field or `/mode`. Mode changes call the focused loop controller's `SetMode`, take effect at a turn
boundary, and update the TUI only when the committed event arrives.

### Models

CodeRig derives selectable normal-turn models from its validated model catalog in stable order.
Title-generation-only models are excluded. Duplicate model keys collapse to their first
occurrence, and incompatible models are removed.

The TUI returns only the selected typed model ID. CodeRig resolves it to a trusted, secret-free
descriptor before calling `loop.ChangeModel`; the Harness remains the final validator. The TUI
never manufactures a descriptor from typed text.

### Effort

Effort choices come from the adapter for the focused loop's current model. CodeRig maps thinking
capability and provider format to the effort values the provider actually supports. The TUI does
not assume every dialect-neutral enum value is meaningful.

### Access

CodeRig builds access choices from the configured session cap. Selection calls
`SessionController.SetSecurityLimit`, is journaled as `SecurityLimitChanged`, and restores with
the session. The generic TUI receives adapter-defined `AccessID` values and does not import
sandbox or confinement policy types.

## Slash-command trays

The visible command catalog becomes dynamic. Existing commands remain:

```text
/clear
/compact
/exit
```

Runtime commands appear only when their scope is editable:

```text
/model
/effort
/access
/mode
```

`/sessions` appears when a session browser is configured. `/resume` is an exact synonym and a
related search term; only `/sessions` needs to occupy a row in the command tray.

Runtime commands reuse the completion panel above the composer:

1. **Command state** filters command names and related-word aliases.
2. **Value state** begins after the command plus a space, filters current choices, and reuses the
   selected background, animation, mouse movement, and Up/Down navigation of today's trays.

Tab completes the selected row. Enter applies the exact or selected typed value. Escape closes
the tray and restores a click-preserved draft. Arbitrary fuzzy text never crosses the mutation
boundary.

This phase adds no Tab/Shift+Tab runtime cycling, Left/Right loop switching, or centered modal.

## Authoritative screen state

The TUI folds a per-loop runtime projection from enduring events:

- `LoopStarted`: name, primer/delegate provenance, initial mode, model, limits, and effort.
- `LoopModeChanged`: effective mode, model, limits, and effort.
- `LoopInferenceChanged`: effective model, limits, and effort without changing mode.
- `ContextMeasured`: latest input-token count and input limit.
- Loop and turn lifecycle: live state and delegate visibility.
- `SecurityLimitChanged`: current session access.

`ActiveLoopChanged` does not move presentation focus. The initial `Agent.ActiveLoopID()` may seed
the first focus, after which explicit TUI navigation owns it. Focused submissions already use
`SubmitToLoop`, so local focus needs no Harness mutation.

The latest `ContextMeasured` for the focused loop drives the right-aligned meter. Percentage is
`InputTokens / InputLimit`; the bar clamps at 100% while the numeric value may exceed 100%. A
heuristic measurement is prefixed with `~`. With no measurement or zero limit, the meter is
omitted.

Runtime display changes only after authoritative events. A failed command leaves previous values
intact and commits a faint typed-error notice.

## Status line

```text
● streaming… (25s · gpt-5.4 · high)                         ███░░░░░ 42%
```

- Elapsed time appears only for a running focused loop.
- Model is shown when known. Effort renders as `auto`, `low`, `medium`, `high`, or `max`.
- Separators collapse when elapsed or effort is absent.
- Model and effort receive hit regions only with multiple editable choices.
- On narrow terminals, context blocks drop before the numeric percentage, then effort drops
  before model. Lifecycle text is never removed.
- Runtime metadata remains stable while lifecycle text animates.

## Composer mode line

The composer uses the existing accent rail on every physical row. The bottom padding row
right-aligns the focused loop mode:

```text
│
│  Type a message…
│                                                                  Build
```

No named modes means no label. A fixed mode is muted. Multiple editable modes use link hover and
click to open `/mode `. Multiline input grows between the two padding rows.

## Loop footer and fresh-session bootstrap

Primers are `LoopStarted` roots with no spawning-loop cause and no parent tool-use ID. Every
primer remains visible for the session. Delegates follow primers in creation order and remain
visible while running or gated. A focused idle delegate remains visible until focus moves away.

Fresh CodeRig sessions create their primers before the TUI subscribes, while
`sessionadapter.New` intentionally has no replay backlog. Add a generic replay-aware constructor,
conceptually `sessionadapter.NewWithReplay`, that cold-replays already-durable public enduring
events for a newly created session. `New` may retain its no-backlog behavior for simple/headless
consumers. CodeRig uses the replay-aware constructor for both new and restored TUI sessions.

This supplies topology and runtime state without CodeRig-specific behavior in presentation. Live
events continue through the single all-loops subscription. Cold replay completes before live
attachment, and boundary tests prove no event is lost or folded twice.

The footer has no fixed loop-count cap. It adds physical rows as needed, and the surface budget
subtracts every row before sizing the live tail. Render and mouse hit regions use the same wrapped
layout.

## Session browser

Session browsing belongs to a process-scoped optional capability because the shared store and its
catalog outlive the current `Agent`:

```go
type SessionBrowser interface {
    ListSessions(context.Context) ([]SessionSummary, error)
    ResumeSession(context.Context, SessionID) (Agent, error)
}
```

The exact API may keep new-session opening separate through the existing `OpenAgent`. A backwards-
compatible TUI option supplies `SessionBrowser`; it is not embedded in `Agent`.

CodeRig implements the browser over `SessionStoreFactory`. It maps Harness `SessionMeta` into a
generic summary containing session ID, title, lifecycle state, created time, last-active time,
agent kind, loop count, and configuration compatibility. Listing reads the replay-free catalog
and remains most-recently-active-first.

### Session tray

`/sessions` and `/resume` open the same tray. The current session is excluded. Each previous
session uses two content rows with one blank padding row between records:

```text
│
│  Fix persistence race                            idle · 12m ago
│  CodeRig · 3 loops · created Jul 15 · 8f2a1c3d
│
│  Build ACP foreign-loop support                 failed · 2h ago
│  CodeRig · 5 loops · created Jul 14 · 31c90e72
│
│  Document runtime access controls              stopped · 1d ago
│  CodeRig · 3 loops · created Jul 13 · a82db146
│
```

The accent rail is uninterrupted and visually independent of the records. There are no boxes or
corner glyphs. Typography, padding, and the selected background distinguish records.

- The first row shows the title, lifecycle state, and relative last activity.
- The second shows rig/agent kind, loop count, creation date, and a short session ID.
- Long titles truncate before state and recency. Metadata drops in reverse importance on narrow
  terminals; the session ID remains available to filtering even when hidden.
- Up and Down move by session, not physical row. Mouse hover and click treat both content rows as
  one item. The selected background covers the two content rows, not the blank padding.
- The visible window is measured in records. An eight-content-row budget shows four sessions plus
  their inter-record padding as the surface budget permits.
- Typing filters title, full or short ID, state, and rig name. Escape cancels without changing the
  current session.
- Empty catalogs show `No previous sessions`. Listing failures produce a non-fatal notice and
  leave the current session untouched.

### Session switching

Selecting a session is an explicit ownership handoff:

1. If the current session is idle, begin the close-and-resume handoff immediately.
2. If it is running, compacting, or waiting on a gate, request a session interrupt and show
   `interrupting…`.
3. Wait for the authoritative terminal event, then show `resuming…`.
4. Close the current session before opening the selected one so exclusive workspace resources can
   transfer safely.
5. Restore the selected backlog, rebuild all projections, then attach live events.

An interrupt refusal cancels the switch and leaves the current session open. Once close begins
there is no rollback: as with `/clear`, a close or restore failure is terminal because ownership
of the old session has already been released. In-flight queued input and open gates are cancelled
by the explicit interrupt-and-close operation.

A configuration mismatch is surfaced before handoff when the catalog can determine it. This
design does not silently opt into `AllowConfigMismatch`; a future explicit action may do so.

## Failure and concurrency behavior

- Catalog queries for unknown or exited loops fail closed with no dynamic choices.
- Stale runtime selections are revalidated by the adapter and Harness.
- Mode/model/effort changes target the loop captured when the command opens. Focus changes close
  and rebuild the tray rather than silently retargeting it.
- Access changes are session-scoped.
- Tightening access affects the next permission check. An already-running OS command finishes
  under the policy with which it started. Loosening remains capped by the rig.
- Every asynchronous query, mutation, interrupt, and handoff is context-bounded and reports a
  typed Bubble Tea message. `Update` never blocks.
- Cold history rebuilds mode, model, effort, access, context, and topology before live attachment.

## Alternatives considered

### Embed runtime and session methods in `Agent` — rejected

This would force every agent, fake, and third-party adapter to implement capabilities the rig may
not possess. Session browsing also has process lifetime, not current-agent lifetime.

### Put current values in option catalogs — rejected

It creates two authorities and allows query results to disagree with durable events. Events own
current state; catalogs own only available choices.

### Hard-code choices in the TUI — rejected

It would make the generic TUI know CodeRig models, access ordinals, and provider effort quirks.

### Mutate Harness active loop on footer focus — rejected

Focused submission already targets a loop explicitly. A viewing action should not change the
session-wide default used by other clients.

### Put session browsing on CodeRig alone — rejected

The store implementation belongs to CodeRig, but the interaction is a reusable optional TUI
capability that other rigs can implement.

### Box every session record — rejected

Boxes and corner marks compete with the tray's continuous accent rail. Blank padding, two-line
typography, and a two-row selection background provide enough grouping with less visual noise.

### Centered modal — deferred

The existing tray is understood, tested, mouse-aware, and compatible with the inline surface
height budget.

## Delivery phases

### Phase 1: authoritative visibility

1. Harness mode catalog.
2. Optional TUI runtime contracts and pure event reducer.
3. Generic new-session bootstrap replay in `sessionadapter`.
4. Read-only status metadata, context meter, composer mode, and wrapped loop footer.

Phase 1 is independently releasable and validates the read model before mutations.

### Phase 2: controls and navigation

1. Dynamic command and value trays.
2. CodeRig runtime catalogs and typed mutations for mode, model, effort, and access.
3. Runtime clicks, hover, draft preservation, and stale-choice handling.
4. Optional session browser, two-line session tray, and interrupt-then-resume handoff.

Publish and bump modules in dependency order. Generated vendor snapshots are re-vendored and
never edited by hand.

## Testing

### Harness

- Mode catalog returns base and named modes in stable order with defensive copies.
- New, restored, primer, and delegate handles expose the same catalog.

### TUI

- Optional capability detection leaves existing agents unchanged.
- Current runtime values come only from event folding; option catalogs contain no current value.
- Dynamic commands cover zero, one, and multiple choices.
- Value trays cover filtering, selection, Tab, Enter, Escape, mouse, animation, stale choices, and
  click-preserved drafts.
- Status covers idle/running/transient states, missing values, narrow widths, hit regions, and
  measured, heuristic, and over-limit context.
- Composer covers three-row minimum, multiline growth, cap, mode alignment, and fixed/editable
  modes.
- Footer covers multiple primers, delegates, focused-idle retention, wrapping, local-only focus,
  workspace elision, and hover.
- Bootstrap plus live subscription has neither gaps nor duplicates.
- Session tray covers two content rows, blank padding, continuous rail, record-based navigation,
  filtering, empty/error states, mouse hit regions, and narrow terminals.
- Session switching covers idle handoff, running/gated interrupt, terminal-event wait, refusal,
  close failure, restore failure, stale results, and shutdown during handoff.
- Fuzz arbitrary widths and catalog labels to prevent implicit wrapping or hit-test drift.

### CodeRig

- Model ordering, deduplication, filtering, and trusted ID-to-descriptor resolution.
- Effort choices by capability and provider format.
- Access ladders for every cap, `Writable` display mapping, aliases, and above-cap rejection.
- Mode/model/effort mutations target the captured loop; access targets the session.
- New and restored bootstrap produce equivalent runtime and topology projections.
- Session browser maps catalog metadata, excludes the current session, preserves recent-first
  order, and refuses incompatible or unavailable restores without silent override.

Every module runs its race suite. TUI additionally runs formatting, lint, security, and render-
width checks required by its repository policy.

## Deferred work

- Tab/Shift+Tab runtime cycling.
- Left/Right loop switching or dedicated footer-focus mode.
- Center-screen modal pickers.
- Sending one prompt to multiple loops.
- Per-field visibility/editability policy flags.
- A marker for direct model/effort overrides relative to the selected mode.
- Explicit config-mismatch override from the session tray.
