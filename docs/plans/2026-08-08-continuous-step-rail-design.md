# Continuous Step Rail Design

## Goal

Render thinking, assistant narration, and tool activity as one continuous step timeline,
including when node headers wrap, while making the rail quieter than its nodes and text.

## Rendering

The leftmost timeline column is shared by all content in an assistant step. A node glyph
(`●`, `○`, or `◍`) occupies that column on the first row of assistant narration or a tool
header. On every wrapped continuation row, the same column renders `│` instead of becoming
blank. Bare `│` rows continue to connect adjacent nodes. The rail ends after the final node
in the step, so normal turn spacing remains blank.

Expanded thinking and tool details continue to use their existing railed rows. Collapsed
thinking and collapsed tool-run summaries obey the same timeline rule; collapsing removes
detail, not connectivity.

## Styling

Introduce a rail-specific style that is subtler than the reasoning text style. Apply it only
to spine and connector glyphs. Assistant and tool nodes retain their status colors, and
thinking/tool-result text retains its existing readability.

## Testing

Pin the shared rail-node primitive so depth-zero and nested wrapped continuations contain a
rail in the node column and remain width-aligned. Pin the rail style independently from the
thinking style, then run the presentation tests to cover expanded and collapsed composition.
