// Package ingest turns uploaded runbooks into embedded, searchable chunks.
package ingest

import "strings"

const (
	// targetTokens is the chunk size the retrieval pipeline is tuned for.
	targetTokens = 500

	// overlapTokens carries context across a boundary so a diagnostic step split
	// mid-procedure still retrieves with the paragraph that introduced it.
	overlapTokens = targetTokens / 10

	// charsPerToken approximates the tokenizer well enough for sizing decisions.
	// Chunk sizes are a retrieval tuning knob, not a hard API limit.
	charsPerToken = 4
)

// Chunk is a segment of a runbook awaiting embedding.
type Chunk struct {
	Text       string
	TokenCount int
}

// Split segments markdown into overlapping chunks, preferring paragraph boundaries so
// runbook procedures and fenced code blocks stay intact.
func Split(content string) []Chunk {
	paragraphs := splitParagraphs(content)
	if len(paragraphs) == 0 {
		return nil
	}

	var (
		chunks  []Chunk
		current []string
		size    int
	)
	for _, para := range paragraphs {
		paraSize := EstimateTokens(para)
		if size > 0 && size+paraSize > targetTokens {
			chunks = append(chunks, newChunk(current))
			current, size = carryOverlap(current)
		}
		current = append(current, para)
		size += paraSize
	}
	if len(current) > 0 {
		chunks = append(chunks, newChunk(current))
	}
	return chunks
}

// EstimateTokens approximates the token length of a string.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return max(1, len(text)/charsPerToken)
}

// newChunk joins accumulated paragraphs into a single chunk.
func newChunk(paragraphs []string) Chunk {
	text := strings.Join(paragraphs, "\n\n")
	return Chunk{Text: text, TokenCount: EstimateTokens(text)}
}

// carryOverlap seeds the next chunk with the tail of the previous one, so that a
// boundary never severs a paragraph from the step that set it up.
func carryOverlap(paragraphs []string) ([]string, int) {
	var (
		carried []string
		size    int
	)
	for i := len(paragraphs) - 1; i >= 0; i-- {
		paraSize := EstimateTokens(paragraphs[i])
		if size+paraSize > overlapTokens && len(carried) > 0 {
			break
		}
		carried = append([]string{paragraphs[i]}, carried...)
		size += paraSize
	}
	return carried, size
}

// splitParagraphs breaks content on blank lines, keeping fenced code blocks whole.
func splitParagraphs(content string) []string {
	var (
		paragraphs []string
		current    []string
		inFence    bool
	)
	flush := func() {
		if joined := strings.TrimSpace(strings.Join(current, "\n")); joined != "" {
			paragraphs = append(paragraphs, joined)
		}
		current = nil
	}

	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		current = append(current, line)
	}
	flush()
	return paragraphs
}
