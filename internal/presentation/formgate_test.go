package presentation

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/looprig/core/uuid"
	"github.com/looprig/harness/pkg/event"
	"github.com/looprig/harness/pkg/gate"
	"github.com/looprig/harness/pkg/identity"
)

// styledCursor matches the ▸ focus cursor immediately preceded by an SGR escape — the
// evidence that the focused FORM field row is highlighted (CardSelectedStyle wraps the row,
// which begins with "▸ "), not rendered plain like the unfocused rows. Bold alone emits an
// SGR even with color off, so this holds across color profiles.
//
// It lives here because the form card is the last surface still using a glyph cursor: the
// choice and permission cards have moved to the banded styles.SelectedRow treatment, which
// carries no glyph at all.
var styledCursor = regexp.MustCompile("\x1b\\[[0-9;]*m▸")

// formGateID is the deterministic gate id the form fixtures carry.
var formGateID = gate.ID{0xf0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x01}

// formLoopID is the deterministic producing-loop id the form fixtures carry.
var formLoopID = uuid.UUID{0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xa0, 0xb0, 0xc0, 0xd0, 0xe0, 0xf0, 0x02}

// formSchema is the fixture schema: one required text field, one select, one
// confirm — the three kinds a form gate may ask for.
func formSchema() gate.PromptSchema {
	return gate.PromptSchema{Fields: []gate.Field{
		{Name: "username", Label: "Username", Kind: gate.FieldText, Required: true},
		{Name: "region", Label: "Region", Kind: gate.FieldSelect, Options: []gate.Option{
			{Value: "eu", Label: "Europe"},
			{Value: "us", Label: "United States"},
		}},
		{Name: "consent", Label: "Share telemetry", Kind: gate.FieldConfirm},
	}}
}

// formGateOpened builds the GateOpened envelope the TUI folds. schema and controls
// vary per test; everything else is the fixture's.
func formGateOpened(schema gate.PromptSchema, controls ...string) event.GateOpened {
	prompt := gate.Prompt{Title: "Sign in to Example", Body: "The integration needs your details.", Schema: schema}
	for _, c := range controls {
		prompt.Controls = append(prompt.Controls, gate.Control{Action: c, Label: c})
	}
	return event.GateOpened{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: formLoopID}},
		Gate: gate.Gate{
			ID:       formGateID,
			Kind:     gate.KindForm,
			Resolver: gate.ResolverSession,
			Prompt:   prompt,
		},
	}
}

// formModel folds a form GateOpened into a fresh interaction model.
func formModel(schema gate.PromptSchema, controls ...string) interactionModel {
	return newInteractionModel().ApplyEvent(formGateOpened(schema, controls...))
}

// renderForm renders the active form card the way the surface does.
func renderForm(t *testing.T, m interactionModel) string {
	t.Helper()
	p := m.ActivePrompt()
	if p == nil {
		t.Fatal("no active prompt to render")
	}
	return renderFormBox(*p, 60, m.PendingCount())
}

// TestFormGateFoldsAndRendersFromGateOpened is the render-driving proof that the
// TUI renders a form gate out of the PUBLIC envelope: every label, the chosen
// select option, and the confirm state must appear on screen.
func TestFormGateFoldsAndRendersFromGateOpened(t *testing.T) {
	t.Parallel()
	m := formModel(formSchema(), gate.FormActionAccept, gate.FormActionDecline)
	if m.mode != modeFormPrompt {
		t.Fatalf("mode = %v, want modeFormPrompt", m.mode)
	}
	if m.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", m.PendingCount())
	}
	got := stripANSI(renderForm(t, m))
	for _, want := range []string{
		"Sign in to Example",                 // the title
		"The integration needs your details", // the body
		"Username *",                         // the required mark
		"Region",
		"Europe", // the default select option's LABEL
		"Share telemetry",
		"no", // the confirm's initial state
	} {
		if !strings.Contains(got, want) {
			t.Errorf("form card missing %q in:\n%s", want, got)
		}
	}
	assertPanelFramed(t, renderForm(t, m))
}

