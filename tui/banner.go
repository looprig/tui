package tui

import "strings"

// AgentBanner is the agent metadata shown as the startup info notice — its Name and
// Description, threaded in at construction from the composition root (cmd/swe) so the
// Agent interface need not expose them. The zero value renders a name-less banner;
// bannerText degrades gracefully when either field is empty.
//
// It lives in its own file (not the shell) because it is shared state on the embedded
// sessionCore that BOTH the composition root and the presentation shell read — see
// sessioncore.go's banner field and Screen's use of bannerText.
type AgentBanner struct {
	Name        string
	Description string

	// Greeting is the OPTIONAL, UI-only startup greeting (§5a): a deterministic, already-
	// built capability description (composed by the composition root from the agent
	// registry — never the model). When non-empty it is committed as a SECOND opening
	// transcript notice, after the banner, by the systemReady handler. It is purely a
	// rendered opening entry — NOT a turn, NOT a command, never in the model's context —
	// so the active loop's history stays empty until the first real user message. Empty
	// (the default-off case) → no greeting entry, behavior identical to today.
	Greeting string
}

// bannerText renders the startup banner line from the agent metadata: "<Name> —
// <Description>" when both are present, just the Name when the description is empty,
// just the Description when the name is empty, and a neutral fallback when both are
// empty (the notice still marks the session start). It degrades rather than emitting
// a dangling separator.
func (b AgentBanner) bannerText() string {
	name, desc := strings.TrimSpace(b.Name), strings.TrimSpace(b.Description)
	switch {
	case name != "" && desc != "":
		return name + " — " + desc
	case name != "":
		return name
	case desc != "":
		return desc
	default:
		return "session ready"
	}
}
