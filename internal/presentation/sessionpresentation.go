package presentation

import "strings"

// SessionPresentation is the synchronous, consumer-supplied session metadata the TUI
// displays. The TUI never queries it asynchronously and never infers it from events:
// the composition root (CodeRig) fills it at screen construction (WithSessionPresentation)
// and, on a reopen, the TUI refreshes it from the replacement Agent via SessionPresenter,
// because the workspace, the fixed access profile, and the permission diagnostics are known
// before the session runs a single turn. A cross-session browser resume therefore displays
// the RESUMED session's context, not the prior one (see SessionPresenter and
// handleReopenResult).
//
//   - WorkspaceRoot is the session's workspace path, shown in the footer metadata.
//   - ProfileName is the FIXED access profile's display name. It is shown as session
//     metadata (the footer), NOT as a mutable control — there is no way to change the
//     profile from the TUI, so it must not look changeable.
//   - PermissionDiagnostics are display-ready notices for manual, out-of-catalog allow
//     families the consumer detected. They MUST be visible before the first permission
//     gate, so the Screen commits them in the startup metadata area (before any event,
//     and therefore before any gate, can arrive).
type SessionPresentation struct {
	WorkspaceRoot         string
	ProfileName           string
	PermissionDiagnostics []string
}

// footerParts returns the non-empty session-metadata fragments the footer renders
// AFTER the agent name: the fixed profile name, then the workspace root. Order is
// stable (profile before workspace) so the footer header reads identically each frame.
func (p SessionPresentation) footerParts() []string {
	parts := make([]string, 0, 2)
	if name := strings.TrimSpace(p.ProfileName); name != "" {
		parts = append(parts, name)
	}
	if root := strings.TrimSpace(p.WorkspaceRoot); root != "" {
		parts = append(parts, root)
	}
	return parts
}

// diagnostics returns the trimmed, non-empty permission diagnostic lines, in order.
func (p SessionPresentation) diagnostics() []string {
	out := make([]string, 0, len(p.PermissionDiagnostics))
	for _, d := range p.PermissionDiagnostics {
		if trimmed := strings.TrimSpace(d); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// SessionPresenter is the OPTIONAL capability a reopened or resumed Agent implements to
// supply its OWN SessionPresentation, so the reopen path can refresh the footer + pre-gate
// permission diagnostics to the REPLACEMENT session's security context instead of retaining
// the prior session's. It mirrors the RuntimeCatalog/RuntimeController optional-interface
// pattern: the Screen detects it on the swapped Agent by type assertion.
//
// CONTRACT (CodeRig Phase 5 fills this): the composition root's agent implements
// SessionPresentation() returning the session's fixed access profile name, workspace root,
// and any manual out-of-catalog permission diagnostics — the same values it would pass to
// WithSessionPresentation at construction, but read from the RESUMED session. A cross-session
// browser resume may land on a session with a DIFFERENT workspace root and DIFFERENT fixed
// profile, so this is the authority for the resumed session's displayed security context.
//
// An Agent that does NOT implement it degrades safely: a cross-session browser resume CLEARS
// the presentation (empty ⇒ show nothing, never a different session's context), while a
// /clear reopen (same session family, same workspace + fixed profile) retains the
// construction-time value, which is still correct. Showing nothing is acceptable; showing a
// different session's security context is not.
type SessionPresenter interface {
	SessionPresentation() SessionPresentation
}

// WithSessionPresentation supplies the synchronous session metadata (workspace, fixed
// profile, permission diagnostics) the Screen captures at construction. It is
// consumer-filled and minimal; an omitted option yields the zero presentation (no
// profile, no workspace, no diagnostics).
func WithSessionPresentation(p SessionPresentation) Option {
	return func(options *screenOptions) { options.presentation = p }
}
