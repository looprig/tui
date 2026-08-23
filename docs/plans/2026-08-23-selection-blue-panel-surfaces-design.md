# Selection-blue panel surfaces design

## Goal

Use the established selection blue, `#A2D2FF`, as the background for the modern composer, unselected completion-tray rows, and modern user-message rows.

## Design

Keep one shared token: change `styles.PanelBg` from neutral gray to the existing
`CardBorderColor` selection-blue token. The modern composer, unselected tray rows,
and user-message rows already consume `PanelBg`, so this changes exactly the three
requested surfaces without adding another mutable color token.

Selected tray rows continue to render through `styles.SelectedRow`; their near-black
foreground treatment remains the contrast treatment for the same light blue fill.

## Verification

Update explicit `#303030` assertions and the tray render golden to `#A2D2FF`, then
run the focused components and presentation tests plus standalone package tests.
