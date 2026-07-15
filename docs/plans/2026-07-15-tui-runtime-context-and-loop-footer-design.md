# TUI runtime context, controls, and loop footer — design

Date: 2026-07-15  
Status: approved design  
Scope: `harness` + `cli` + `swe`; no `sandbox` or `inference` API change

## Goal

Make the CLI show the runtime that actually governs the focused loop, keep every primer and
currently relevant delegate discoverable, and let the user change supported runtime settings
through the existing slash-command tray. The design must work for rigs other than SWE without
hard-coding SWE topology or assuming every rig offers runtime choices.

The surface is organized by ownership:

- The status line shows live turn state, elapsed time, the focused loop's effective model and
  effort, and that loop's latest measured context occupancy.
- The composer shows the focused loop's mode on its bottom padding row.
- The footer shows the rig name, session environment, all primers, and running delegates.
- Runtime mutation uses slash commands and the existing blue completion tray. No centered modal
  and no new keyboard shortcuts are part of this phase.

## Final layout

```text
● streaming… (25s · gpt-5.4 · high)                         ███░░░░░ 42%

│
│  Fix the flaky session reset test
│                                                               Plan/Build

│  SWE · Trusted ENV(/Users/ipotter/code/looprig) · ● planner (a1b2) · ○ builder (c3d4)
```

The composer has a three-row minimum: one top padding row, a one-row editor, and one bottom
padding row. Newlines grow only the editor region, preserving the top and bottom rows. The
existing editor cap remains in force; after the cap, the textarea scrolls internally.

The footer wraps at loop-segment boundaries when it cannot fit. Continuation rows retain the
gray rail and align with the first loop rather than repeating `SWE` and the environment:

```text
│  SWE · Trusted ENV(/Users/ipotter/code/looprig) · ● planner (a1b2) · ○ builder (c3d4)
│                                                     ○ reviewer (e5f6) · ○ operator (g7h8)
```

Long workspace roots first replace the user's home with `~`, then middle-elide. Loop display
names truncate before their four-character IDs. The footer never soft-wraps implicitly: layout
computes physical rows so Bubble Tea's inline renderer keeps an exact height.

## Visual and interaction language

- Active status keeps the existing lime-to-blue animation and filled `●`; idle keeps the subtle
  hollow `○`.
- The focused loop uses `●`; every other loop uses `○`.
- Loop segments use the established semantic-link hover: a slow blue glow and text-only
  underline. Clicking a loop focuses its transcript and calls the harness active-loop setter so
  subsequent default input targets the same loop.
- Model, effort, mode, and environment use the same link hover only when at least two valid
  choices exist. A single fixed value is muted and does not react to hover.
- Context occupancy is display-only and never receives link styling.
- The footer uses middle dots as separators. It does not use the completion tray's selected-row
  background because loop focus is already encoded by `●` and link hover.

Clicking an editable runtime value opens its corresponding slash-value tray. If the composer
already contains a draft, the interaction model saves it, temporarily presents the slash
command, and restores the draft after apply or Escape. A click must never destroy user input.

## Environment terminology

The CLI presents the sandbox ladder as:

| Internal sandbox value | User-facing label |
|---|---|
| `ZeroTrust` | `Untrusted` |
| `ReadOnly` | `Read Only` |
| `Write` | `Writable` |
| `Trusted` | `Trusted` |
| `Unconfined` | `Unconfined` |

`sandbox.Write` remains unchanged. The exported enum is defined in the separate `sandbox`
repository and used by the separate `swe` repository, so renaming it violates the requested
"only if it is not a multi-repository change" constraint. Configuration parsing may accept both
`write` and `writable`; the UI always renders `Writable`.

The environment setting is session-scoped. SWE's launch-time security ceiling is both the
initial value and maximum runtime value. The runtime choices are every permitted rung at or
below that cap. A session capped at `Writable`, for example, offers `Untrusted`, `Read Only`,
and `Writable`, but not `Trusted`. `Unconfined` is returned only by an adapter whose composition
root explicitly permits and acknowledges it. Per-role and parent clamps can remain more
restrictive than the session setting; changing the environment never bypasses those clamps.

## Runtime-choice discovery

