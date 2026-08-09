# Runtime Metadata and StartAgent Card Design

## Goal

Make the focused-loop status show `model · effort · context` and render one stable `StartAgent(<agent>)` card for each current agent spawn, regardless of parent/child event order.

## Runtime metadata

The status line remains the only runtime-metadata surface. It does not show the loop mode. It renders the event-authoritative model first, then effort, then context usage. Because `model.EffortNone` is represented internally by the empty string, the presentation layer renders an empty effort as `none` whenever a model runtime exists. A missing runtime still renders no model or effort metadata, preserving compatibility with old or harness-managed events.

## Agent spawn cards

`StartAgent` is the canonical label for current agent-tool spawns. A provisional child card defaults to `StartAgent`, while a reconciled committed card preserves the actual parent tool name so restored legacy `Subagent` calls remain readable.

The transcript reducer supports both cross-loop delivery orders:

1. When `LoopStarted` arrives before the parent `StepDone`, the existing accumulator is attached as the parent tool card commits.
2. When the parent `StepDone` arrives first, the reducer records the unresolved committed spawn card by parent loop and provider tool-use ID. A later child `LoopStarted` promotes that same committed entry in place and marks its accumulator reconciled, so no provisional duplicate appears.

Correlation fails closed when the parent/tool-use identity is missing or ambiguous. Spawn failures remain ordinary tool cards with their error result.

## Testing

Focused presentation tests cover explicit `none` effort, the required metadata ordering with context, provisional `StartAgent(<agent>)` labeling, legacy `Subagent` reconciliation, and the parent-first event ordering that previously produced two nodes. The complete TUI suite runs with the race detector before completion.
