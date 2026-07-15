package presentation

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestInternalPresentationPackageBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dir       string
		forbidden []string
	}{
		{name: "input is a leaf", dir: "../input", forbidden: []string{"github.com/looprig/tui", "github.com/looprig/tui/internal/model", "github.com/looprig/tui/internal/view"}},
		{name: "model does not render", dir: "../model", forbidden: []string{"github.com/looprig/tui", "github.com/looprig/tui/internal/view"}},
		{name: "presentation does not import facade", dir: ".", forbidden: []string{"github.com/looprig/tui"}},
		{name: "view does not import root", dir: "../view", forbidden: []string{"github.com/looprig/tui"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			entries, err := os.ReadDir(tt.dir)
			if err != nil {
				t.Fatalf("ReadDir(%q): %v", tt.dir, err)
			}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
					continue
				}
				path := filepath.Join(tt.dir, entry.Name())
				parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if err != nil {
					t.Fatalf("ParseFile(%q): %v", path, err)
				}
				for _, spec := range parsed.Imports {
					path, err := strconv.Unquote(spec.Path.Value)
					if err != nil {
						t.Fatalf("Unquote import %q: %v", spec.Path.Value, err)
					}
					for _, forbidden := range tt.forbidden {
						if path == forbidden {
							t.Errorf("%s imports forbidden package %s", entry.Name(), path)
						}
					}
				}
			}
		})
	}
}