The CLI must not infer choices from current values or hard-code provider/model names. The agent
adapter exposes two read-only snapshots and narrow mutation methods:

```go
type LoopRuntimeOptions struct {
    CurrentMode   loop.ModeName
    CurrentModel  inference.ModelKey
    CurrentEffort inference.Effort
    Modes         []ModeOption
    Models        []ModelOption
    Efforts       []inference.Effort
}

type EnvironmentRuntimeOptions struct {
    Current EnvironmentID
    Root    string
    Choices []EnvironmentOption
}
```

The concrete API uses typed IDs and defensive copies. A model selection sends a model key back
to the adapter; the adapter resolves that key to the trusted, secret-free descriptor from its
catalog before calling `loop.ChangeModel`. The CLI never manufactures a model descriptor from
typed text. Environment IDs are likewise adapter-defined typed values rather than imported
`sandbox.Mode`s, keeping the generic CLI independent of the sandbox module.

Following the CLI's interface-segregation rule, runtime discovery and mutation are focused
interfaces embedded by `tui.Agent`, not flags added to a large configuration object. There is
no `hidden/read-only/editable` permission mechanism. Choice count alone determines the UI:

- Zero choices: hide the field and command.
- One choice: show the fixed value where applicable, but provide no command, hover, or click.
- Two or more choices: show the interactive value and register its slash command.

Options are queried when a value tray opens, not cached indefinitely. The screen also refreshes
after focus, mode, model, or environment changes. A stale option can therefore be refused safely
by the adapter without producing an optimistic UI update.

### Modes

Harness already retains every bound mode, but public `loop.Handle` exposes only the current mode
and model. Add a separate read-only `loop.ModeCatalog` interface implemented by the concrete
session loop handle. It returns a defensive copy of selectable names from the bound definition;
it does not expose tools, prompts, or permission internals.

The unnamed base mode is labeled `Default`. A definition with no named modes presents no mode
field and no `/mode` command. Once a named mode exists, `Default` and all named modes are
selectable, so `/mode` has at least two entries. Other adapters may return a single fixed named
mode; the CLI displays it muted and still omits `/mode`.

Mode changes call the focused loop controller's `SetMode`. They take effect at the next turn
boundary and atomically restore that mode's model, effort, tools, tool limits, and instructions.
The status and composer update only from the committed reply/enduring event. Direct model or
effort changes override the selected mode's inference values until a later mode change resets
them; this phase does not add a separate “custom override” badge.

### Models

SWE derives selectable normal-turn models from its validated model catalog in stable order:

1. all `Standard` entries;
2. all `Premium` entries;
3. the focused loop's current model, inserted only when absent.

`Economy` remains title-generation-only and is not offered. Duplicate model keys collapse to
their first occurrence. The adapter returns only models compatible with that loop's fixed
transport/context binding and current runtime requirements. Runtime mutation resolves the
selected key back to the catalog descriptor and calls `loop.ChangeModel`; the harness remains the
final validator.

If the resulting list contains fewer than two choices, `/model` is absent and the status model
is non-interactive.

### Effort

The inference enum's syntactic validity does not prove that a model supports every value.
Effort choices therefore come from the adapter for the focused loop's current model. SWE uses
the model's `Caps.Thinking` plus its API format:

- no thinking capability or an unknown format: current/default only;
- OpenAI reasoning: Auto, Low, Medium, High (`Max` is omitted because the codec clamps it to
  High);
- Anthropic or Gemini thinking: Auto, Low, Medium, High, Max.

`Auto` maps to `inference.EffortNone`. The adapter may narrow this list for a specific model.
The CLI never assumes that the five dialect-neutral enum values are all meaningful. If fewer
than two effective choices remain, `/effort` is absent.

### Environment

SWE builds the environment choices from the session ceiling configured at the rig composition
root. The selected environment changes through `SessionController.SetSecurityCeiling`, is
journaled as `SecurityCeilingChanged`, and is restored with the session. `/env` is absent only
when the adapter returns fewer than two permitted values.

## Slash-command trays

The visible command catalog becomes dynamic. Existing commands remain:

```text
/clear
/compact
/exit
```

Runtime commands are added only when their focused scope has multiple choices:

