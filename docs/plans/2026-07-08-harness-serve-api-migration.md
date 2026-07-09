# Migration: `cli` → updated `github.com/looprig/harness` (event.Delivery + gate RespondGate)

**Status:** ready to execute (spec only — no code changed yet)
**Date:** 2026-07-08
**Owner module:** `github.com/looprig/cli` (TUI under `cli/tui/`)
**Depends on:** harness API change (landed) and swe's own migration (see Dependency Ordering)

---

## 1. Context

harness reshaped its session / event surface. Three changes ripple outward:

1. **The live subscription channel now carries `event.Delivery`, not `event.Event`.**
   `event.Subscription.Events()` returns `<-chan event.Delivery`, where
   `event.Delivery = struct{ Event event.Event; JournalSeq uint64 }`. `JournalSeq`
   rides only the live delivery path (0 for Ephemeral, the append seq for Enduring).
   Every consumer that reads/ranges `sub.Events()` must now unwrap `d.Event`.

2. **The legacy gate trio was deleted from `session.Session`.** `Session.Approve`,
   `Session.Deny`, and `Session.ProvideUserInput` are gone, replaced by a single
   `Session.RespondGate(ctx, gate.GateResponse)`.

3. **harness archived its `pkg/transcript` tree (2026-07-09).** `pkg/transcript`,
   `pkg/transcript/html`, and `pkg/transcript/journalsource` were moved out of harness
   to `../archive/transcript/` (19 files) and `github.com/yuin/goldmark` was dropped
   from harness — the tree had **no in-harness consumer**; only cli imports it
   (cross-module). harness no longer provides these packages, so cli's `go mod tidy` /
   `go mod vendor` (§4) will FAIL until cli **absorbs the tree in-house** under
   `cli/transcript` (relocation in §4.1). This is independent of the two API changes
   above but must land in the same cli migration, before the vendor refresh.

**Key finding — the gate change does NOT reach cli's own surface.** cli does not
call the harness session's gate methods. cli defines its own `tui.Agent` interface
(`cli/tui/agent.go`) with cli-owned method names `Approve` / `Deny` /
`ProvideAnswer`; the concrete implementation lives in **swe**
(`swe/swarms/swe/agent.go` `*sessionAgent`), which imports `github.com/looprig/cli/tui`.
The dependency direction is **swe → cli** (cli is the leaf; cli's `go.mod` has no
`looprig/swe`). So swe absorbs the entire `RespondGate` migration *internally* and
keeps satisfying cli's unchanged interface. cli's only real breaks are the
`event.Delivery` channel change and the stale vendored harness copy.

