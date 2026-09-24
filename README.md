# tui

The reusable terminal user interface for
[looprig](https://github.com/looprig/harness): a Bubble Tea v2 screen that
drives a harness session — transcript, composer with `@path` and slash-command
completion, subagent loops, and permission and ask-user gates. It keeps the
Bubble Tea presentation stack outside the core Harness. Carbon and policy53
embed it.

```sh
go get github.com/looprig/tui@latest
```

## Packages

- `tui` (root) — the stable public facade for the interactive screen and its
  `Agent` contract (`tui.New`, options such as `tui.WithSessionPresentation`)
- `runtime` — `runtime.Run`: process signals, logging to `~/.looprig/looprig.log`,
  terminal capture, Bubble Tea startup and bounded teardown
- `sessionadapter` — adapts a harness `session.SessionController` to the TUI
  contract (`New`, `NewWithReplay`, `Restore`)
- `restore` — an interactive `session.RestoreDecider` an application wires into
  its Rig with `rig.WithRestoreDecider(restore.NewDecider(restore.NewTerminalUI()))`
- `components` and `styles` — reusable presentation leaves
- `examples/runtimehost`, `examples/sessionadapter`, `examples/restore` —
  runnable examples of each seam

The dependency is one-directional: TUI imports Harness, never the reverse. The
application composes the Rig and the session; the TUI never does.

## Usage

A product entry point supplies an agent constructor and a banner; `runtime.Run`
returns the process exit code (from `examples/runtimehost`):

```go
openAgent := func(ctx context.Context) (tui.Agent, error) {
	// Wrap an already-composed Rig session, e.g. sessionadapter.New(sess)
	// or sessionadapter.Restore(ctx, sess, store).
	return sessionadapter.New(sess), nil
}
os.Exit(runtime.Run(ctx, openAgent,
	runtime.Banner{Name: "Purpose-built assistant", Description: "Terminal interface"}))
```

## Internals

Internal ownership is similarly one-directional:

- `internal/presentation` owns the complete Bubble Tea state machine and behavioral tests
- `internal/input` owns attachment parsing, validation, and file completion
- `internal/model` owns independent value projections such as compaction and collapse state
- `internal/view` owns reusable rendering primitives such as rails and width wrapping
- `internal/ttylog` owns private terminal log capture

The transcript, interaction model, viewport, and `Screen` stay together in
`internal/presentation` because they form one Bubble Tea state machine rather than independent
reusable packages. Consumers import only the public packages above.

## Tool-preparation approval

A tool call that needs approval renders as ONE combined prompt built from the typed
`gate.PermissionPayload` (`event.PermissionRequested.Request`, a `tool.Request` narrowed
to its unmet requirements, each carrying its exact reusable rule candidates). The TUI
renders that typed payload verbatim — it never reconstructs a rule or parses tool
arguments. When Harness projects a mutation preview, the prompt shows the diff.
The prompt offers the `gate.ApprovalControls` actions, keyed:

- `y` — **Approve** (`gate.ApprovalApprove`; grants once, persists nothing)
- `a` — **Approve always for this workspace** (`gate.ApprovalApproveAlwaysWorkspace`;
  persists the displayed candidates). Offered only when the prompt carries a
  reusable candidate, and only on an unmodified `a`: ctrl/alt/shift+a grant nothing.
- `n` / `esc` — **Deny** (`gate.ApprovalDeny`; fail-secure)

The actions are also selectable rows (↑/↓, then enter; the cursor starts on
Approve, and an out-of-range cursor denies). `?` toggles a panel listing the
screen's key bindings.

There is no session scope, user-global scope, per-capability sub-prompt, `/access`
command, access tray, or mutable security level.

## Session presentation metadata

Workspace path, the fixed access-profile name, and permission diagnostics are supplied
synchronously by the consumer at screen construction via
`tui.WithSessionPresentation(tui.SessionPresentation{…})` — never queried asynchronously
and never inferred from events. The fixed profile is shown as footer/session metadata
(not a mutable control), and permission diagnostics for manual, out-of-catalogue allow
families are committed in the startup metadata area so they are visible **before** the
first permission gate.

## Building & testing

The Go baseline is 1.26.8. Dependencies are pinned by `go.mod`/`go.sum`, not
vendored. Verify standalone against the pinned modules:

```sh
GOWORK=off go test ./...
make check              # gofmt, vet, staticcheck, gosec, govulncheck, race tests, build
make test-integration   # -tags integration
```

tui sits at tier 4; its direct Looprig dependencies are `core`, `harness` and
`inference`.

## License

Apache License 2.0; see [LICENSE](LICENSE).