// TestFormGateRendersFocusCursor proves the focused field is visibly highlighted
// and that ↓ moves the highlight — the editor's only affordance for "which field
// am I typing into".
func TestFormGateRendersFocusCursor(t *testing.T) {
	t.Parallel()
	m := formModel(formSchema(), gate.FormActionAccept)
	first := renderForm(t, m)
	if !styledCursor.MatchString(first) {
		t.Errorf("focused field is not highlighted:\n%s", first)
	}
	if !strings.Contains(stripANSI(first), "▸ Username") {
		t.Errorf("cursor does not start on the first field:\n%s", stripANSI(first))
	}
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if !strings.Contains(stripANSI(renderForm(t, m)), "▸ Region") {
		t.Errorf("down did not move focus to Region:\n%s", stripANSI(renderForm(t, m)))
	}
}

// TestFormGateRejectsMultiSelect is the graceful-degradation case. A multi-select
// cannot be represented in a form answer (gate.ValidateFormSchema refuses one in a
// payload), but Gate.Prompt is a projection that is NOT run through that
// validation — so the TUI must refuse to render an editor rather than silently
// submit one selection where the user believed they chose several.
func TestFormGateRejectsMultiSelect(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema gate.PromptSchema
	}{
		{
			name: "multi-select field",
			schema: gate.PromptSchema{Fields: []gate.Field{
				{Name: "tags", Label: "Tags", Kind: gate.FieldMultiSelect, Options: []gate.Option{{Value: "a"}, {Value: "b"}}},
			}},
		},
		{
			name: "unknown field kind",
			schema: gate.PromptSchema{Fields: []gate.Field{
				{Name: "mystery", Kind: gate.FieldKind("invented")},
			}},
		},
		{
			name: "select with no options",
			schema: gate.PromptSchema{Fields: []gate.Field{
				{Name: "region", Kind: gate.FieldSelect},
			}},
		},
		{
			name:   "empty schema",
			schema: gate.PromptSchema{},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := formModel(tt.schema, gate.FormActionAccept, gate.FormActionDecline)
			p := m.ActivePrompt()
			if p == nil {
				t.Fatal("an unrenderable form must still be shown so it can be declined")
			}
			if !p.unsupported {
				t.Fatal("schema was accepted as renderable")
			}
			if len(p.Fields) != 0 {
				t.Fatalf("unsupported form built %d editable field(s); it must build none", len(p.Fields))
			}
			got := stripANSI(renderForm(t, m))
			if !strings.Contains(got, "cannot show") {
				t.Errorf("no unsupported notice in:\n%s", got)
			}
			// Enter must not submit an answer the user never gave.
			_, action, _ := m.formKey(tea.KeyPressMsg{Code: tea.KeyEnter})
			if action.Kind != uiNoop {
				t.Errorf("enter on an unsupported form produced %v, want uiNoop", action.Kind)
			}
			// Decline remains available: it is the only honest action left.
			_, action, _ = m.formKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			if action.Kind != uiGateRespond || action.GateAction != gate.FormActionDecline {
				t.Errorf("esc = %v/%q, want a decline", action.Kind, action.GateAction)
			}
		})
	}
}

