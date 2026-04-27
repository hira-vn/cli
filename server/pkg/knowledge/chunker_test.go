package knowledge

import (
	"strings"
	"testing"
)

func TestChunk_SimpleHeadings(t *testing.T) {
	body := `# Intro

Hello world

## Team: CS

CS team owns refunds.

## Policy: Refund v2

30-day return window.`
	got := ChunkMarkdown(body)
	if len(got) != 3 {
		t.Fatalf("expected 3 chunks, got %d: %+v", len(got), got)
	}
	if got[0].Heading != "Intro" {
		t.Errorf("chunk[0] heading = %q", got[0].Heading)
	}
	if !strings.Contains(got[1].Content, "CS team owns refunds") {
		t.Errorf("chunk[1] content missing: %q", got[1].Content)
	}
	if got[2].Heading != "Policy: Refund v2" {
		t.Errorf("chunk[2] heading = %q", got[2].Heading)
	}
	for i, c := range got {
		if c.Ordinal != i {
			t.Errorf("chunk[%d] ordinal = %d", i, c.Ordinal)
		}
	}
}

func TestChunk_NoHeading(t *testing.T) {
	body := "Just a plain paragraph with no headings at all."
	got := ChunkMarkdown(body)
	if len(got) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(got))
	}
	if got[0].Heading != "" {
		t.Errorf("heading should be empty, got %q", got[0].Heading)
	}
	if got[0].Content != body {
		t.Errorf("content mismatch: %q", got[0].Content)
	}
}

func TestChunk_Empty(t *testing.T) {
	if got := ChunkMarkdown(""); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
	if got := ChunkMarkdown("   \n\n  "); got != nil {
		t.Errorf("expected nil for whitespace, got %+v", got)
	}
}

func TestChunk_OversizedSection(t *testing.T) {
	// Force a single section larger than maxChunkChars.
	longPara := strings.Repeat("Sentence. ", 300) // ~3000 chars
	body := "## Big\n\n" + longPara
	got := ChunkMarkdown(body)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks from oversized section, got %d", len(got))
	}
	for _, c := range got {
		if len(c.Content) > maxChunkChars {
			t.Errorf("chunk exceeds max: len=%d", len(c.Content))
		}
	}
}
