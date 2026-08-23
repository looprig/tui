# Darker-gray panel surfaces design

## Goal

Darken the modern composer, unselected completion-tray rows, and modern user-message rows from `#303030` to the existing near-neutral gray `#242527`.

## Design

Keep one shared token: point `styles.PanelBg` at the existing `CardPanelBg` dark-gray token.
The modern composer, unselected tray rows, and user-message rows already consume `PanelBg`, so
this changes exactly the three requested surfaces without adding another mutable color token.

Selected tray rows continue to render through `styles.SelectedRow`; their existing selection blue
and near-black foreground treatment are unchanged.

## Verification

Update explicit `#303030` assertions and the tray render golden to `#242527`, then
run the focused components and presentation tests plus standalone package tests.