Verified API shapes (against current harness):
- `harness/pkg/event/event.go:122` — `Subscription.Events() <-chan Delivery`
- `harness/pkg/event/event.go:132` — `type Delivery struct { Event Event; JournalSeq uint64 }`
- `harness/pkg/gate/response.go:24` — `type GateResponse struct { GateID ID; Action string; Values map[string]json.RawMessage; Source ResponseSource }`
- `harness/pkg/session/gates.go:404` — `func (s *Session) RespondGate(ctx, gate.GateResponse) error` (the trio is absent)
- `harness/pkg/hub/subscription.go:20` — `SubscriptionLossError` still exists, unchanged (cli's `screen_test.go` import stays valid)

---

## 2. API deltas relevant to cli

| Old (harness) | New (harness) | cli impact |
|---|---|---|
| `Subscription.Events() <-chan event.Event` | `Subscription.Events() <-chan event.Delivery` | **Breaks** `tui/commands.go:72` and the `fakeSubscription` test double. |
| `event.Delivery` did not exist | `event.Delivery{ Event event.Event; JournalSeq uint64 }` | New unwrap point; cli keeps `eventMsg{ev event.Event}` by unwrapping `.Event` at the boundary. |
| `Session.Approve/Deny/ProvideUserInput(...)` | `Session.RespondGate(ctx, gate.GateResponse)` | **No cli change.** Handled entirely inside swe (see §3). cli's `tui.Agent.Approve/Deny/ProvideAnswer` names and signatures are unchanged. |
| `Session.SubscribeEvents(...) *hub.EventSubscription` | `Session.SubscribeEvents(...) (event.Subscription, error)` | **No cli change.** cli already types the handle as `EventStream = event.Subscription` (`tui/agent.go:19`) and never calls `SubscribeEvents` directly (that call lives in swe). Only the channel *element* type changes. |
| `harness/pkg/transcript{,/html,/journalsource}` (importable from harness) | **removed from harness** — archived to `../archive/transcript/`; `goldmark` dropped from harness | **Breaks every cli import of the tree** (`tui/export.go`, `tui/agent.go`, + `tui/screen_test.go`, `tui/export_test.go`, `cli/run_test.go`). cli must absorb the package under `cli/transcript` and take ownership of `goldmark` (§4.1). |

cli's `tui/agent.go:19` alias `type EventStream = event.Subscription` needs **no
edit** — the interface identity is unchanged; only `Events()`'s element type moved
from `event.Event` to `event.Delivery`, which propagates automatically.

---

## 3. Dependency ordering

Because **swe imports cli** (not the reverse), the leaf-first build order is:

1. **harness** — DONE (source of the change).
2. **cli** — can migrate independently and FIRST; it has no `looprig/swe` dependency.
   Its only coupling to the change is (a) the `event.Delivery` channel and (b) the
   vendored harness copy. After this migration cli must `go build ./...` and
   `go test -race ./...` clean on its own.
3. **swe** — migrates *after* cli, because swe replaces both harness and cli locally
   (`swe/go.mod`: `replace github.com/looprig/cli => ../cli`). swe must:
   - Reimplement `sessionAgent.Approve/Deny/ProvideAnswer`
     (`swe/swarms/swe/agent.go:230,237,244`) via `session.RespondGate(...)` — the old
     bodies call the deleted `a.session.Approve/Deny/ProvideUserInput` and will not
     compile. Mapping mirrors the harness test helpers
     (`harness/pkg/session/gates_test.go:733-757`):
     - approve → `GateResponse{GateID, Action:"approve", Values:{"scope": scope}, Source:{Kind:"user"}}`
     - deny → `GateResponse{GateID, Action:"deny", Source:{Kind:"user"}}`
     - answer → `GateResponse{GateID, Action:"answer", Values:{"answer": answer}, Source:{Kind:"user"}}`
   - Unwrap `d.Event` in its own subscription reader
     (`swe/swarms/swe/persistence.go:349` — `for ev := range sub.Events()` where
     `ev.(event.SessionIdle)` will otherwise fail to type-assert an `event.Delivery`).

**cli's dependency on swe:** cli does **not** depend on swe exposing *new* reply
methods. cli depends only on swe *continuing to satisfy the unchanged*
`tui.Agent` interface (`Approve` / `Deny` / `ProvideAnswer` with their existing
signatures). No new method is added to the interface by this migration.

---

## 4. FIRST cli step — absorb the archived transcript layer, then refresh the vendored harness

### 4.1 Absorb the transcript tree (harness archived it — do BEFORE the vendor refresh)

harness moved `pkg/transcript{,/html,/journalsource}` out to `../archive/transcript/`
(19 files) and dropped `goldmark`; cli is now its only consumer. Bring the tree
in-house under `cli/transcript` so cli owns the package (this is why §4.2's
`go mod tidy`/`vendor` can even resolve — without it they fail on the missing
`harness/pkg/transcript`).

```sh
cd /Users/ipotter/code/looprig/cli
cp -R ../archive/transcript ./transcript

# Rewrite the tree's OWN self-references AND every cli consumer to the new path.
# The tree's harness/core type imports — pkg/{command,event,identity,journal,tool},
# core/{content,uuid} — are deliberately NOT touched; they stay pointing at
# harness/core (the dependency stays one-way cli → harness).
find ./transcript ./tui ./cli -name '*.go' -type f -exec sed -i '' \
  -e 's|github.com/looprig/harness/pkg/transcript|github.com/looprig/cli/transcript|g' \
  {} +
```

Verify (expect NO output): `grep -rn "looprig/harness/pkg/transcript" . --include='*.go' | grep -v /vendor/`

Consumers rewritten by the `sed`: production `tui/export.go` (transcript + `/html` +
`/journalsource`), `tui/agent.go`; tests `tui/screen_test.go`, `tui/export_test.go`,
`cli/run_test.go`. API surface preserved by the move:
`transcript.{Loop,Session,Reconstruct,RecordSource,SystemPromptResolver,CommitError,CommitNotice,ReconstructError}`,
`html.{Render,RenderError}`, `journalsource.ExportUnavailableError`.

cli now owns the CommonMark renderer, so **add the `github.com/yuin/goldmark` entry
back to cli's `CLAUDE.md`** approved-deps list — Task 1 Step 6 of the
console-extraction plan (`../harness/docs/plans/2026-07-02-looprig-console-extraction-plan.md`)
had removed it; that reversal is now warranted. Copy the rationale verbatim from
harness's old entry, including the raw-HTML-disabled / `template.HTML` XSS note.
`go mod tidy` (§4.2) then promotes `goldmark` from an indirect to a direct require.

### 4.2 Refresh the vendored harness

cli builds harness via `replace github.com/looprig/harness => ../harness`
(`cli/go.mod:101`) **and** carries a committed `vendor/` tree. With a `vendor/`
directory present, Go builds in vendor mode, so the *vendored* copy — not
`../harness` directly — is what compiles. That copy is **stale**: verified
`cli/vendor/github.com/looprig/harness/pkg/event/event.go:122` still declares
`Events() <-chan Event` and has no `Delivery` type. Until it is regenerated, cli
will compile the old API and the source edits below will not match reality.

**Run in the cli module root, before touching any consumer:**

```sh
cd /Users/ipotter/code/looprig/cli
go mod tidy
go mod vendor
```

This re-vendors harness (and the other locally-replaced modules — core, inference,
bubbletea fork) from their `replace` targets, bringing `event.Delivery` into
`cli/vendor/...`. Only after this do the consumer edits in §5 compile.

> Note: swe (`../swe`) is **not** a cli dependency and is not vendored by cli, so
> the cli vendor refresh is independent of swe's migration state. But do not merge
> the whole stack until swe is migrated too, or swe will fail to build against the
> updated cli+harness.

---

## 5. File-by-file change list (cli)

### 5.1 `tui/commands.go` — unwrap `Delivery` in `subNext` (line ~72)

The only production break. Keep `eventMsg{ev event.Event}` unchanged; unwrap at the
channel boundary.

BEFORE (`tui/commands.go:71-76`):
```go
		ev, ok := <-sub.Events()
		if !ok {
			return subClosedMsg{err: sub.Err()}
		}
		return eventMsg{ev: ev}
```

AFTER:
```go
		d, ok := <-sub.Events()
		if !ok {
			return subClosedMsg{err: sub.Err()}
		}
		return eventMsg{ev: d.Event}
```

(`d.JournalSeq` is intentionally dropped — the TUI has no use for the durable
sequence today. If a future need arises, thread it onto `eventMsg`; not in scope
here.)

No other production `.Events()` call sites exist in cli — verified
`rg -n "\.Events\(\)" cli/ --glob '!vendor/'` returns only `tui/commands.go:72`.

### 5.2 `tui/messages.go` — no change

`eventMsg struct{ ev event.Event }` (line 12) stays `event.Event`; the unwrap in
5.1 preserves it. `subscribedMsg`/`EventStream` (lines 14-19) unchanged.

### 5.3 `tui/agent.go` — no change

`EventStream = event.Subscription` (line 19) and the `Agent` interface
(`Approve`/`Deny`/`ProvideAnswer`, `Subscribe`, `ReplayBacklog`) are all unchanged.

### 5.4 Gate-reply path (`tui/commands.go` approve/deny/provideAnswer cmds, `tui/screen.go`, `tui/interaction.go`) — no change

`approveCmd`/`denyCmd`/`provideAnswerCmd` (`tui/commands.go:114-146`) call
`agent.Approve/Deny/ProvideAnswer` on the cli-owned interface. The interface,
the call sites, the `uiApprove`/`uiDeny` action plumbing, and the
gateApproved/gateDenied transcript verbs are all untouched. All `RespondGate`
translation happens inside swe (§3).

---

## 6. Test-update list (cli)

### 6.1 `tui/screen_test.go` — `fakeSubscription` must carry `event.Delivery` (BREAKS)

This is the only test break. `fakeSubscription` is the single shared subscription
double for the whole `tui` package (used by `commands_test.go`, `tool_handoff_test.go`,
`strand_integration_test.go`, and `screen_test.go` itself). Its compile-time
assertion `var _ event.Subscription = (*fakeSubscription)(nil)`
(`screen_test.go:208`) will fail because `Events()` returns the wrong element type.

Change the channel + accessor to `event.Delivery`, and wrap in `push` so callers
keep passing bare `event.Event` (no churn at the ~dozens of `push`/`feed`/`deliver`
call sites):

BEFORE (`screen_test.go:170-205`, elided):
```go
type fakeSubscription struct {
	ch       chan event.Event
	closeErr error
	closed   bool
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{ch: make(chan event.Event, 64)}
}

func (s *fakeSubscription) Events() <-chan event.Event { return s.ch }
// ...
func (s *fakeSubscription) push(ev event.Event) {
	select {
	case s.ch <- ev:
	default:
		panic("fakeSubscription buffer full")
	}
}
```

AFTER:
```go
type fakeSubscription struct {
	ch       chan event.Delivery
	closeErr error
	closed   bool
}

func newFakeSubscription() *fakeSubscription {
	return &fakeSubscription{ch: make(chan event.Delivery, 64)}
}

func (s *fakeSubscription) Events() <-chan event.Delivery { return s.ch }
// ...
func (s *fakeSubscription) push(ev event.Event) {
	select {
	case s.ch <- event.Delivery{Event: ev}:
	default:
		panic("fakeSubscription buffer full")
	}
}
```

`Close()` (closes `s.ch`) and `Err()` are unchanged. `push`'s `event.Event`
parameter is deliberately kept so no call site changes.

### 6.2 Tests that need NO change (verified)

- `tui/commands_test.go` — reads via `subNext`, asserts on `eventMsg.ev`
  (`event.Event`). Since `push` still takes `event.Event` and `subNext` returns
  `eventMsg{ev: d.Event}`, all assertions hold. The `subClosedMsg` /
  `hub.SubscriptionLossError` cases are unaffected.
- `tui/screen_test.go` gate tests (`fakeAgent.Approve/Deny/ProvideAnswer` at
  lines 126/134/141; `TestApproveKeyDispatchesApprove`, `TestAnswerKeyDispatchesProvideAnswer`)
  — the `tui.Agent` interface is unchanged, so `fakeAgent` still satisfies it. No change.
- `tui/restore_test.go`, `tui/run_test.go` (`cli/run_test.go`), `tui/anim_test.go`,
  `tui/tool_handoff_test.go`, `tui/strand_integration_test.go`,
  `tui/transcript_test.go`, `tui/interaction_test.go`, `tui/commands_test.go`
  gate-cmd tests — all build `event.Event` values and either call `ApplyEvent`/
  `FoldDisplay` directly or hand-deliver `eventMsg{ev: ...}` (not through
  `.Events()`), so the channel-type change does not reach them. No change.
- `tui/screen_test.go:15` `import ".../pkg/hub"` — `hub.SubscriptionLossError`
  (used at `screen_test.go:1360`) still exists in harness; import stays valid.

> Re-verify after the vendor refresh: run the full test build; if any additional
> `fakeSubscription`-style double exists in an integration-tagged file, apply the
> same `event.Delivery` wrap. (Current search finds only the one in `screen_test.go`.)

---

## 7. Build / verify steps

```sh
cd /Users/ipotter/code/looprig/cli

# 0. Absorb the archived transcript layer (§4.1) — harness no longer provides it,
#    so tidy/vendor fail without this. Then add goldmark back to cli's CLAUDE.md.
cp -R ../archive/transcript ./transcript
find ./transcript ./tui ./cli -name '*.go' -type f -exec sed -i '' \
  -e 's|github.com/looprig/harness/pkg/transcript|github.com/looprig/cli/transcript|g' {} +

# 1. Refresh the stale vendored harness (see §4.2) — after the relocation.
go mod tidy
go mod vendor

# 2. Apply §5.1 (commands.go unwrap) and §6.1 (fakeSubscription).

# 3. Build + test (race is mandatory per repo policy).
go build ./...
go test -race ./...

# 4. Integration-tagged tests (strand_integration_test.go etc.).
go test -tags integration -race ./...
```

Even though `replace => ../harness` points at the local tree, the committed
`vendor/` tree wins at build time — so step 1's `go mod vendor` is non-optional,
not a convenience. Skipping it makes the source edits fail to compile against the
old vendored API.

Downstream (separate module, not part of cli's CI gate): once cli is green, run the
swe migration (§3) and `go build ./... && go test -race ./...` in `../swe`.

---

## 8. Open questions / flags

1. **Interface reshape (optional, not required for build).** cli's `tui.Agent`
   keeps the `Approve` / `Deny` / `ProvideAnswer` names; swe re-backs them with
   `RespondGate` internally. If the team instead wants cli's interface to *mirror*
   the harness `RespondGate` shape (a single `RespondGate(ctx, gate.GateResponse)`
   method), that is a larger, deliberate refactor touching `tui/agent.go`,
   `tui/commands.go`, `tui/screen.go`/`interaction.go` (the `uiApprove`/`uiDeny`
   action plumbing), and swe. It is **not** needed for compilation and is out of
   scope for this migration unless explicitly requested.

2. **swe loopID/callID → gate.ID mapping (swe-side, flagged for the swe migration).**
   cli's reply methods pass `(loopID, callID uuid.UUID)` where `callID` is the
   `PermissionRequested.ToolExecutionID`. `RespondGate` keys on `gate.ID`. swe must
   resolve the open gate's `gate.ID` from `(loopID, callID)` inside its `sessionAgent`.
   If that resolution is not feasible purely inside swe (i.e., the gate id is not
   recoverable from the tool-execution id available to the TUI), then cli's interface
   *would* need to surface a gate identifier — which would turn open question #1 from
   optional into required. **Verify against swe before executing swe's migration.**
   This does not change cli's migration, but it gates whether cli's interface can
   truly stay frozen.

3. **`Delivery.JournalSeq` drop.** §5.1 discards `JournalSeq`. Confirm the TUI has
   no near-term need for the durable sequence (e.g., dedup on restore-then-live
   handoff). Current code does not use it; noted for future.
