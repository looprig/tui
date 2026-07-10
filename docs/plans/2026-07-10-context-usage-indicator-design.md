# Context-window usage indicator — investigation + design

Status: **spec (to refine)** · Date: 2026-07-10 · Branch: `stage2-loop-messaging`

## Goal / use case

Show the user how full the model's context window is, live in the **modern viewport**:
the **percent of the context window used** plus the **used / window token counts**,
derived from the model's context-window length.

Proposed placement — append to the existing status line (`tui/modern.go:868 modernStatusLine`),
after the elapsed suffix:

```
● streaming… (12s) · 46% · 92k/200k
```

Placement and format are a **proposal to refine** (right-aligned segment on the status
row is an alternative; `92k/200k` vs `92,000/200,000` vs a bar are all open).

---

## Findings — what is reachable today

### The CLI's two windows onto the data

1. **`tui.Agent`** (`tui/agent.go`) — the narrow interface the TUI drives.
   Methods: `Submit`, `SubmitToLoop`, `PrimaryLoopID`, `Interrupt`, `Close`,
   `AcceptsImages`, `Subscribe`, `ReplayBacklog`, `Approve`, `Deny`, `ProvideAnswer`.
   There is **no token / usage / context method**. `AcceptsImages() bool` is the *only*
   model-capability the interface exposes — it is the precedent this design mirrors.

2. **The event stream** (`vendor/github.com/looprig/harness/pkg/event/*.go`, consumed via
   `Agent.Subscribe`). A grep of the whole package for
   `token|usage|contextwindow|inputtoken|outputtoken|cost` finds **zero** count-bearing
   fields. The relevant events:
   - `TokenDelta{ Chunk content.Chunk }` (`turn.go:108`) — streaming delta, **no counts**.
     `content.Chunk` is `TextChunk` / `ThinkingChunk` / `ToolUseChunk`
     (`vendor/.../core/content/chunk.go`) — none carry usage.
   - `TurnDone{ Message *content.AIMessage }`, `StepDone{ Messages }`, `TurnStarted`, … —
     none carry token usage. `content.AIMessage` (`.../core/content/message.go`) has no
     usage field either.

**Neither number reaches the CLI today.** But the two halves have very different distance:

### (a) Context-window LENGTH — reachable upstream, trivially

- It is `inference.Capabilities.MaxContext int`
  (`/Users/ipotter/code/looprig/inference/capabilities.go:7`), carried on every model as
  `loop.Config.Model.Caps.MaxContext`.
- Populated per-model in the swe catalog via `inference.WithMaxContext(128_000)`
  (`/Users/ipotter/code/looprig/swe/swarms/swe/catalog_models.go:60,72`) — a **real value**,
  not zero.
- The concrete agent already reads the sibling field: `sessionAgent` captures
  `primary.Model.Caps.AcceptsImages` at construction
  (`/Users/ipotter/code/looprig/swe/swarms/swe/agent.go:78,98,118,199`). `MaxContext` sits in
  the same `Caps` struct — surfacing it is a one-line mirror of `AcceptsImages()`.

### (b) USED tokens (running context size) — NOT captured anywhere reachable

- The domain type exists: `inference.Usage{ InputTokens, OutputTokens int }`
  (`/Users/ipotter/code/looprig/inference/client.go:57-61`).
- It is decoded **only on the non-streaming `Invoke` path** — `Response.Usage`
  (`inference/client.go:44-48`), built in `codec/anthropicapi/decode.go:28-44` (and the
  OpenAI/Gemini decoders).
- **The harness loop never calls `Invoke`.** It drives the model via the **streaming** path:
  `loop/turn.go` streams an `inference.StreamReader[content.Chunk]` (`.../harness/pkg/loop/turn.go:432`),
  which yields only `content.Chunk`. The streaming codec **discards usage** —
  `codec/anthropicapi/stream.go` has no usage handling (the Anthropic SSE `message_start` /
  `message_delta` usage fields are dropped at the frame→chunk mapping).
- Consequently the harness retains no `Usage` (grep `harness/pkg` for `.Usage` → none),
  **no event carries it**, and **no session accessor exposes it**.

### Feasibility verdict

**The percentage cannot be computed from the CLI today.** The context-window *length* is one
thin method away (mirror of `AcceptsImages`), but the *used-token* numerator is not captured
anywhere the CLI (or even the harness) can currently read — the streaming inference path throws
provider usage away before the harness sees it. **A new seam is required for the numerator.**

---

## Design

### Path A — IF the data were reachable now (window only; insufficient alone)

Add the window to the agent surface, mirroring `AcceptsImages`:

```go
// tui/agent.go
ContextWindow() int // 0 when the model advertises no MaxContext
```