// TestFormGateAcceptEncodesSchemaTypes is the answer-path proof. It types a value,
// cycles the select, toggles the confirm, and submits — then asserts the response
// carries each field as the JSON TYPE its kind requires. A confirm encoded as the
// string "true" rather than the bool true would be refused by
// gate.ParseFormAnswers, so the types here are the contract.
func TestFormGateAcceptEncodesSchemaTypes(t *testing.T) {
	t.Parallel()
	m := formModel(formSchema(), gate.FormActionAccept, gate.FormActionDecline)

	for _, r := range "ada" {
		m, _, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // Europe → United States
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace}) // toggle the confirm on

	// The typed value must be visible before it is submitted.
	if got := stripANSI(renderForm(t, m)); !strings.Contains(got, "ada") {
		t.Errorf("typed text not rendered:\n%s", got)
	}

	m, action, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action.Kind != uiGateRespond {
		t.Fatalf("enter produced %v, want uiGateRespond", action.Kind)
	}
	if action.GateAction != gate.FormActionAccept {
		t.Errorf("action = %q, want accept", action.GateAction)
	}
	if action.GateID != formGateID {
		t.Errorf("gate id = %v, want %v", action.GateID, formGateID)
	}
	want := map[string]string{
		"username": `"ada"`, // text → JSON string
		"region":   `"us"`,  // select → the option VALUE, not its label
		"consent":  `true`,  // confirm → JSON bool
	}
	for name, wantRaw := range want {
		got, ok := action.Values[name]
		if !ok {
			t.Errorf("no value submitted for %q", name)
			continue
		}
		if string(got) != wantRaw {
			t.Errorf("value[%q] = %s, want %s", name, got, wantRaw)
		}
	}
	// The answered gate is gone from the queue.
	if m.PendingCount() != 0 {
		t.Errorf("pending after accept = %d, want 0", m.PendingCount())
	}
}

// TestFormGateRequiresRequiredFields proves an incomplete submit is refused
// locally rather than sent for the session to reject.
func TestFormGateRequiresRequiredFields(t *testing.T) {
	t.Parallel()
	m := formModel(formSchema(), gate.FormActionAccept, gate.FormActionDecline)

	m, action, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action.Kind != uiNoop {
		t.Fatalf("submitting an unanswered required field produced %v, want uiNoop", action.Kind)
	}
	if m.PendingCount() != 1 {
		t.Fatalf("the form was popped despite not being submitted")
	}
	if got := stripANSI(renderForm(t, m)); !strings.Contains(got, "required") {
		t.Errorf("no validation notice after an incomplete submit:\n%s", got)
	}
	// Typing clears the notice and unblocks the submit.
	m, _, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	if got := stripANSI(renderForm(t, m)); strings.Contains(got, "required") {
		t.Errorf("validation notice survived an edit:\n%s", got)
	}
	_, action, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if action.Kind != uiGateRespond {
		t.Errorf("a completed form did not submit: %v", action.Kind)
	}
}

// TestFormGateHonorsOfferedControls is the fail-closed boundary at the key router.
// The session refuses any action a gate never offered (validateGateAction), so a
// key bound to an unoffered action must do nothing rather than send a doomed
// request.
func TestFormGateHonorsOfferedControls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		controls []string
		key      tea.KeyPressMsg
		wantKind uiActionKind
	}{
		{name: "decline offered", controls: []string{gate.FormActionAccept, gate.FormActionDecline}, key: tea.KeyPressMsg{Code: tea.KeyEsc}, wantKind: uiGateRespond},
		{name: "decline not offered", controls: []string{gate.FormActionAccept}, key: tea.KeyPressMsg{Code: tea.KeyEsc}, wantKind: uiNoop},
		{name: "accept not offered", controls: []string{gate.FormActionDecline}, key: tea.KeyPressMsg{Code: tea.KeyEnter}, wantKind: uiNoop},
		{name: "no controls at all", controls: nil, key: tea.KeyPressMsg{Code: tea.KeyEsc}, wantKind: uiNoop},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// A single optional text field, so accept is never blocked by validation.
			schema := gate.PromptSchema{Fields: []gate.Field{{Name: "note", Kind: gate.FieldText}}}
			m := formModel(schema, tt.controls...)
			_, action, _ := m.formKey(tt.key)
			if action.Kind != tt.wantKind {
				t.Fatalf("action = %v, want %v", action.Kind, tt.wantKind)
			}
		})
	}
}

