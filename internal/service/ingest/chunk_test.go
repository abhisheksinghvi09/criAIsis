package ingest_test

import (
	"strings"
	"testing"

	"criaisis/internal/service/ingest"
)

// A severed code fence would hand a specialist half a diagnostic query, which is
// worse than handing it none.
func TestSplit_KeepsFencedCodeBlocksIntact(t *testing.T) {
	content := `# Runbook

Intro paragraph.

` + "```sql" + `
SELECT pid, state, query
FROM pg_stat_activity

WHERE state = 'idle in transaction';
` + "```" + `

Trailing paragraph.`

	chunks := ingest.Split(content)
	if len(chunks) == 0 {
		t.Fatal("expected at least one chunk")
	}

	var fenceFound bool
	for _, c := range chunks {
		opens := strings.Count(c.Text, "```")
		if opens%2 != 0 {
			t.Fatalf("chunk splits a code fence: %q", c.Text)
		}
		if strings.Contains(c.Text, "SELECT pid") {
			fenceFound = true
			if !strings.Contains(c.Text, "idle in transaction") {
				t.Error("code block body was split across chunks")
			}
		}
	}
	if !fenceFound {
		t.Error("code block disappeared during chunking")
	}
}

func TestSplit_OverlapsConsecutiveChunks(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 40; i++ {
		sb.WriteString(strings.Repeat("connection pool saturation evidence ", 8))
		sb.WriteString("\n\n")
	}

	chunks := ingest.Split(sb.String())
	if len(chunks) < 2 {
		t.Fatalf("expected the long document to split, got %d chunk(s)", len(chunks))
	}

	tail := lastParagraph(chunks[0].Text)
	if !strings.Contains(chunks[1].Text, tail) {
		t.Error("expected chunk 2 to carry overlap from the tail of chunk 1")
	}
	for i, c := range chunks {
		if c.TokenCount <= 0 {
			t.Errorf("chunk %d has non-positive token count", i)
		}
	}
}

func TestSplit_EmptyContent(t *testing.T) {
	if chunks := ingest.Split("   \n\n  "); len(chunks) != 0 {
		t.Errorf("expected no chunks for blank content, got %d", len(chunks))
	}
}

func lastParagraph(text string) string {
	parts := strings.Split(text, "\n\n")
	return parts[len(parts)-1]
}