```go
// swe/swarms/swe/agent.go — captured at construction like acceptsImages
contextWindow: primary.Model.Caps.MaxContext,
func (a *sessionAgent) ContextWindow() int { return a.contextWindow }
```

This alone gives the **denominator** but no numerator, so it can only render `…/200k`, not a
percent. It is a prerequisite of Path B, not a solution.

### Path B — the numerator seam (required)

Two ways to get "used tokens". They are not exclusive — B2 can ship first as a rough gauge and
be replaced by B1 later.

#### B1 — authoritative provider usage (clean, cross-repo)

Thread the real usage the provider already sends but the stream currently drops:

1. **inference** — `StreamReader[content.Chunk]` gains a terminal accessor, e.g.
   `Usage() *inference.Usage`, populated from the final SSE usage frame after the stream
   drains (the data already arrives on the wire; the anthropic/openai/gemini stream decoders
   stop discarding it). *Touch: `inference/stream.go`, `codec/*/stream.go`.*
2. **harness** — `loop/turn.go` reads `sr.Usage()` after the step's stream completes and
   retains the **last turn's** usage on the loop/session. Surface it either as
   (i) a new **Enduring** event `TurnUsage{ TurnIndex; InputTokens, OutputTokens int }` on the
   fan-in (the CLI already subscribes — it would accumulate with zero new plumbing), or
   (ii) a field on `TurnDone`, or (iii) session state read by a query method. *Touch:
   `harness/pkg/loop/turn.go`, `pkg/session/session.go`, optionally `pkg/event`.*
3. **swe** — `sessionAgent` delegates a `ContextUsage(loopID)` to the session, exactly as it
   delegates `SubmitToLoop` (`agent.go:136`). *Touch: `swe/swarms/swe/agent.go`.*
4. **cli** — new interface method:
   ```go
   // tui/agent.go
   ContextUsage(loopID uuid.UUID) (used, window int, ok bool)
   ```
   `ok=false` when the model has no `MaxContext` or no turn has completed yet (render nothing).
   If step 2 chose the event route, the CLI instead reads a `TurnUsage` field off its own
   accumulated state and only needs `ContextWindow()` from the agent.

   **What "used" means:** the truest "how full is context right now" is the **input tokens of
   the most recent completed turn's request** — the provider counted the actual prompt (system
   prompt + tool schemas + full message thread) just before replying, and it is already
   post-compaction. Cumulative sums or "input+output of every turn" over-count and drift after
   any context trimming. Recommend `used = last turn InputTokens` (optionally `+ OutputTokens`
   of that turn to reflect the reply now in-thread).

#### B2 — client-side estimate (zero upstream change, approximate)

The CLI already holds the full committed transcript (StepDone/TurnDone messages +
`ReplayBacklog`). Estimate tokens locally over the focused loop's thread (~`chars/4` heuristic)
plus a fixed system/tool overhead constant. Cheap, no cross-repo work, updates live — but it is
an **approximation**: it omits the real tokenizer, prompt-cache accounting, and exact
system/tool-schema sizing, so it can diverge materially. Render it prefixed `~` (`~46%`).

### Rendering (both paths)

`ModernScreen` has `m.agent` in scope. Extend `modernStatusLine()` (`tui/modern.go:873`) to
append ` · <pct>% · <used>/<window>` after the elapsed suffix when `ok` (else omit). Use the
existing faint `styles.StatusStyle`. Humanize counts (`92k/200k`). Hide entirely when
`window == 0` (unknown) — fail quiet.

---

## Open questions for the user

1. **Per-loop vs whole-session context?** In a multi-loop (subagent) session each loop can run a
   different model → different `MaxContext`. `sessionAgent` captures only the **primary's**
   caps today. Does the indicator follow the **focused loop** (needs per-loop window+usage) or
   always show the **primary/operator** loop?
2. **What is "used"?** Last completed turn's `InputTokens` (recommended — the real current
   context size, post-compaction) vs cumulative vs including that turn's `OutputTokens`?
3. **Authoritative (B1) or estimate (B2)?** Ship the rough client-side estimate now, or wait
   for real provider usage threaded through inference→harness→swe? (They can layer.)
4. **Update cadence.** Real usage is only known at **turn end** (`TurnDone`); the estimate could
   update **live** during streaming. Is a per-turn refresh acceptable, or is a live-growing
   count wanted while a turn streams?
5. **Format / placement.** Inline status-line suffix (`· 46% · 92k/200k`) vs right-aligned
   segment vs a small bar? `k` humanization vs full numbers? Show percent only, or both?
6. **Unknown / zero window.** Confirm: when the model advertises no `MaxContext`, hide the
   indicator entirely (proposed) rather than showing a bare token count.