// TestFormGateResolvedElsewhereClearsPrompt proves a gate answered by someone else
// (a policy timeout, another client) stops asking this user for an answer.
func TestFormGateResolvedElsewhereClearsPrompt(t *testing.T) {
	t.Parallel()
	m := formModel(formSchema(), gate.FormActionAccept)
	if m.PendingCount() != 1 {
		t.Fatalf("pending = %d, want 1", m.PendingCount())
	}
	m = m.ApplyEvent(event.GateResolved{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: formLoopID}},
		GateID: formGateID,
		Reason: gate.ClosePolicyResponse,
	})
	if m.PendingCount() != 0 {
		t.Fatalf("pending after GateResolved = %d, want 0", m.PendingCount())
	}
	if m.mode != modeCompose {
		t.Errorf("mode = %v, want modeCompose once the queue drained", m.mode)
	}
}

// TestGateResolvedLeavesNonFormPromptsAlone guards the zero-gate-id case.
//
// Permission and AskUser prompts are enqueued from their own per-kind events and
// carry NO gate id. A GateResolved naming a different gate must obviously not
// touch them — but the case that bites is a GateResolved carrying a ZERO gate id
// (a malformed or replayed record): without the zero guard it would match every
// one of those prompts at once and silently wipe the queue, leaving the user with
// no way to answer gates that are still open.
func TestGateResolvedLeavesNonFormPromptsAlone(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		resolved gate.ID
	}{
		{name: "a different gate", resolved: formGateID},
		{name: "a zero gate id matches nothing", resolved: gate.ID{}},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newInteractionModel()
			m = m.ApplyEvent(event.UserInputRequested{
				Header:          event.Header{Coordinates: identity.Coordinates{LoopID: formLoopID}},
				ToolExecutionID: uuid.UUID{0x09},
				Question:        "which branch?",
			})
			m = m.ApplyEvent(event.PermissionRequested{
				Header:          event.Header{Coordinates: identity.Coordinates{LoopID: formLoopID}},
				ToolExecutionID: uuid.UUID{0x0a},
				Request:         bashPermission("ls"),
			})
			if m.PendingCount() != 2 {
				t.Fatalf("pending = %d, want 2", m.PendingCount())
			}
			m = m.ApplyEvent(event.GateResolved{
				Header: event.Header{Coordinates: identity.Coordinates{LoopID: formLoopID}},
				GateID: tt.resolved,
			})
			if m.PendingCount() != 2 {
				t.Fatalf("GateResolved cleared unrelated gate-id-less prompts (pending = %d, want 2)", m.PendingCount())
			}
		})
	}
}

// TestFormGateDedupesByGateID proves the append-once rule keys a form gate on its
// GATE id. An integration's form gate need not belong to a tool call, so two such
// gates on one loop both carry a zero ToolExecutionID — deduping on that pair
// would silently drop the second.
func TestFormGateDedupesByGateID(t *testing.T) {
	t.Parallel()
	m := newInteractionModel()
	first := formGateOpened(formSchema(), gate.FormActionAccept)
	m = m.ApplyEvent(first)
	m = m.ApplyEvent(first) // the same gate re-delivered — must not double-queue
	if m.PendingCount() != 1 {
		t.Fatalf("a re-delivered gate queued twice (pending = %d)", m.PendingCount())
	}

	second := formGateOpened(formSchema(), gate.FormActionAccept)
	second.Gate.ID = gate.ID{0xf0, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0x02}
	m = m.ApplyEvent(second)
	if m.PendingCount() != 2 {
		t.Fatalf("a DIFFERENT gate with the same (zero) tool-execution id was dropped (pending = %d)", m.PendingCount())
	}
}

// TestFormGateIgnoresNonFormGateOpened proves the fold is narrow: a permission
// gate also emits GateOpened, and folding it here would double-queue the prompt
// its own PermissionRequested already enqueued.
func TestFormGateIgnoresNonFormGateOpened(t *testing.T) {
	t.Parallel()
	m := newInteractionModel().ApplyEvent(event.GateOpened{
		Header: event.Header{Coordinates: identity.Coordinates{LoopID: formLoopID}},
		Gate: gate.Gate{
			ID:       formGateID,
			Kind:     gate.KindPermission,
			Resolver: gate.ResolverLoop,
			Prompt:   gate.Prompt{Controls: []gate.Control{{Action: "approve"}}},
		},
	})
	if m.PendingCount() != 0 {
		t.Fatalf("a permission GateOpened enqueued a form prompt (pending = %d)", m.PendingCount())
	}
}

