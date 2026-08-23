# Tray Header Spacing and Provider Groups Design

## Goal

Give the searchable Sessions and Models trays one panel row of breathing room above their
headers, and group live model choices by the canonical provider configured by Carbon.

## Data flow

Carbon already retains the canonical provider on each configured primer candidate and on the
currently selected model. `RuntimeAgent.LoopRuntimeOptions` will copy that value into
`tui.ModelOption.Provider`. The TUI already forwards that display value into its model picker,
which will therefore create headings such as `OPENAI` and `ANTHROPIC` instead of collapsing
valid choices under `OTHER`.

`OTHER` remains only as the defensive presentation for a catalog that genuinely omits provider
metadata.

## Layout

The common tray-header renderer will add one blank, rail-carrying panel row before its title.
Because Sessions and Models share that renderer, both retain the same geometry: top spacer,
bold uppercase title, muted count, blank separator, then choices. Pointer-coordinate handling
will move with the header height so the new chrome stays inert.

## Verification

Tests will prove Carbon exposes a selected model provider and configured candidate providers;
the TUI will prove the header has the new top spacer and correct pointer offset. The complete
TUI standalone suite and the focused Carbon package suite will verify the integration.
