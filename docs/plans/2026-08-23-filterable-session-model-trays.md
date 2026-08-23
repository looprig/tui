# Filterable Session and Model Trays Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make session and model trays searchable through the existing bottom input box, with titled tray headers, context-sensitive footer help, and non-selectable provider groups.

**Architecture:** Keep the composer as the sole editable control while a session or runtime tray is open. Extend the shared tray engine with non-selectable chrome and component-level headers, then let each picker own its matching rules and query state. Session search accepts title, optional description, and identifiers; model search accepts provider, label, aliases, and opaque ID while provider headings remain visual-only.

**Tech Stack:** Go, Bubble Tea v2, Bubbles list, Lip Gloss, existing TUI component tests.

---

### Task 1: Give tray rows a safe, non-selectable chrome representation

**Files:**
- Modify: `components/traylist.go`
- Test: `components/traylist_test.go`

**Step 1: Write the failing tests**

Add tests that a non-selectable row renders in a tray window but `Up`, `Down`, and pointer selection never leave the cursor on it.

**Step 2: Run the focused tests to verify they fail**

Run: `GOWORK=off go test ./components -run 'TestTrayList.*NonSelectable'`

Expected: FAIL because the tray has no non-selectable row or cursor-skip behavior.

**Step 3: Write the minimal implementation**

Add row kind/payload metadata to the shared tray item, render headings as bold non-selected panel rows, and make movement plus hover/click selection skip those rows. Preserve the existing selectable-item behavior for slash, file, session, and runtime trays.

**Step 4: Run the focused tests to verify they pass**

Run: `GOWORK=off go test ./components -run 'TestTrayList.*NonSelectable'`

Expected: PASS.

### Task 2: Make session selection and searching filter-safe

**Files:**
- Modify: `components/sessioncomplete.go`
- Modify: `internal/presentation/sessionbrowser.go`
- Modify: `internal/presentation/screen.go`
- Test: `components/sessioncomplete_test.go`
- Test: `internal/presentation/sessionbrowser_test.go`

**Step 1: Write the failing tests**

Cover filtering a session by optional description and full identifier, selecting a filtered result by its original payload, the `SESSIONS` title/count/blank separator, and an empty-result row that stays non-selectable.

**Step 2: Run the focused tests to verify they fail**

Run: `GOWORK=off go test ./components ./internal/presentation -run 'TestSession.*(Filter|Header|Payload)'`

Expected: FAIL because sessions do not retain descriptions or accept an interactive filter.

**Step 3: Write the minimal implementation**

Add optional `Description` to `SessionSummary` and `SessionItem`, route it into the session picker, and expose filter/reset/query/count methods on `SessionComplete`. Render a bold `SESSIONS` header, a muted dynamic count (`N sessions` or `N of M sessions`), and one rail-carrying spacer before records. Keep the header and no-results row inert, use title fuzzy matches plus description/ID fallback matches without misleading underlines, and resolve resume IDs through the unfiltered item index.

**Step 4: Run the focused tests to verify they pass**

Run: `GOWORK=off go test ./components ./internal/presentation -run 'TestSession.*(Filter|Header|Payload)'`

Expected: PASS.

### Task 3: Render grouped, searchable model choices

**Files:**
- Modify: `components/valuecomplete.go`
- Modify: `internal/presentation/runtimecontrol.go`
- Modify: `internal/presentation/screen.go`
- Test: `components/valuecomplete_test.go`
- Test: `internal/presentation/runtimecontrol_test.go`

**Step 1: Write the failing tests**

Cover model rows grouped by provider under bold headings, cursor/hover selection skipping provider headings, provider/name/alias/ID matching, and a selected filtered model resolving to its original opaque ID.

**Step 2: Run the focused tests to verify they fail**

Run: `GOWORK=off go test ./components ./internal/presentation -run 'Test(ValueComplete|Runtime).*?(Provider|Filter|Select)'`

Expected: FAIL because `ValueItem` carries no provider and runtime tray input is inert.

**Step 3: Write the minimal implementation**

Add optional provider display data from `ModelOption` through `ValueItem`. In the model picker, group matching model rows under bold inert provider headings; render model name with a single muted, truncating description column. Keep non-model runtime pickers on their current ungrouped layout while exposing the same filtering mechanics where appropriate.

**Step 4: Run the focused tests to verify they pass**

Run: `GOWORK=off go test ./components ./internal/presentation -run 'Test(ValueComplete|Runtime).*?(Provider|Filter|Select)'`

Expected: PASS.

### Task 4: Use the bottom input box and footer for tray-specific interaction

**Files:**
- Modify: `internal/presentation/screen.go`
- Modify: `internal/presentation/keypanel.go`
- Test: `internal/presentation/screen_test.go`
- Test: `internal/presentation/paste_test.go`

**Step 1: Write the failing tests**

Cover typing and paste into the bottom input box updating an open session/model tray without submitting a message, restoring ordinary compose behavior after dismissal, and showing context-sensitive footer text in place of workspace metadata.

**Step 2: Run the focused tests to verify they fail**

Run: `GOWORK=off go test ./internal/presentation -run 'Test.*(Tray.*(Filter|Footer|Input)|Paste.*Tray)'`

Expected: FAIL because runtime/session trays currently own and discard text input while the footer always shows workspace metadata.

**Step 3: Write the minimal implementation**

On opening a searchable tray, preserve the compose draft, clear the shared input, set a contextual `Search sessions…` or `Search models…` placeholder, and route edits/paste to the filter rather than the interaction command parser. `Up`, `Down`, `Tab`, and `Enter` retain picker semantics; `Esc` closes and restores the draft. Make the footer’s header text context-specific while a tray is open: sessions mention searching descriptions or IDs and resuming; models mention searching provider or name and selecting. Restore the workspace/profile header when closed.

**Step 4: Run the focused tests to verify they pass**

Run: `GOWORK=off go test ./internal/presentation -run 'Test.*(Tray.*(Filter|Footer|Input)|Paste.*Tray)'`

Expected: PASS.

### Task 5: Format and verify the complete module

**Files:**
- Modify: all implementation and test files above

**Step 1: Format the changed Go sources**

Run: `gofmt -w components/traylist.go components/traylist_test.go components/sessioncomplete.go components/sessioncomplete_test.go components/valuecomplete.go components/valuecomplete_test.go internal/presentation/sessionbrowser.go internal/presentation/sessionbrowser_test.go internal/presentation/runtimecontrol.go internal/presentation/runtimecontrol_test.go internal/presentation/screen.go internal/presentation/screen_test.go internal/presentation/paste_test.go internal/presentation/keypanel.go`

**Step 2: Run targeted suites**

Run: `GOWORK=off go test ./components ./internal/presentation`

Expected: PASS.

**Step 3: Run standalone verification**

Run: `GOWORK=off go test ./...`

Expected: PASS.

**Step 4: Review the diff**

Run: `git diff --check && git status --short`

Expected: no whitespace errors and only feature-local files changed.
