package knowledge

import (
	"fmt"
	"sort"
	"strings"
)

// DigestDoc is the minimal shape of a published doc needed to render a
// workspace digest: agents see title, category, and slug so they can decide
// which doc to open with the `hira` CLI or MCP `get_doc` tool.
type DigestDoc struct {
	Title    string
	Slug     string
	Category DocCategory
}

// RenderDigest builds a table-of-contents style block for the workspace
// knowledge graph. It is always rendered when any published doc exists — even
// when the claim-time retrieval returns zero hits — so the agent learns the
// KB exists and how to query it, instead of silently missing it.
//
// entityCount is the total number of entities in the workspace (any kind);
// it gives the agent a sense of how dense the graph is. Zero is fine.
func RenderDigest(docs []DigestDoc, entityCount int) string {
	if len(docs) == 0 {
		return ""
	}
	// Group by category so the digest reads like a structured directory.
	grouped := map[DocCategory][]DigestDoc{}
	for _, d := range docs {
		cat := d.Category
		if cat == "" {
			cat = CategoryGeneral
		}
		grouped[cat] = append(grouped[cat], d)
	}
	// Stable category order: known categories first (alphabetical), then
	// any custom category we did not hardcode.
	known := []DocCategory{
		CategoryBrand,
		CategoryGeneral,
		CategoryGlossary,
		CategoryOrg,
		CategoryPolicy,
		CategoryProcess,
		CategorySOP,
	}
	seen := map[DocCategory]bool{}
	var order []DocCategory
	for _, c := range known {
		if _, ok := grouped[c]; ok {
			order = append(order, c)
			seen[c] = true
		}
	}
	for c := range grouped {
		if !seen[c] {
			order = append(order, c)
		}
	}
	var b strings.Builder
	b.WriteString("# Knowledge Digest\n\n")
	fmt.Fprintf(&b, "This workspace has **%d published doc(s)** and **%d entity(ies)** in its knowledge graph.\n\n", len(docs), entityCount)
	b.WriteString("Use this directory to pick the right doc for your task. Pre-loaded chunks (if any) follow below.\n\n")

	for _, cat := range order {
		items := grouped[cat]
		sort.SliceStable(items, func(i, j int) bool { return items[i].Title < items[j].Title })
		fmt.Fprintf(&b, "## %s\n\n", capitalizeCategory(string(cat)))
		for _, d := range items {
			fmt.Fprintf(&b, "- **%s** — slug `%s`\n", d.Title, d.Slug)
		}
		b.WriteString("\n")
	}

	b.WriteString("## How to use\n\n")
	b.WriteString("- Open a doc: `hira knowledge doc get <slug>` (when available) or ask a human teammate.\n")
	b.WriteString("- Drill down mid-task: call the MCP tool `search_knowledge` if present in your tool list.\n")
	b.WriteString("- Do NOT guess when a doc likely covers the topic — prefer reading the doc over improvising.\n\n")

	return b.String()
}

// capitalizeCategory upper-cases the first rune of a category label for
// display. Kept local so the package does not pull in golang.org/x/text for
// something this trivial.
func capitalizeCategory(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// RenderAgentContext builds the markdown that gets written to
// .agent_context/knowledge_context.md for an agent task. Returns an empty
// string when there are no hits, so callers can skip file creation.
//
// totalCap enforces a soft ceiling on bytes; chunks are dropped once the cap
// is exceeded so the agent's token budget stays sane.
func RenderAgentContext(hits []Hit, totalCap int) string {
	if len(hits) == 0 {
		return ""
	}
	if totalCap <= 0 {
		totalCap = 8192
	}
	var b strings.Builder
	b.WriteString("# Knowledge Context\n\n")
	b.WriteString("Most relevant business knowledge for this task, retrieved from the workspace knowledge graph.\n\n")

	for i, h := range hits {
		if b.Len() >= totalCap {
			break
		}
		heading := h.Heading
		if heading == "" {
			heading = "(intro)"
		}
		fmt.Fprintf(&b, "## %d. %s › %s  _(%s)_\n\n", i+1, h.DocTitle, heading, h.DocCategory)

		content := strings.TrimSpace(h.Content)
		if remaining := totalCap - b.Len(); remaining > 0 && len(content) > remaining {
			content = content[:remaining] + "..."
		}
		b.WriteString(content)
		b.WriteString("\n\n")

		if len(h.RelatedEntities) > 0 {
			b.WriteString("**Related:** ")
			for j, re := range h.RelatedEntities {
				if j > 0 {
					b.WriteString("; ")
				}
				fmt.Fprintf(&b, "%s `%s`", re.Kind, re.Name)
				if re.Description != "" {
					fmt.Fprintf(&b, " — %s", re.Description)
				}
			}
			b.WriteString("\n\n")
		}
		b.WriteString("---\n\n")
	}
	return b.String()
}
