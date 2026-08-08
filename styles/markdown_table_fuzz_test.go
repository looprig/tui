package styles

import (
	"strings"
	"testing"
)

func FuzzRenderMarkdownTables(f *testing.F) {
	f.Add("| A | Notes |\n|---|---|\n| one | a sufficiently long narrative value with several words |", 40)
	f.Add("```markdown\n| A | B |\n|---|---|\n| one | two |\n```", 30)
	f.Add("ordinary prose\n\n- with a list", 80)

	f.Fuzz(func(t *testing.T, markdown string, width int) {
		if width < 1 || width > 240 {
			t.Skip()
		}
		r, err := NewMarkdownRenderer(width)
		if err != nil {
			t.Fatalf("NewMarkdownRenderer(%d): %v", width, err)
		}
		out, err := RenderMarkdown(r, markdown, width)
		if err != nil {
			t.Fatalf("RenderMarkdown(): %v", err)
		}
		if strings.TrimSpace(markdown) != "" && strings.TrimSpace(out) == "" {
			t.Fatalf("non-empty Markdown rendered empty")
		}
	})
}
