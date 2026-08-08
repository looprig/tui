# Remove Empty Step Gap Design

## Goal

Keep assistant-step content visually connected in the TUI without inserting fully empty rows
between the step's railed spacer and its tool node or collapsed tool summary.

The intended collapsed shape is:

```text
│ thought for 3s
│
○ 1 tool · Bash
```

When narration is present, the same rule applies through the entire step:

```text
│ thought for 3s
│
● No README. Let me inspect the repository.
│
○ Bash(...)
│ output
```

## Scope

Change only the modern TUI transcript composition. Preserve the single railed spacer between
nodes, normal blank spacing after a completed step, collapse behavior, tool result rendering,
and selection provenance.

## Approach

Trace the runtime event shape that produces the extra rows and remove the unstyled separator at
the composition boundary where it is introduced. Require the separator's provenance to identify
a committed assistant entry; a visually identical bare rail produced by an empty tool-result
line must retain its spacing before a later tool or Subagent card. Do not globally remove
`renderThinking`'s railed spacer or tool-node spacing. Both collapsed tool summaries and expanded
tool nodes must be adjacent to the preceding assistant spacer with no fully empty rows between
them.

## Testing

Add a regression test using the runtime event sequence that reproduces the bug. Assert the exact
visible line adjacency for a thinking-only step and a thinking-plus-narration step in collapsed
and expanded states. Retain the existing tests for turn-boundary breathing space and rail
connectors, then run the full TUI test suite.
