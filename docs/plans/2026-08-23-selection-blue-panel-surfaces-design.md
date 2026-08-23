# Darker-gray panel surfaces design

## Goal

Darken the modern composer, unselected completion-tray rows, modern user-message rows, and the startup session/agent banner from `#303030` to the existing near-neutral gray `#242527`. Darken their shared left rail from `#737373` to `#505050`.

## Design

Keep one shared token: point `styles.PanelBg` at the existing `CardPanelBg` dark-gray token.
The modern composer, unselected tray rows, and user-message rows already consume `PanelBg`.
The startup banner has its own entry kind so the modern viewport can apply the same fill without
changing generic info, warning, or error notices. Its rail, plus the composer/user/tray rails,
all resolve through the existing `RailColor` token (`#505050`).

Selected tray rows continue to render through `styles.SelectedRow`; their existing selection blue
and near-black foreground treatment are unchanged.

## Verification

Update explicit `#303030` assertions and the tray render golden to `#242527`, pin the `#505050`
rail, and verify the startup banner's full-width panel treatment. Run the focused components and
presentation tests plus standalone package tests.
