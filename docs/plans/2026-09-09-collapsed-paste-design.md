# Collapsed Composer Paste Design

## Problem

Carbon's composer safely accepts a terminal bracketed paste as one `tea.PasteMsg`, but
it inserts the full payload into the textarea. Large multiline pastes fill the composer
with many rows and make the interaction look like a burst of separate messages. The
composer should present multiline pasted input as one compact item while preserving the
exact payload for the eventual turn.

## Design

The input component will own a sequence of opaque pasted segments alongside the editable
textarea value. When it receives a multiline `tea.PasteMsg`, it will insert a unique,
human-readable marker such as `[pasted 123 chars]` into the textarea and retain the exact
paste bytes behind that marker. Single-line pastes continue through the textarea normally.

`Value` will remain the submission-facing API and expand every live marker back to its
original content. A separate display-value helper will expose the textarea's compact text
where presentation code needs to detect visible edits. Reset and SetValue will clear stale
paste segments. If editing removes or changes a marker, expansion leaves the edited text
alone and drops the unreachable payload; deleting the marker therefore deletes the whole
paste.

This keeps paste handling inside the component that already wraps the textarea. The screen
and interaction layers continue routing `tea.PasteMsg` exactly once and continue submitting
one `uiSubmit` action containing the expanded value.

## Testing

Component tests will prove that multiline pastes render as one marker, preserve the exact
payload through `Value`, and disappear as a unit when their marker is removed. Presentation
tests will prove the paste does not submit immediately and that one later Enter submits the
original multiline text exactly once. Existing single-line paste, completion, answer-field,
and input layout tests must remain green.
