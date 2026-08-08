package styles

import (
	"strings"
	"testing"
)

func FuzzRenderMarkdownTables(f *testing.F) {
	f.Add("| A | Notes |\n|---|---|\n| one | a sufficiently long narrative value with several words |", 40)
	f.Add("| A | Notes |\n|---|---|\n| one | a sufficiently long narrative value with several words |\n| two | <!A |", 40)
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

		cellRenderFailed := false
		for _, table := range parseResponsiveTables(markdown, width) {
			cells := append([]markdownTableCell(nil), table.headers...)
			for _, row := range table.rows {
				cells = append(cells, row...)
			}
			for _, cell := range cells {
				if _, cellErr := renderTableCell(r, cell.raw); cellErr != nil {
					cellRenderFailed = true
					break
				}
			}
		}
		if cellRenderFailed {
			direct, directErr := r.Render(markdown)
			if directErr != nil {
				t.Fatalf("direct Render(): %v", directErr)
			}
			if out != direct {
				t.Fatalf("responsive cell render failed without whole-document fallback")
			}
		}
	})
}