```text
/model
/effort
/env
/mode
```

Typing or clicking a runtime command reuses the completion panel above the composer. It has two
states:

1. **Command state** filters command names and existing related-word aliases.
2. **Value state** begins after the command plus a space, filters that command's current choices,
   and uses the same selected blue rail, three-step background transition, mouse movement, and
   Up/Down navigation as today's `/` and `@` trays.

Examples:

```text
/mode b

│ plan       planning configuration
▌ build      implementation configuration
│ review     review configuration
```

Tab completes the selected row, preserving its current completion meaning. Enter applies an
exact or selected value. Escape closes the tray and restores a click-preserved draft. Runtime
values match case-insensitively by label and explicit aliases; commands retain their existing
related-word matching. Arbitrary fuzzy resolution never crosses the mutation boundary: the
adapter receives only the typed ID attached to a catalog row.

This phase adds no Tab/Shift+Tab mode switching and no Left/Right loop switching. Existing key
bindings remain unchanged. Keyboard shortcuts and centered modals are deferred.

## Authoritative screen state

The TUI keeps a per-loop runtime projection folded from enduring events:

- `LoopStarted`: display name, primer/delegate provenance, initial mode, model, limits, effort.
- `LoopModeChanged`: current mode plus its resolved model/limits/effort.
- `LoopInferenceChanged`: current model/limits/effort without changing the mode.
- `ContextMeasured`: latest authoritative input-token count and input limit.
- `LoopIdle` and turn lifecycle: delegate visibility and live state.
- `SecurityCeilingChanged`: current session environment.
- `ActiveLoopChanged`: harness active target, independent of presentation focus unless the user
  made the selection through the footer.

The latest `ContextMeasured` for the focused loop drives the right-aligned meter. Percentage is
`InputTokens / InputLimit`; the bar clamps its fill at 100% while the numeric label may exceed
100% because over-limit measurements remain valid audit evidence. A heuristic measurement is
prefixed with `~`. With no measurement or zero limit, the meter is omitted.

Runtime display changes only after an authoritative event or successful controller reply. A
failed command leaves the previous values intact and commits a faint typed-error notice.

## Status line

The focused loop's status line renders:

```text
● streaming… (25s · gpt-5.4 · high)                         ███░░░░░ 42%
```

Rules:

- Elapsed time appears only for a running focused loop, using the existing event/tick clock.
- Model is always shown when known. Effort is shown as `auto`, `low`, `medium`, `high`, or `max`.
- Separators collapse cleanly when elapsed or effort is absent.
- Model and effort receive link styling and hit regions only with multiple choices.
- Context is right-aligned. On narrow terminals, the meter drops blocks first and retains the
  numeric percentage; then effort drops before model. The lifecycle label is never removed.
- Active lifecycle text retains the existing animated gradient; runtime metadata stays stable so
  the whole line does not shimmer.

## Composer mode line

The composer uses the existing left accent rail on every physical row. The bottom padding row
right-aligns the focused loop mode:

```text
│
│  Type a message…
│                                                                  Build
```

No named modes means no label. A single fixed mode is muted. Multiple modes use link hover and a
click opens `/mode `. Multiline input grows between the two padding rows and leaves the mode
anchored at the bottom.

## Loop footer and topology bootstrap

Primers are `LoopStarted` roots: no spawning loop cause and no parent tool-use ID. Every primer
remains visible for the whole session. Delegates follow primers in creation order and remain
visible while running or gated. An idle focused delegate remains temporarily visible so its
transcript does not disappear beneath the pointer; after focus moves away, it leaves the footer.

The current adapter misses initial primer events in a fresh session because the rig creates loops
before the TUI subscribes and `ReplayBacklog` currently returns history only for restored
sessions. Extend the adapter's bootstrap replay to return already-durable public enduring events
for new sessions as well. A fresh session has no user turns before adapter construction, so this
replay supplies topology/runtime state without duplicating conversation content. Live delegates
continue through the single all-loops subscription.

The footer has no fixed loop-count cap. It creates another physical row when needed, as requested,
and the surface height budget subtracts every row before sizing the live tail. Mouse hit regions
are computed from the same wrapped layout used to render, preventing render/hit-test drift.

## Failure and concurrency behavior

