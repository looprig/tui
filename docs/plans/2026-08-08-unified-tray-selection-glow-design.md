# Unified Tray Selection Glow Design

## Goal

Give slash-command, file, runtime-value (including `/model`), and session trays the same
selected-row background transition whenever keyboard or mouse navigation changes selection.

## Root cause

All four tray renderers already receive `traySelectionColor(m.trayGlowFrame)`, and all four
selection paths already call `startTrayGlow`. The scheduled tick stops in `handleTrayGlow`,
however, because its active-tray guard recognizes only slash-command and file trays. Runtime and
session trays therefore render the initial transition color but never advance through the shared
animation frames.

## Design

Add a presentation-owned helper that reports whether any completion tray is open. Use that helper
in `handleTrayGlow` so the existing epoch, frame, duration, and rendering behavior applies equally
to all tray kinds. Keep the stale-tick guard: closing every tray still terminates an outstanding
animation chain.

Extend the existing table-driven transition test with slash, file, runtime, and session tray
fixtures. For each fixture, start the glow directly and assert that every scheduled tick advances
to the final frame and renders the selected row using that frame's shared background color. This
isolates the animation-driver contract from each tray's different navigation and record geometry.

No renderer, color, timing, keyboard, mouse, layout, or public API changes are required.
