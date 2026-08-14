# Contributing to looprig/tui

Thanks for considering a contribution. `tui` is the interactive terminal
presentation layer in a multi-module Go ecosystem; it depends on
`github.com/looprig/harness` for runtime contracts, and harness never
imports `tui`. This file is the short guide for working in *this*
repository.

## Before you write code

1. Read [`CLAUDE.md`](CLAUDE.md) (a.k.a. `AGENTS.md`). It is the authoritative
   source for the design, security, dependency, build, and code rules this
   module follows. PRs that contradict it will be asked to change.
2. Note the package boundary: the root package is the module's stable public
   facade; the coupled Bubble Tea application lives in
   `internal/presentation`. Internal packages never import the root facade.
3. Skim a couple of recent files in [`docs/plans/`](docs/plans/) for the
   design-doc style the project uses.
4. Open an issue for anything non-trivial so we can agree on direction
   before you spend the time.

## Design and security rules (the short version)

- **Strict typing everywhere.** No `any`/`interface{}` past a serialization
  boundary. Named types (`type UserID string`) over bare primitives when the
  value has domain meaning. All domain concepts are typed structs, never
  `map[string]interface{}`.
- **All errors are typed.** Sentinel or typed errors for public failures
  callers classify with `errors.As`; wrapped ordinary errors for contextual
  failures callers only report. Never swallow with `_`.
- **Security is first-class.** Validate at every boundary. Authenticate
  before authorize, authorize before act. Fail secure: on error or
  ambiguity, deny by default. Never log secrets, tokens, or PII. Use
  `crypto/rand` for anything security-sensitive.
- **Least privilege always.** Every component, goroutine, and service gets
  only the permissions it needs. Never pass a full config or god-object when
  a narrow interface suffices.
- **Prefer stdlib.** External packages require explicit user approval in
  the conversation that adds them. State what the package is, why stdlib is
  insufficient, and what the package adds. Once approved, the package is
  added to the approved list in `CLAUDE.md`. Never `go get` without that
  approval.
- **Open-Closed + Interface Segregation.** Small focused interfaces; never
  widen an existing interface. Liskov substitution: every implementation
  honors the full contract — no panicking, no unexpected errors, no
  silently doing less.
- **TUI stays a renderer.** This module does not implement command tools or
  couple to concrete tool/sandbox packages; it renders permission
  interactions supplied by `sessionadapter` and stays independent of them.

## Build, test, and secure

**Dependencies are pinned, not vendored.** `go.mod` pins exact versions and
`go.sum` verifies their content hashes, which is what makes a build
reproducible. This module deliberately has no `vendor/`: a vendor tree is
ignored under a `go.work` but silently satisfies a `GOWORK=off` build, so a
stale one lets standalone verification pass against the vendored copy rather
than the version `go.mod` actually pins — defeating the purpose of verifying
standalone.

This module participates in the parent `../go.work` workspace for local
multi-module editing, so the Make targets resolve sibling modules from your
local checkouts by default. Add `GOWORK=off` to check the module against its
real pinned dependencies instead:

```sh
make fmt                # gofmt the whole module in place
make test               # go test -race ./...
make test-integration   # go test -tags integration -race, scoped to this module
make lint               # fmt-check + vet + staticcheck + gosec
make vuln               # go mod verify + govulncheck
make secure             # lint + vuln — run this before every commit
GOWORK=off go test ./... # verify against the pinned dependency versions
```

`gosec` is scoped to this module's own package directories (not a plain
`./...`) so it doesn't descend into the nested `.worktrees/` checkouts, which
are separate modules.

Build binaries with `CGO_ENABLED=0 go build -trimpath` so they never leak
local paths. Fuzz any parser of external input:
`go test -fuzz=FuzzXxx ./pkg -fuzztime=30s`.

## Tests

- **Table-driven tests, mandatory** when several cases share setup and
  assertion shape. Each subtest calls `t.Parallel()`. Cover the happy path,
  boundary values (zero/empty/max), error cases (invalid/missing/wrong
  type), and domain edge cases.
- A test that passes without `-race` but fails with it is **not passing**.
- Integration tests (tagged `//go:build integration`) live in
  `*_integration_test.go` files and are excluded from the default
  `go test ./...` run. Run them explicitly with
  `GOWORK=off make test-integration`.
- Never assume a test framework or script. The `Makefile` is the source of
  truth; if you change how tests run, update it.

## Pull requests

- Branch from `main`, name the branch something descriptive.
- One logical change per PR. If a change spans modules, open a PR per
  module and stack them; the `replace` directives let each module build
  against the others' local checkout.
- Write a clear description: what, why, the design alternative you
  rejected, and how you verified. `GOWORK=off make secure` output is
  welcome in the PR body.
- Don't force-push after review; add commits and let the reviewer squash.
- Don't commit secrets, tokens, or credentials. Don't add a new external
  dependency without prior approval (see `CLAUDE.md`).
- Don't update `CLAUDE.md`, `Makefile`, or `go.mod` `replace` directives
  unless the change is the point of the PR.

## Code of conduct

Be excellent to each other. Discussions stay technical and respectful;
personal attacks, harassment, and discrimination are not welcome.

## License

By contributing, you agree that your contributions are licensed under the
Apache License 2.0, as described in [`LICENSE`](LICENSE).
