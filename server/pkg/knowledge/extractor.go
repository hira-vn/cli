package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// ExtractedEntity is the raw output the extractor produces before it gets
// upserted into the DB (no IDs yet).
type ExtractedEntity struct {
	Kind        EntityKind     `json:"kind"`
	Name        string         `json:"name"`
	Aliases     []string       `json:"aliases,omitempty"`
	Description string         `json:"description,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

// ExtractedRelation references entities by (kind, name) pairs because the
// extractor doesn't know the final UUIDs yet.
type ExtractedRelation struct {
	SourceKind EntityKind   `json:"source_kind"`
	SourceName string       `json:"source_name"`
	Relation   RelationKind `json:"relation"`
	TargetKind EntityKind   `json:"target_kind"`
	TargetName string       `json:"target_name"`
}

// Extraction is the full result for one doc.
type Extraction struct {
	Entities  []ExtractedEntity   `json:"entities"`
	Relations []ExtractedRelation `json:"relations"`
}

// Extractor pulls entities/relations out of a markdown doc.
type Extractor interface {
	Extract(ctx context.Context, title, category, body string) (Extraction, error)
}

// NewExtractorFromEnv returns an Anthropic-backed extractor when
// ANTHROPIC_API_KEY is present, otherwise a deterministic rule-based fallback
// so the pipeline still produces useful output without an LLM.
func NewExtractorFromEnv() Extractor {
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		return &anthropicExtractor{
			apiKey:     key,
			baseURL:    envOr("ANTHROPIC_BASE_URL", "https://api.anthropic.com/v1"),
			model:      envOr("ANTHROPIC_EXTRACTION_MODEL", "claude-haiku-4-5-20251001"),
			httpClient: &http.Client{Timeout: 60 * time.Second},
			fallback:   &ruleExtractor{},
		}
	}
	return &ruleExtractor{}
}

// anthropicExtractor asks Claude Haiku for a structured JSON extraction.
type anthropicExtractor struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
	fallback   Extractor
}

type anthropicMessageRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    string                 `json:"system"`
	Messages  []anthropicMessagePart `json:"messages"`
}

type anthropicMessagePart struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessageResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

const extractSystemPrompt = `You extract a business knowledge graph from markdown documents.

Return STRICT JSON matching this schema and nothing else:
{
  "entities": [
    {
      "kind": "team|person|process|policy|brand_asset|product|system|metric|customer_segment",
      "name": "canonical display name",
      "aliases": ["optional", "variants"],
      "description": "one-sentence summary",
      "attributes": { "free-form": "kv" }
    }
  ],
  "relations": [
    {
      "source_kind": "...", "source_name": "...",
      "relation": "owns|responsible_for|part_of|references|governed_by|supersedes|uses|reports_to",
      "target_kind": "...", "target_name": "..."
    }
  ]
}

Rules:
- Prefer high-signal entities (teams, policies, owned processes, named products).
- Use the language of the doc for names (Vietnamese if the doc is Vietnamese).
- Omit "attributes" unless you have a concrete attribute to record.
- Never invent IDs. Refer to entities only by (kind, name).
- Output JSON ONLY — no prose, no code fences.`

func (e *anthropicExtractor) Extract(ctx context.Context, title, category, body string) (Extraction, error) {
	if strings.TrimSpace(body) == "" {
		return Extraction{}, nil
	}
	userMsg := fmt.Sprintf("Title: %s\nCategory: %s\n\n%s", title, category, body)

	reqBody, err := json.Marshal(anthropicMessageRequest{
		Model:     e.model,
		MaxTokens: 2048,
		System:    extractSystemPrompt,
		Messages: []anthropicMessagePart{{
			Role:    "user",
			Content: userMsg,
		}},
	})
	if err != nil {
		return Extraction{}, fmt.Errorf("marshal extract request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/messages", bytes.NewReader(reqBody))
	if err != nil {
		return Extraction{}, fmt.Errorf("build extract request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", e.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		// Soft-fail to the rule-based extractor to keep indexing resilient.
		if e.fallback != nil {
			return e.fallback.Extract(ctx, title, category, body)
		}
		return Extraction{}, fmt.Errorf("anthropic extract: %w", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if e.fallback != nil {
			return e.fallback.Extract(ctx, title, category, body)
		}
		return Extraction{}, fmt.Errorf("anthropic extract http %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed anthropicMessageResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return Extraction{}, fmt.Errorf("parse extract response: %w", err)
	}
	if parsed.Error != nil {
		return Extraction{}, fmt.Errorf("anthropic extract: %s", parsed.Error.Message)
	}

	var text strings.Builder
	for _, c := range parsed.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	ex, err := parseExtractionJSON(text.String())
	if err != nil && e.fallback != nil {
		return e.fallback.Extract(ctx, title, category, body)
	}
	return ex, err
}

// parseExtractionJSON is defensive: strips Markdown fences if the model
// wraps its response in ```json ...```.
func parseExtractionJSON(s string) (Extraction, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove opening fence line.
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	var ex Extraction
	if err := json.Unmarshal([]byte(s), &ex); err != nil {
		return Extraction{}, fmt.Errorf("parse extraction: %w", err)
	}
	return ex, nil
}

// ruleExtractor pulls entities out with simple markdown heuristics so the
// pipeline produces useful output even without an LLM. It recognizes:
//   - Explicit typed headings: `## Team: Foo`, `## Policy: Bar`
//   - Owner/responsible lines: `- **Owner**: Alice`
//   - Any H2/H3 heading — mapped to an entity kind derived from the doc's
//     category (e.g. SOP doc → "process", brand doc → "brand_asset").
//   - The doc title itself — always promoted to a top-level entity so even
//     minimal docs produce at least one node.
type ruleExtractor struct{}

var (
	frontMatterRe = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\s*\n`)
	kindHeadingRe = regexp.MustCompile(`(?mi)^#{1,3}\s+(team|person|process|policy|brand asset|product|system|metric|customer segment):\s*(.+?)\s*$`)
	ownerLineRe   = regexp.MustCompile(`(?mi)^[-*]\s*\*\*(owner|responsible|lead|manager|chu tri|phu trach)\*\*\s*[:\-]\s*(.+?)\s*$`)
	anyHeadingRe  = regexp.MustCompile(`(?m)^(#{1,3})\s+(.+?)\s*$`)
)

// categoryToEntityKind maps a doc category to the EntityKind used when
// promoting plain headings. Falls back to EntityProcess for anything unknown.
func categoryToEntityKind(category string) EntityKind {
	switch strings.ToLower(category) {
	case "brand":
		return EntityBrandAsset
	case "policy":
		return EntityPolicy
	case "org":
		return EntityTeam
	case "glossary":
		return EntityMetric
	case "product":
		return EntityProduct
	case "process", "sop":
		return EntityProcess
	default:
		return EntityProcess
	}
}

func (r *ruleExtractor) Extract(_ context.Context, title, category string, body string) (Extraction, error) {
	ex := Extraction{}
	if strings.TrimSpace(body) == "" && strings.TrimSpace(title) == "" {
		return ex, nil
	}

	seen := make(map[string]bool) // key = kind|lower(name)
	add := func(e ExtractedEntity) {
		e.Name = strings.TrimSpace(e.Name)
		if len(e.Name) < 2 {
			return
		}
		if e.Kind == "" {
			e.Kind = categoryToEntityKind(category)
		}
		key := string(e.Kind) + "|" + strings.ToLower(e.Name)
		if seen[key] {
			return
		}
		seen[key] = true
		ex.Entities = append(ex.Entities, e)
	}

	// Promote the doc title itself to a top-level entity so even minimal docs
	// show up in the entities browser.
	if titleClean := strings.TrimSpace(title); titleClean != "" {
		add(ExtractedEntity{
			Kind:        categoryToEntityKind(category),
			Name:        titleClean,
			Description: fmt.Sprintf("Chủ đề chính của tài liệu %q", titleClean),
		})
	}

	// Typed headings — highest signal.
	for _, m := range kindHeadingRe.FindAllStringSubmatch(body, -1) {
		kindStr := strings.ToLower(strings.ReplaceAll(m[1], " ", "_"))
		name := m[2]
		add(ExtractedEntity{
			Kind:        EntityKind(kindStr),
			Name:        name,
			Description: fmt.Sprintf("Trích từ %q", title),
		})
		if title != "" {
			ex.Relations = append(ex.Relations, ExtractedRelation{
				SourceKind: EntityKind(kindStr), SourceName: name,
				Relation:   RelReferences,
				TargetKind: categoryToEntityKind(category), TargetName: title,
			})
		}
	}

	// Plain H2/H3 headings — map to the doc's category kind.
	fallbackKind := categoryToEntityKind(category)
	for _, m := range anyHeadingRe.FindAllStringSubmatch(body, -1) {
		hashes := m[1]
		name := strings.TrimSpace(m[2])
		if len(hashes) > 3 || name == "" {
			continue
		}
		// Skip "Team: X" style — already handled above.
		if kindHeadingRe.MatchString(hashes + " " + name) {
			continue
		}
		add(ExtractedEntity{
			Kind:        fallbackKind,
			Name:        name,
			Description: fmt.Sprintf("Mục trong tài liệu %q", title),
		})
		if title != "" && name != title {
			ex.Relations = append(ex.Relations, ExtractedRelation{
				SourceKind: fallbackKind, SourceName: name,
				Relation:   RelPartOf,
				TargetKind: categoryToEntityKind(category), TargetName: title,
			})
		}
	}

	// Owner/responsible lines → person entity.
	for _, m := range ownerLineRe.FindAllStringSubmatch(body, -1) {
		person := strings.TrimSpace(m[2])
		role := strings.ToLower(m[1])
		add(ExtractedEntity{
			Kind:        EntityPerson,
			Name:        person,
			Description: fmt.Sprintf("%s của %q", role, title),
		})
		if title != "" {
			ex.Relations = append(ex.Relations, ExtractedRelation{
				SourceKind: EntityPerson, SourceName: person,
				Relation:   RelResponsibleFor,
				TargetKind: categoryToEntityKind(category), TargetName: title,
			})
		}
	}

	return ex, nil
}
