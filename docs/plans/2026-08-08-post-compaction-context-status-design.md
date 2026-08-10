# Post-Compaction Context Status Design

## Goal

Update the focused loop's context percentage immediately after successful conversation
compaction so the TUI displays the harness-authoritative post-compaction measurement instead
of retaining the pre-compaction value.

## Ownership

Context counting remains outside the TUI. The inference `contextcount` package supplies the
counter implementation, Carbon injects that counter into the loop, and the harness measures
the complete inference request and publishes durable authoritative measurements. The TUI only
folds those public events into display state and renders the resulting percentage.

## Approach

Extend the TUI runtime projection's event fold to handle `event.CompactionCommitted`. Replace
the committing loop's cached context measurement with the event's `PostContext` value and mark
the measurement present. This mirrors the harness session restore and session catalog folds,
which already treat `PostContext` as the current authoritative measurement.

Do not emit a duplicate `ContextMeasured` event, recalculate occupancy in the TUI, or clear the
measurement until a later turn. A rejected compaction leaves the prior measurement unchanged.
The update remains scoped by the committed event's loop ID, so other loops retain their own
measurements.

## Testing

Add projection coverage proving that `CompactionCommitted.PostContext` replaces the old
measurement for only the committing loop. Add screen coverage reproducing the visible bug:
render a pre-compaction percentage, deliver a committed compaction with a lower `PostContext`,
and assert that the status line immediately renders the lower percentage. Run the focused
presentation package with the race detector, followed by the complete TUI test suite.