- Catalog queries for an unknown/exited loop fail closed with no dynamic choices.
- A choice that becomes stale between rendering and Enter is revalidated by the adapter and
  harness; no optimistic state is applied.
- Mode/model/effort changes target the focused loop captured when the command opens. Changing
  focus while its tray is open closes and rebuilds the tray rather than silently retargeting.
- Environment changes are session-scoped and do not depend on focused loop.
- Tightening sandbox mode affects the next permission check immediately; an already-running OS
  command finishes under the policy with which it started. Loosening remains capped by the rig.
- Every asynchronous runtime change is context-bounded and returns a typed result message to the
  Bubble Tea update loop. The UI never blocks in `Update`.
- Restored events rebuild current mode/model/effort/environment/context before the live
  subscription attaches. Last durable change wins.

## Alternatives considered

### Hard-code choices in the CLI — rejected

This is simple initially but makes the generic CLI know SWE tiers, sandbox ordinals, and provider
effort quirks. It can offer invalid choices and cannot represent other rigs.

### Put all choices in enduring events — rejected

Events should preserve the runtime that actually executed, not mutable catalogs. Persisting every
available model or sandbox choice would make restore depend on stale configuration and bloat the
journal. Events remain authoritative for current state; the live adapter owns available choices.

### Centered runtime modal — deferred

A modal could avoid temporarily using the composer, but the current inline/native-scrollback
renderer does not own a stable full-screen canvas. The existing tray is already understood,
tested, mouse-aware, and compatible with the surface height budget.

## Repository scope and delivery order

1. **Harness:** add the narrow read-only mode-catalog capability on live loop handles and tests.
2. **CLI:** define typed runtime discovery/mutation interfaces; build projections, dynamic slash
   trays, status/composer/footer rendering, clicks, wrapping, and fakes.
3. **SWE:** implement the interfaces over the session controller, validated model catalog,
   effort mapping, and configured security cap; expand new-session bootstrap replay.
4. Publish/bump in dependency order and re-vendor generated dependency snapshots. Do not edit
   vendor copies by hand.

`sandbox.Write` is not renamed, so the sandbox repository is not part of this delivery. The
inference package already has model keys, context limits, capabilities, and effort values needed
by the adapter, so it also needs no API change.

## Testing

### Harness

- Mode catalog returns base and named modes in stable order.
- Returned slices are defensive copies.
- New, restored, primer, and delegate handles expose the same catalog.

### CLI

- Dynamic slash-command registration for zero/one/multiple choices.
- Nested value-tray filtering, selection, Tab completion, Enter apply, Escape, mouse movement,
  three-step background transition, and stale-choice refusal.
- Click-opened runtime tray preserves and restores a non-empty draft.
- Status formatting across idle/running/transient states, missing fields, narrow widths,
  clickable hit regions, and measured/heuristic/over-limit context.
- Three-row composer minimum, multiline growth, editor cap, mode alignment, and missing/fixed/
  interactive mode states.
- Multiple primers, live delegates, focused-idle delegate retention, wrapped footer rows,
  environment path elision, click focus+active mutation, and hover link animation.
- Cold bootstrap plus live subscription does not duplicate events.
- Fuzz width and arbitrary catalog labels to guarantee no wrap-induced height or hit-test drift.

### SWE

- Model choice ordering, deduplication, economy exclusion, compatibility filtering, and trusted
  key-to-descriptor resolution.
- Effort choices by thinking capability/API format, including OpenAI `Max` exclusion.
- Environment ladders for every cap, `Writable` display mapping, alias parsing, and rejection of
  levels above the cap.
- Mode/model/effort mutations target the requested loop; environment mutation targets the
  session; typed errors propagate unchanged.
- New and restored session bootstrap produces identical primer/runtime projections.

Every module runs its race suite. CLI additionally runs formatting, lint, security checks, and
render-width tests required by its repository policy before release commits.

## Deferred work

- Tab/Shift+Tab runtime cycling.
- Left/Right loop switching or a dedicated footer-focus mode.
- Center-screen modal pickers.
- “Send to all loops.”
- Per-field visibility/editability policy flags.
- A visual marker for direct model/effort overrides relative to the selected mode.
