# Responsive Markdown Tables Design

## Problem

Glamour renders Markdown tables through Lip Gloss. When a prose-heavy cell is
wider than its allocated column, Lip Gloss wraps that cell while leaving the
other cells blank on the continuation lines. Row separators make the logical
boundaries visible, but they do not make description-heavy tables scan like
records. The result still appears detached or out of order even though the
source row ordering is technically preserved.

Disabling table wrapping is not acceptable because it truncates content. An
upstream patch is also unnecessary: the TUI already parses Markdown around its
Glamour renderer and can own a responsive presentation policy.

## Decision

Use a Codex-style responsive key/value presentation for tables whose narrative
content would wrap in a grid. Keep Glamour's normal grid for compact tables that
fit without narrative wrapping.

For example, a description-heavy row renders as:

```text
 Module        core
 Language      Go
 What it does  Foundational shared types: the content block/message
               vocabulary used across every module, plus logging and uuid.
────────────────────────────────────────────────────────────────────────────
 Module        storage
 Language      Go
 What it does  Neutral, stdlib-only storage contracts...
```

At widths where an aligned label and a useful value cannot coexist, each field
stacks instead:

```text
 Module
  core
 Language
  Go
 What it does
  Foundational shared types...
```

## Detection and layout

Goldmark remains the source of truth for locating real GFM tables, so fenced
code, escaped pipes, pipe-less rows, definition descriptions, blockquotes, and
line endings are not confused with tables.

The adapter classifies a column as narrative when its non-empty body cells
average at least four words or 28 display columns. This is content-based rather
than dependent on header spelling, so arbitrary LLM-generated headers work.
Other columns remain compact.

For each table, the adapter measures visible display width, including Unicode
cell width, and estimates the grid allocation at the current renderer width.
It chooses record mode when:

- at least one narrative column would wrap at that allocation; or
- the columns cannot retain a small readable minimum width.

This trigger deliberately differs from Codex's current conservative fallback.
Codex waits until prose becomes severely cramped; LoopRig switches on the first
narrative wrap because detached description continuations are the reported
failure.

In record mode, headers become labels and every source body row becomes one
record. Labels are aligned to the widest header when at least 24 value columns
remain. Otherwise labels stack above values. Continuation lines align under the
value, and a muted full-width rule separates records. Empty cells still render
their labels with an empty value so the table structure is not silently changed.

## Rendering integration

`styles.RenderMarkdown` stays the single TUI-owned adapter used by assistant and
user-message rendering. It will replace responsive tables while passing all
non-table Markdown through the existing configured Glamour renderer unchanged.
Cell contents must retain supported inline Markdown styling, links, code, and
Unicode text. The adapter will not add a dependency or modify vendored Glamour
or Lip Gloss code.

The existing private-use marker-row separator workaround is removed. Compact
tables continue through Glamour without marker rows; responsive tables receive
record separators from the TUI renderer directly.

Because presentation is computed from the renderer width on every render, a
terminal resize naturally moves a table between grid and record modes without
changing stored transcript content.

## Failure behavior

If responsive table extraction or cell rendering cannot produce a valid record
layout, the adapter falls back to unmodified Glamour rendering. Markdown content
must never disappear because the responsive path failed.

## Testing

Regression coverage will use the reported `Module / Language / What it does`
table and assert that its long descriptions render as labeled records at the
reported width with all content preserved. Additional tests cover:

- compact tables remaining normal grids;
- content-based narrative detection with arbitrary headers;
- aligned and stacked record layouts at their width boundary;
- rich inline cell content and Unicode display widths;
- empty cells and uneven rows;
- fenced table-shaped code and non-table Markdown remaining unchanged;
- blockquoted tables retaining their enclosing prefix;
- terminal-width changes selecting the appropriate layout; and
- fuzzing arbitrary Markdown to ensure the adapter never panics or loses a
  successfully rendered non-table document.

