# Markdown Table Row Separators Design

## Problem

Markdown tables rendered in the TUI wrap long cells to the available terminal width. Without separators between body rows, a wrapped continuation line can look like a new or displaced row, especially because user and assistant content also carries a two-column timeline rail.

## Design

Keep table wrapping enabled so dynamic LLM content remains readable. After Glamour parses Markdown into logical table rows, configure its Lip Gloss table to draw a horizontal border between body rows. Lip Gloss computes the full height of each logical row after wrapping, so the border is emitted only after every wrapped line in that row.

The change is local to the vendored Glamour table adapter used by this module. The existing header separator and open outer-border style remain unchanged. No Markdown preprocessing or assumptions about table columns, row counts, or LLM content are introduced.

## Behavior

- Valid Markdown tables retain their source row order.
- Long cells continue to wrap at the current renderer width.
- Wrapped lines remain grouped within one logical row.
- A horizontal separator appears between adjacent body rows, but not after the final row.
- Non-table Markdown rendering is unchanged.

## Testing

Add a renderer regression test using a narrow, two-row Markdown table whose first body row wraps. The test will assert that the wrapped continuation appears before exactly one body-row separator and that the second row appears after it. Run the focused styles tests and the complete TUI test suite.
