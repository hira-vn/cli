package knowledge

import (
	"context"
	"testing"
)

// TestRuleExtractor_PromotesDocTitle mirrors a common user scenario: a doc
// called "Brand" with minimal body like `## Guideline\nmarketing phu trach`
// should produce at least one entity (the doc itself) plus one for the
// heading so the Entities tab never looks empty.
func TestRuleExtractor_PromotesDocTitle(t *testing.T) {
	ex := &ruleExtractor{}
	got, err := ex.Extract(context.Background(), "Brand", "brand",
		"## Guideline\nmarketing phu trach")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Entities) < 2 {
		t.Fatalf("expected >=2 entities, got %d: %+v", len(got.Entities), got.Entities)
	}

	var hasTitle, hasHeading bool
	for _, e := range got.Entities {
		switch e.Name {
		case "Brand":
			hasTitle = true
			if e.Kind != EntityBrandAsset {
				t.Errorf("title entity should be brand_asset, got %s", e.Kind)
			}
		case "Guideline":
			hasHeading = true
			if e.Kind != EntityBrandAsset {
				t.Errorf("heading entity should be brand_asset, got %s", e.Kind)
			}
		}
	}
	if !hasTitle {
		t.Errorf("expected title entity 'Brand'")
	}
	if !hasHeading {
		t.Errorf("expected heading entity 'Guideline'")
	}
}

func TestRuleExtractor_SOPCategory(t *testing.T) {
	ex := &ruleExtractor{}
	got, _ := ex.Extract(context.Background(), "Quy trinh hoan tien", "sop",
		"## CSKH phu trach\nBuoc 1: ...")
	if len(got.Entities) < 2 {
		t.Fatalf("expected >=2 entities, got %+v", got.Entities)
	}
	// Both entities should be process (SOP category).
	for _, e := range got.Entities {
		if e.Kind != EntityProcess {
			t.Errorf("SOP entity should be process kind, got %s for %q", e.Kind, e.Name)
		}
	}
}

func TestRuleExtractor_TypedHeadingOverridesCategory(t *testing.T) {
	ex := &ruleExtractor{}
	got, _ := ex.Extract(context.Background(), "Doc", "general",
		"## Team: CS\n...")
	var csFound bool
	for _, e := range got.Entities {
		if e.Name == "CS" {
			csFound = true
			if e.Kind != EntityTeam {
				t.Errorf("typed heading should win over category, got %s", e.Kind)
			}
		}
	}
	if !csFound {
		t.Errorf("expected team entity 'CS' from typed heading")
	}
}
