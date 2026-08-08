# Unified Tray Selection Glow Design

## Goal

Give slash-command, file, runtime-value (including `/model`), and session trays the same
selected-row background transition when the tray opens and whenever keyboard or mouse navigation
changes selection.

## Root cause

All four tray renderers already receive `traySelectionColor(m.trayGlowFrame)`. Runtime and session
open paths and all four selection-movement paths call `startTrayGlow`, but the scheduled tick stops
in `handleTrayGlow` because its active-tray guard recognizes only slash-command and file trays.
Runtime and session trays therefore render the initial transition color but never advance through
the shared animation frames.

Slash-command and file trays have a second discrepancy: they open synchronously while editing the
composer, but `routeToInteraction` only starts the glow after Up or Down changes an already-open
tray's cursor. Their initially selected row stays at the final static color until the cursor moves.

## Design

Add a presentation-owned helper that reports whether any completion tray is open. Use that helper
in `handleTrayGlow` so the existing epoch, frame, duration, and rendering behavior applies equally
to all tray kinds. Keep the stale-tick guard: closing every tray still terminates an outstanding
animation chain.

After routing a composer key, compare the completion state from before and after the update. Start
the glow when a slash-command or file tray transitions from closed to open, while retaining the
existing restart when Up or Down changes selection. Runtime and session result handlers keep their
existing open-time `startTrayGlow` calls because those trays arrive asynchronously outside the
composer interaction path.

Extend the existing table-driven transition test with slash, file, runtime, and session tray
fixtures. For each fixture, start the glow directly and assert that every scheduled tick advances
to the final frame and renders the selected row using that frame's shared background color. This
isolates the animation-driver contract from each tray's different navigation and record geometry.
Add an interaction test proving that entering `/` starts at frame zero as soon as the slash tray
opens; the same state transition owns file-tray opening.

No renderer, color, timing, keyboard, mouse, layout, or public API changes are required.
