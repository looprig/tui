package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const offlineExamplesCommand = "GOWORK=off GOCACHE=/tmp/looprig-tui-docs-gocache go test ./examples/..."

type examplesManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Repository    string `json:"repository"`
	ProofSources  []struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Path   string `json:"path"`
		Symbol string `json:"symbol,omitempty"`
	} `json:"proofSources"`
	Examples []struct {
		ID             string            `json:"id"`
		Ecosystem      string            `json:"ecosystem"`
		Owner          string            `json:"owner"`
		SourcePath     string            `json:"sourcePath"`
		Availability   string            `json:"availability"`
		Versions       map[string]string `json:"versions"`
		OfflineCommand string            `json:"offlineCommand"`
		Assertion      string            `json:"assertion"`
		WorkflowPath   string            `json:"workflowPath"`
		JobID          string            `json:"jobId"`
		Cleanup        string            `json:"cleanup"`
		LiveGate       any               `json:"liveGate"`
		ProofIDs       []string          `json:"proofIds"`
	} `json:"examples"`
}

func TestRunnableExamplesExist(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"examples/sessionadapter/example_test.go",
		"examples/restore/example_test.go",
		"examples/runtimehost/example_test.go",
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(filepath.Join("..", path)); err != nil {
				t.Fatalf("runnable example %q: %v", path, err)
			}
		})
	}
}

func TestDocsExamplesArtifacts(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "testdata/docs/examples.json"))
	if err != nil {
		t.Fatalf("read examples manifest: %v", err)
	}
	var manifest examplesManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode examples manifest: %v", err)
	}
	if manifest.SchemaVersion != 1 || manifest.Repository != "tui" {
		t.Fatalf("manifest identity = schema %d repository %q", manifest.SchemaVersion, manifest.Repository)
	}
	wantProofs := map[string]struct {
		typeName string
		path     string
		symbol   string
	}{
		"example-tui-session-adapter-fixture": {"executable-fixture", "examples/sessionadapter/example_test.go", "Example_adapterOwnsOnlySessionPresentation"},
		"example-tui-restore-decider-fixture": {"executable-fixture", "examples/restore/example_test.go", "Example_restoreDecision"},
		"example-tui-runtime-host-fixture":    {"executable-fixture", "examples/runtimehost/example_test.go", "Example_runtimeHost"},
		"example-tui-session-adapter-source":  {"source", "sessionadapter/adapter.go", "New"},
		"example-tui-restore-decider-source":  {"source", "restore/decider.go", "Decider.DecideRestore"},
		"example-tui-runtime-host-source":     {"source", "runtime/run.go", "Run"},
		"example-tui-session-lifecycle-test":  {"test", "sessionadapter/adapter_test.go", "TestCloseShutsDownOnce"},
		"example-tui-runtime-lifecycle-test":  {"test", "runtime/run_test.go", "TestRunTeardownViaAgentHolder"},
		"example-tui-manifest-contract-test":  {"test", "tests/example_test.go", "TestDocsExamplesArtifacts"},
	}
	proofs := make(map[string]bool, len(manifest.ProofSources))
	for _, proof := range manifest.ProofSources {
		want, ok := wantProofs[proof.ID]
		if !ok {
			t.Errorf("unexpected proof source ID %q", proof.ID)
			continue
		}
		if proof.Type != want.typeName || proof.Path != want.path || proof.Symbol != want.symbol {
			t.Errorf("proof %q = type %q path %q symbol %q, want type %q path %q symbol %q", proof.ID, proof.Type, proof.Path, proof.Symbol, want.typeName, want.path, want.symbol)
		}
		if strings.Contains(proof.Path, "#") {
			t.Errorf("proof %q path contains symbol fragment: %q", proof.ID, proof.Path)
		}
		if _, err := os.Stat(filepath.Join("..", proof.Path)); err != nil {
			t.Errorf("proof %q path does not resolve: %v", proof.ID, err)
		}
		proofs[proof.ID] = true
	}
	if len(manifest.ProofSources) != len(wantProofs) {
		t.Errorf("proof source count = %d, want %d", len(manifest.ProofSources), len(wantProofs))
	}
	if len(manifest.Examples) != 3 {
		t.Fatalf("manifest examples = %d, want 3", len(manifest.Examples))
	}
	seen := make(map[string]bool, len(manifest.Examples))
	for _, example := range manifest.Examples {
		if seen[example.ID] {
			t.Errorf("duplicate example ID %q", example.ID)
		}
		seen[example.ID] = true
		if !strings.HasPrefix(example.ID, "example-tui-") || example.Ecosystem != "go" || example.Owner != "tui" || example.Availability != "source-workspace" {
			t.Errorf("example %q classification is incorrect", example.ID)
		}
		if example.Versions["github.com/looprig/tui"] != "source-workspace" || len(example.Versions) != 1 {
			t.Errorf("example %q versions = %#v", example.ID, example.Versions)
		}
		if example.OfflineCommand != offlineExamplesCommand {
			t.Errorf("example %q offlineCommand = %q", example.ID, example.OfflineCommand)
		}
		if example.SourcePath == "" || example.Assertion == "" || example.WorkflowPath != ".github/workflows/docs-examples.yml" || example.JobID != "docs-examples" || example.Cleanup == "" || example.LiveGate != nil {
			t.Errorf("example %q has incomplete execution metadata", example.ID)
		}
		if _, err := os.Stat(filepath.Join("..", example.SourcePath)); err != nil {
			t.Errorf("example %q sourcePath does not resolve: %v", example.ID, err)
		}
		for _, proofID := range example.ProofIDs {
			if !proofs[proofID] {
				t.Errorf("example %q references unknown proof %q", example.ID, proofID)
			}
		}
		if len(example.ProofIDs) < 3 {
			t.Errorf("example %q proofIds = %v, want fixture, source, and test proofs", example.ID, example.ProofIDs)
		}
	}

	workflow, err := os.ReadFile(filepath.Join("..", ".github/workflows/docs-examples.yml"))
	if err != nil {
		t.Fatalf("read docs examples workflow: %v", err)
	}
	for _, literal := range []string{
		"docs-examples:",
		offlineExamplesCommand,
		"GOWORK=off GOCACHE=/tmp/looprig-tui-docs-gocache make check",
		"GOWORK=off GOCACHE=/tmp/looprig-tui-docs-gocache go test -race ./...",
	} {
		if !strings.Contains(string(workflow), literal) {
			t.Errorf("workflow does not contain %q", literal)
		}
	}
}
