# Markdown Table Row Separators Design

## Problem

Markdown tables rendered in the TUI wrap long cells to the available terminal width. Without separators between body rows, a wrapped continuation line can look like a new or displaced row, especially because user and assistant content also carries a two-column timeline rail.

## Design

Keep table wrapping enabled so dynamic LLM content remains readable. A TUI-owned renderer adapter uses Goldmark's GFM AST—the same parser used by Glamour—to locate logical table body rows, then inserts a private, collision-free marker row between adjacent rows before passing the document to Glamour. Glamour therefore parses and wraps the marker as a logical row using its normal table pipeline. After rendering, the adapter replaces each marker row with a horizontal separator of the same rendered width, retaining the existing column junctions.

The change lives entirely in `styles`; no upstream or vendored dependency is patched. Documents without a multi-row Markdown table bypass the adapter and render byte-for-byte through Glamour as before. The marker is selected from Unicode's private-use area and must not occur in the input, preventing LLM content from colliding with the internal row identity.

## Behavior

- Valid Markdown tables retain their source row order.
- Long cells continue to wrap at the current renderer width.
- Wrapped lines remain grouped within one logical row.
- A horizontal separator appears between adjacent body rows, but not after the final row.
- Non-table Markdown rendering is unchanged.

## Testing

Add a renderer regression test using a narrow, two-row Markdown table whose first body row wraps. The test will assert that the wrapped continuation appears before exactly one body-row separator and that the second row appears after it. Run the focused styles tests and the complete TUI test suite.
