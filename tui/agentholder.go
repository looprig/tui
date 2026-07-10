package tui

// AgentHolder is the narrow, read-only view of a presentation shell that the CLI
// composition root (cli/run.go — a DIFFERENT package) type-asserts against at teardown:
// it exposes ONLY the live Agent, so Run can bound a best-effort Close of whichever agent
// a /clear may have swapped in, without depending on either concrete shell type. Both
// Screen and ModernScreen satisfy it through the Agent() method promoted from the embedded
// sessionCore (a value receiver), so Run asserts the final tea.Model against this one
// interface and teardown works whichever shell cli.Run wires. It is exported because the
// cross-package assertion in cli/run.go needs the name, and deliberately tiny — one method
// — so the composition root depends on nothing it does not use (interface segregation).
type AgentHolder interface {
	Agent() Agent
}

// Compile-time assertions that BOTH presentation shells satisfy AgentHolder, so cli/run.go's
// teardown assertion resolves for whichever shell is wired (ModernScreen today, Screen as the
// unwired legacy fallback). Agent() is a value receiver on the embedded sessionCore, so the
// zero-value struct literals satisfy the interface — no fields are needed to promote it.
var (
	_ AgentHolder = Screen{}
	_ AgentHolder = ModernScreen{}
)