// TestFormGateTextEntryRejectsModifiedKeys proves a control chord cannot be typed
// into an answer as a literal.
func TestFormGateTextEntryRejectsModifiedKeys(t *testing.T) {
	t.Parallel()
	schema := gate.PromptSchema{Fields: []gate.Field{{Name: "note", Kind: gate.FieldText}}}
	m := formModel(schema, gate.FormActionAccept)
	m, _, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c", Mod: tea.ModCtrl})
	if got := m.ActivePrompt().Fields[0].value(); got != "" {
		t.Fatalf("a modified key was typed into the field: %q", got)
	}
	m, _, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if got := m.ActivePrompt().Fields[0].value(); got != "c" {
		t.Fatalf("an unmodified rune did not type: %q", got)
	}
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.ActivePrompt().Fields[0].value(); got != "" {
		t.Fatalf("backspace did not delete: %q", got)
	}
}

// TestFormFieldEncodeOmitsUnansweredOptional proves an optional field the user left
// blank is omitted rather than submitted as an empty string: the schema
// distinguishes "not answered" from "answered empty".
func TestFormFieldEncodeOmitsUnansweredOptional(t *testing.T) {
	t.Parallel()
	schema := gate.PromptSchema{Fields: []gate.Field{{Name: "note", Kind: gate.FieldText}}}
	m := formModel(schema, gate.FormActionAccept)
	_, action := m.submitForm(*m.ActivePrompt())
	if action.Kind != uiGateRespond {
		t.Fatalf("action = %v, want uiGateRespond", action.Kind)
	}
	if _, ok := action.Values["note"]; ok {
		t.Errorf("an unanswered optional field was submitted: %s", action.Values["note"])
	}
}

// TestFormGateDefaultsApply proves a schema default pre-fills the editor.
func TestFormGateDefaultsApply(t *testing.T) {
	t.Parallel()
	schema := gate.PromptSchema{Fields: []gate.Field{
		{Name: "note", Kind: gate.FieldText, Default: json.RawMessage(`"hello"`)},
		{Name: "region", Kind: gate.FieldSelect, Default: json.RawMessage(`"us"`), Options: []gate.Option{
			{Value: "eu"}, {Value: "us"},
		}},
		{Name: "consent", Kind: gate.FieldConfirm, Default: json.RawMessage(`true`)},
	}}
	m := formModel(schema, gate.FormActionAccept)
	p := m.ActivePrompt()
	if p.Fields[0].value() != "hello" {
		t.Errorf("text default = %q, want %q", p.Fields[0].value(), "hello")
	}
	if p.Fields[1].Choice != 1 {
		t.Errorf("select default index = %d, want 1 (us)", p.Fields[1].Choice)
	}
	if !p.Fields[2].Confirm {
		t.Error("confirm default did not apply")
	}
}

// oneTextField is a single-text-field schema carrying def as its JSON default (""
// for no default). It is the minimum fixture for the text EDITOR tests, which care
// about the field's value and nothing about the surrounding form.
func oneTextField(def string) gate.PromptSchema {
	f := gate.Field{Name: "note", Label: "Note", Kind: gate.FieldText}
	if def != "" {
		f.Default = json.RawMessage(def)
	}
	return gate.PromptSchema{Fields: []gate.Field{f}}
}

// TestFormFieldCursorMoves proves a text field carries a real CURSOR. The old
// editor could only append and trim from the END, so ← was a no-op on a text
// field and backspace always deleted the last rune; here ← steps the cursor back
// into the value and backspace deletes the rune it now sits after.
func TestFormFieldCursorMoves(t *testing.T) {
	t.Parallel()
	m := formModel(oneTextField(`"abcdef"`), gate.FormActionAccept)
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.ActivePrompt().Fields[0].value(); got != "abcdf" {
		t.Fatalf("left then backspace = %q, want %q", got, "abcdf")
	}
}

