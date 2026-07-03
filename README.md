# looprig-console

The terminal-UI (`tui`) and CLI (`cli`) **presentation layer** for
[looprig](https://github.com/looprig/harness), extracted into its own module so
the core SDK can shed the heavy `charm.land/*` (Bubble Tea v2) dependency stack.

This module depends on `github.com/looprig/harness` for core types (content,
event, transcript, …). The dependency is **one-directional**: looprig-console
imports looprig, never the reverse.

## Building & testing

This module vendors its dependencies (`-mod=vendor`), which is incompatible with
an active `go.work` workspace. Always disable the workspace with `GOWORK=off`:

```sh
GOWORK=off make secure     # lint + vuln
GOWORK=off go build ./...
GOWORK=off go test -race ./...
```
