# Current Tray Choice Design

## Problem

Runtime and session trays always open with the first selectable row under the
cursor. The option catalogs contain no presentation marker for the active
choice, and the tray engine has no separate concept of an active row. As a
result, `/mode`, `/model`, `/effort`, and `/sessions` do not tell the user which
value is currently active. If that value is below the initial window, it is not
visible at all.

Model choices cannot be matched safely from display text. Their public IDs may
be product routing aliases rather than the model name stored in the durable
runtime projection.

## Design

Active state and cursor selection are distinct:

- The active choice uses blue primary text whenever it is not under the cursor.
- The cursor keeps the existing full-row selection band and continues to mean
  "this is what Enter will choose."
- On initial open, the cursor lands on the active choice. The tray's existing
  sliding window therefore brings an active choice from anywhere in the list
  into view.
- After navigation, the active choice remains blue while the selection band
  moves with the cursor.
- When active and selected are the same row, the selection band takes visual
  precedence so its text remains legible.

Runtime option records gain a `Current` marker. The runtime catalog owns this
mapping because it alone can resolve product-specific model aliases back to the
live model. This marker is a snapshot used only to identify a catalog row; the
event projection remains authoritative for durable status display and mutation
acknowledgements.

The session tray does not need a wider public browser contract. The screen
already owns the active agent and compares each listed session ID with
`Agent.SessionID()` while building presentation items.

## Components and data flow

1. Carbon reads the focused loop handle while constructing runtime options and
   marks the matching mode, model, and effort option as current.
2. TUI translates that marker into `components.ValueItem.Current`.
3. The session-list handler marks the item whose ID equals the active agent's
   session ID.
4. Value and session constructors carry the marker into their tray rows.
5. The shared tray engine selects the first selectable current row at
   construction and renders current, unselected primary text with the semantic
   blue token.
6. Filtering continues to select the first match. Clearing or changing a search
   does not unexpectedly jump the cursor back to the current value; the
   current marker remains visible whenever that row is in the filtered result.

If no row is current, behavior remains unchanged: the first selectable row is
selected and no row receives current styling. If malformed input marks multiple
rows current, the first selectable current row becomes the initial cursor while
all marked rows remain styled; catalog tests prevent this in Carbon.

## Testing

- Component tests prove initial cursor placement, persistent blue text after
  navigation, selected-row precedence, and window scrolling to a late current
  choice.
- Grouped model tests prove headings and spacers do not interfere with current
  selection.
- Presentation tests prove runtime markers survive option translation and the
  active session is marked from `Agent.SessionID()`.
- Carbon tests prove current mode, aliased model, and effort are marked exactly
  once.