// TestFormFieldAcceptsSpace is the space-bar regression guard. A keypress
// stringifies to "space", not " ", so any printable-input filter written against
// msg.String() would silently swallow the space bar and make multi-word answers
// untypeable.
func TestFormFieldAcceptsSpace(t *testing.T) {
	t.Parallel()
	m := formModel(oneTextField(""), gate.FormActionAccept)
	m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if got := m.ActivePrompt().Fields[0].value(); got != " " {
		t.Fatalf("space typed %q, want %q", got, " ")
	}
}

// TestFormFieldGrows proves a long value WRAPS onto extra card rows rather than
// being clipped into a one-row field. The tail of the value must survive to the
// screen: the old renderer truncated the row to the card width and put an ellipsis
// where the rest of the answer was.
func TestFormFieldGrows(t *testing.T) {
	t.Parallel()
	const width = 40
	const long = "the quick brown fox jumps over the lazy dog and then keeps running"

	shortModel := formModel(oneTextField(`"hi"`), gate.FormActionAccept)
	grownModel := formModel(oneTextField(strconv.Quote(long)), gate.FormActionAccept)
	short := renderFormBox(*shortModel.ActivePrompt(), width, 1)
	grown := renderFormBox(*grownModel.ActivePrompt(), width, 1)

	shortRows, grownRows := strings.Count(short, "\n"), strings.Count(grown, "\n")
	if grownRows <= shortRows {
		t.Errorf("a wrapping value did not grow the card: %d rows against %d for a short value:\n%s", grownRows, shortRows, stripANSI(grown))
	}
	if got := stripANSI(grown); !strings.Contains(got, "running") {
		t.Errorf("the tail of the value never reached the card:\n%s", got)
	}
}

// TestFormFieldBoundsValueLength pins the per-field rune bound. The session refuses an
// over-long answer (gate.ParseFormAnswers caps a value in BYTES), so the field refuses
// the keystroke that would take it past maxFormFieldRunes rather than letting the user
// write a long answer only to have the whole of it come back rejected.
//
// The bound now lives on the editor as its CharLimit rather than in a hand-rolled length
// check, which is exactly why it is worth pinning: it is one field assignment away from
// disappearing without a trace.
func TestFormFieldBoundsValueLength(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("a", maxFormFieldRunes)
	m := formModel(oneTextField(strconv.Quote(full)), gate.FormActionAccept)
	m, _, _ = m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	got := m.ActivePrompt().Fields[0].value()
	if n := len([]rune(got)); n != maxFormFieldRunes {
		t.Fatalf("value is %d runes after typing past the bound, want %d", n, maxFormFieldRunes)
	}
	if strings.Contains(got, "b") {
		t.Error("a keystroke past the bound was accepted")
	}
}

// assertFormCardFits fails when any row of a rendered form card is wider than the card.
//
// An over-wide row is not a cosmetic problem. cardFrame lays the card out row by row and
// the surface reserves a height from that count, so a row the TERMINAL has to fold pushes
// every row beneath it one line down, off its ▌ rail and out of the reservation.
//
// Rows are measured in DISPLAY CELLS on the styled string, which is the unit the terminal
// lays a row out in: a rune count would let two CJK columns pass for one, and a cell count
// taken after stripping ANSI would miss nothing but is no safer.
func assertFormCardFits(t *testing.T, card string, width int, what string) {
	t.Helper()
	for i, line := range strings.Split(card, "\n") {
		if got := ansi.StringWidth(line); got > width {
			t.Errorf("%s: row %d is %d cells wide in a %d-column card (over by %d): %q",
				what, i, got, width, got-width, stripANSI(line))
		}
	}
}

// formWidthSchema is the width-guard fixture: one required text field seeded with value,
// plus a select and a confirm whose labels are deliberately wider than a narrow card, so
// the guard covers a clipped lead-in as well as a wrapped one.
func formWidthSchema(label, value string) gate.PromptSchema {
	return gate.PromptSchema{Fields: []gate.Field{
		{Name: "note", Label: label, Kind: gate.FieldText, Required: true, Default: json.RawMessage(strconv.Quote(value))},
		{Name: "region", Label: "Region", Kind: gate.FieldSelect, Options: []gate.Option{
			{Value: "eu", Label: strings.Repeat("Europe ", 6)},
		}},
		{Name: "consent", Label: strings.Repeat("Share telemetry ", 3), Kind: gate.FieldConfirm},
	}}
}

