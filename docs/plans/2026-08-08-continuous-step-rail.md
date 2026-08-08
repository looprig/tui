# Continuous Step Rail Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Keep the assistant-step rail continuous through wrapped AI and tool node text and render the rail with a quieter color.

**Architecture:** Change the shared `internal/view` rail-node primitive so continuation rows use the node column as a spine. Add a dedicated style in `styles` for rails and connectors, leaving node and content styles unchanged.

**Tech Stack:** Go, Lip Gloss v2, standard `testing`, Bubble Tea presentation tests.

---

### Task 1: Pin continuous wrapped node rendering

**Files:**
- Modify: `internal/view/rail_test.go`

1. Update the wrapped depth-zero node assertion to require `│ ` before continuation text.
2. Add a nested wrapped-node case requiring both the parent spine and node-column rail.
3. Run `go test -race ./internal/view -run 'TestRailNodeWrap'` and confirm it fails because continuation rows contain spaces.

### Task 2: Pin the quieter rail style

**Files:**
- Modify: `styles/styles_test.go`

1. Add a test that requires rail glyph rendering to use a dedicated subtle foreground style while `ThinkingStyle` remains unchanged.
2. Run `go test -race ./styles -run 'TestRailStyle'` and confirm it fails because the rail-specific style does not exist.

### Task 3: Implement the shared fix

**Files:**
- Modify: `internal/view/rail.go`
- Modify: `styles/styles.go`

1. Add the dedicated rail style.
2. Render all spine/connector glyphs with it.
3. Replace the blank node-column continuation indent with the corresponding rail spine.
4. Run the focused tests and confirm they pass.

### Task 4: Verify composition and build

**Files:**
- Test: `internal/presentation/render_test.go`

1. Run focused presentation connector and folding tests.
2. Run `make fmt` and inspect the scoped diff.
3. Run `go test -race ./...`.
4. Run `CGO_ENABLED=0 go build -trimpath ./...`.
