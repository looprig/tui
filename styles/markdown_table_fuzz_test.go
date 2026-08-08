package styles

import (
	"strings"
	"testing"
)

// FuzzMarkTableBodyBoundaries exercises the Markdown-facing parser with arbitrary
// model output. A selected marker must be absent from the input and present only when
// the adapter actually changed the document.
func FuzzMarkTableBodyBoundaries(f *testing.F) {
	f.Add("| A | B |\n| --- | --- |\n| one | long value |\n| two | another value |")
	f.Add("```markdown\n| A | B |\n| --- | --- |\n| one | value |\n| two | value |\n```")
	f.Add("ordinary prose\n\n- with a list")

	f.Fuzz(func(t *testing.T, markdown string) {
		marked, marker := markTableBodyBoundaries(markdown)
		if marker == "" {
			if marked != markdown {
				t.Fatalf("marker is empty but Markdown changed:\ngot  %q\nwant %q", marked, markdown)
			}
			return
		}
		if strings.Contains(markdown, marker) {
			t.Fatalf("selected marker %q already occurs in input", marker)
		}
		if !strings.Contains(marked, marker) {
			t.Fatalf("selected marker %q does not occur in transformed Markdown %q", marker, marked)
		}
	})
}
