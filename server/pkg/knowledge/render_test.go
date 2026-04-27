package knowledge

import (
	"strings"
	"testing"
)

func TestRenderAgentContext_Empty(t *testing.T) {
	if s := RenderAgentContext(nil, 1024); s != "" {
		t.Errorf("expected empty, got %q", s)
	}
}

func TestRenderAgentContext_IncludesRelatedEntities(t *testing.T) {
	hits := []Hit{{
		DocTitle:    "Customer SOP",
		DocCategory: CategorySOP,
		Heading:     "Refunds",
		Content:     "30-day return window.",
		RelatedEntities: []RelatedEntity{
			{Kind: EntityTeam, Name: "CS", Description: "Customer Service team"},
		},
	}}
	out := RenderAgentContext(hits, 4096)
	if !strings.Contains(out, "Customer SOP") {
		t.Errorf("missing doc title: %q", out)
	}
	if !strings.Contains(out, "30-day return window") {
		t.Errorf("missing content: %q", out)
	}
	if !strings.Contains(out, "CS") {
		t.Errorf("missing entity name: %q", out)
	}
}

func TestRenderAgentContext_RespectsBudget(t *testing.T) {
	hits := []Hit{
		{DocTitle: "A", Heading: "h", Content: strings.Repeat("x", 2000), DocCategory: CategorySOP},
		{DocTitle: "B", Heading: "h", Content: strings.Repeat("y", 2000), DocCategory: CategorySOP},
	}
	out := RenderAgentContext(hits, 500)
	if len(out) > 2000 {
		t.Errorf("output exceeds cap wildly: len=%d", len(out))
	}
}

func TestRenderDigest_EmptyReturnsEmpty(t *testing.T) {
	if got := RenderDigest(nil, 0); got != "" {
		t.Errorf("expected empty digest for no docs, got %q", got)
	}
}

func TestRenderDigest_GroupsByCategory(t *testing.T) {
	docs := []DigestDoc{
		{Title: "Brand Guideline", Slug: "brand-guideline", Category: CategoryBrand},
		{Title: "Refund Policy", Slug: "refund", Category: CategoryPolicy},
		{Title: "Onboarding SOP", Slug: "onboarding", Category: CategorySOP},
		{Title: "Team Roster", Slug: "team", Category: CategoryOrg},
	}
	out := RenderDigest(docs, 12)
	for _, want := range []string{
		"Knowledge Digest",
		"4 published doc",
		"12 entity",
		"Brand",
		"Brand Guideline",
		"Policy",
		"Refund Policy",
		"Sop",
		"Onboarding SOP",
		"Org",
		"search_knowledge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("digest missing %q:\n%s", want, out)
		}
	}
}

func TestRenderDigest_DefaultsEmptyCategoryToGeneral(t *testing.T) {
	docs := []DigestDoc{{Title: "Misc Doc", Slug: "misc"}}
	out := RenderDigest(docs, 0)
	if !strings.Contains(out, "General") {
		t.Errorf("expected fallback category 'General' in output:\n%s", out)
	}
}