// TestFormCardRowsFitCardWidth pins every row of a form card to the card width, across
// card widths, field values and which field holds the focus.
//
// A GROWING field makes this sharper than a fixed one: the row's height is a function of
// the value AND the columns the label leaves it, so the column budget has to be right at
// every width rather than merely at the one the card is usually drawn at. The values
// include a token with nowhere to break and CJK text, because a 2-cell rune must not be
// allowed to straddle the limit.
func TestFormCardRowsFitCardWidth(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"empty":          "",
		"short":          "ada",
		"wrapping prose": strings.Repeat("value ", 12),
		"unbroken token": strings.Repeat("x", 80),
		"cjk":            strings.Repeat("日本語", 14),
		"cjk and spaces": strings.Repeat("日本 ", 14),
	}
	labels := map[string]string{
		"short label": "Note",
		"long label":  strings.Repeat("a rather long field label ", 3),
	}

	// The narrow end matters most: the cursor column and the "<label>: " lead-in are
	// emitted whether or not the card has room for them, so a card too narrow to hold
	// its own chrome is where an unclamped row runs over — the same failure keyRow
	// clamps for. Three columns is the floor of the card itself, not of the rows: the ▌
	// rail takes cardRailWidth and cardTextWidth floors the body at one more, so a
	// narrower card cannot be asked for and is not a claim this makes.
	for _, width := range []int{3, 4, 6, 9, 12, 16, 20, 28, 40, 60, 80} {
		for labelName, label := range labels {
			for valueName, value := range values {
				name := strconv.Itoa(width) + "/" + labelName + "/" + valueName
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					m := formModel(formWidthSchema(label, value), gate.FormActionAccept, gate.FormActionDecline)
					// Walk the focus over every field: only the focused row is banded, and
					// the band is exactly what pads a row to the card without clipping it.
					for focus := 0; focus < 3; focus++ {
						p := m.ActivePrompt()
						assertFormCardFits(t, renderFormBox(*p, width, 1), width, name+" focus="+strconv.Itoa(focus))
						// The validation notice is a card row too, and it appears only after
						// a refused submit.
						invalid := *p
						invalid.invalid = true
						assertFormCardFits(t, renderFormBox(invalid, width, 1), width, name+" invalid focus="+strconv.Itoa(focus))
						m, _, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
					}
					// An unsupported schema is a card too — a different body and a
					// different legend, and no field rows to hide behind.
					unsupported := *m.ActivePrompt()
					unsupported.unsupported = true
					assertFormCardFits(t, renderFormBox(unsupported, width, 1), width, name+" unsupported")
				})
			}
		}
	}
}

// TestFormCardWidthSurvivesGrowth re-checks the card after EVERY keystroke. A field that
// grows changes the card's shape as the user types, so the row budget has to hold at each
// intermediate value — including the keystroke that tips the value onto a new row, which
// is exactly where an off-by-one in the column budget shows up.
func TestFormCardWidthSurvivesGrowth(t *testing.T) {
	t.Parallel()

	texts := map[string]string{
		"ascii": "the quick brown fox jumps over the lazy dog",
		"cjk":   "日本語のテキストが折り返されるところ",
	}
	for _, width := range []int{3, 8, 16, 24, 34, 48} {
		for name, text := range texts {
			name := strconv.Itoa(width) + "/" + name
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				m := formModel(formSchema(), gate.FormActionAccept, gate.FormActionDecline)
				for i, r := range text {
					m, _, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
					card := renderFormBox(*m.ActivePrompt(), width, 1)
					assertFormCardFits(t, card, width, name+" after keystroke "+strconv.Itoa(i))
				}
			})
		}
	}
}
