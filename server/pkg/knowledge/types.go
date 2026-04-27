// Package knowledge implements the business knowledge graph: markdown docs
// plus an extracted graph of typed entities (teams, policies, processes...)
// and their relations. Semantic search powers context injection for agents.
package knowledge

import "time"

// DocCategory enumerates the high-level kinds of docs users can write.
type DocCategory string

const (
	CategorySOP      DocCategory = "sop"
	CategoryBrand    DocCategory = "brand"
	CategoryPolicy   DocCategory = "policy"
	CategoryOrg      DocCategory = "org"
	CategoryProcess  DocCategory = "process"
	CategoryGlossary DocCategory = "glossary"
	CategoryGeneral  DocCategory = "general"
)

// DocStatus is the publication state — only `published` docs feed agent context.
type DocStatus string

const (
	StatusDraft     DocStatus = "draft"
	StatusPublished DocStatus = "published"
	StatusArchived  DocStatus = "archived"
)

// IndexingStatus tracks the extraction pipeline state.
type IndexingStatus string

const (
	IndexingIdle    IndexingStatus = "idle"
	IndexingPending IndexingStatus = "pending"
	IndexingRunning IndexingStatus = "indexing"
	IndexingReady   IndexingStatus = "ready"
	IndexingError   IndexingStatus = "error"
)

// EntityKind enumerates supported node types in the graph.
type EntityKind string

const (
	EntityTeam            EntityKind = "team"
	EntityPerson          EntityKind = "person"
	EntityProcess         EntityKind = "process"
	EntityPolicy          EntityKind = "policy"
	EntityBrandAsset      EntityKind = "brand_asset"
	EntityProduct         EntityKind = "product"
	EntitySystem          EntityKind = "system"
	EntityMetric          EntityKind = "metric"
	EntityCustomerSegment EntityKind = "customer_segment"
)

// RelationKind enumerates supported edge types.
type RelationKind string

const (
	RelOwns           RelationKind = "owns"
	RelResponsibleFor RelationKind = "responsible_for"
	RelPartOf         RelationKind = "part_of"
	RelReferences     RelationKind = "references"
	RelGovernedBy     RelationKind = "governed_by"
	RelSupersedes     RelationKind = "supersedes"
	RelUses           RelationKind = "uses"
	RelReportsTo      RelationKind = "reports_to"
)

// Stats is a JSON-serialized summary persisted on each doc after indexing.
type Stats struct {
	Chunks    int `json:"chunks"`
	Entities  int `json:"entities"`
	Relations int `json:"relations"`
	Tokens    int `json:"tokens"`
}

// Doc is a markdown knowledge page.
type Doc struct {
	ID             string         `json:"id"`
	WorkspaceID    string         `json:"workspace_id"`
	Title          string         `json:"title"`
	Slug           string         `json:"slug"`
	Category       DocCategory    `json:"category"`
	Body           string         `json:"body"`
	Tags           []string       `json:"tags"`
	AuthorID       *string        `json:"author_id,omitempty"`
	Status         DocStatus      `json:"status"`
	IndexingStatus IndexingStatus `json:"indexing_status"`
	IndexingError  *string        `json:"indexing_error,omitempty"`
	Stats          Stats          `json:"stats"`
	ExtractedAt    *time.Time     `json:"extracted_at,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// Entity is a node in the knowledge graph.
type Entity struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	Kind        EntityKind     `json:"kind"`
	Name        string         `json:"name"`
	Aliases     []string       `json:"aliases"`
	Description string         `json:"description"`
	Attributes  map[string]any `json:"attributes"`
	Source      string         `json:"source"` // "extracted" | "manual"
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

// Relation is a typed edge between two entities.
type Relation struct {
	ID            string         `json:"id"`
	SourceID      string         `json:"source_id"`
	TargetID      string         `json:"target_id"`
	Relation      RelationKind   `json:"relation"`
	EvidenceDocID *string        `json:"evidence_doc_id,omitempty"`
	Metadata      map[string]any `json:"metadata"`
}

// Chunk is a retrieval unit inside a doc (typically a heading section).
type Chunk struct {
	ID      string `json:"id"`
	DocID   string `json:"doc_id"`
	Ordinal int    `json:"ordinal"`
	Heading string `json:"heading"`
	Content string `json:"content"`
}

// Hit is a semantic search result.
type Hit struct {
	ChunkID         string          `json:"chunk_id"`
	DocID           string          `json:"doc_id"`
	DocTitle        string          `json:"doc_title"`
	DocCategory     DocCategory     `json:"doc_category"`
	Heading         string          `json:"heading"`
	Content         string          `json:"content"`
	Score           float64         `json:"score"`
	RelatedEntities []RelatedEntity `json:"related_entities"`
}

// RelatedEntity is a neighbor surfaced via graph expansion from a hit's mentions.
type RelatedEntity struct {
	ID          string     `json:"id"`
	Kind        EntityKind `json:"kind"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Relation    string     `json:"relation,omitempty"` // relation from mentioned entity
}
