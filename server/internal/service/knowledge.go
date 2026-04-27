package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/hira-vn/cli/server/internal/events"
	"github.com/hira-vn/cli/server/pkg/knowledge"
	"github.com/hira-vn/cli/server/pkg/protocol"
)

// KnowledgeService orchestrates the indexing pipeline (chunk → embed →
// extract → persist) and exposes search. All mutations publish knowledge:*
// events so the frontend cache can stay in sync.
type KnowledgeService struct {
	Store     *knowledge.Store
	Embedder  knowledge.Embedder
	Extractor knowledge.Extractor
	Bus       *events.Bus

	// mu serializes reindex goroutines per doc so duplicate requests merge.
	mu       sync.Mutex
	indexing map[string]bool
}

// NewKnowledgeService constructs a service wired to a pgx pool and external
// model providers.
func NewKnowledgeService(pool *pgxpool.Pool, bus *events.Bus) *KnowledgeService {
	return &KnowledgeService{
		Store:     knowledge.NewStore(pool),
		Embedder:  knowledge.NewEmbedderFromEnv(),
		Extractor: knowledge.NewExtractorFromEnv(),
		Bus:       bus,
		indexing:  make(map[string]bool),
	}
}

// --- Doc CRUD ---

// ListDocs returns docs for a workspace.
func (s *KnowledgeService) ListDocs(ctx context.Context, workspaceID string, status knowledge.DocStatus) ([]knowledge.Doc, error) {
	return s.Store.ListDocs(ctx, knowledge.ListDocsFilter{
		WorkspaceID: workspaceID,
		Status:      status,
	})
}

// GetDoc fetches a single doc.
func (s *KnowledgeService) GetDoc(ctx context.Context, workspaceID, docID string) (knowledge.Doc, error) {
	return s.Store.GetDoc(ctx, workspaceID, docID)
}

// CreateDoc inserts a new doc and kicks off background indexing.
func (s *KnowledgeService) CreateDoc(ctx context.Context, p knowledge.CreateDocParams) (knowledge.Doc, error) {
	if p.Slug == "" {
		p.Slug = slugify(p.Title)
	}
	doc, err := s.Store.CreateDoc(ctx, p)
	if err != nil {
		return knowledge.Doc{}, err
	}
	s.publish(protocol.EventKnowledgeDocCreated, doc.WorkspaceID, docPayload(doc))

	if strings.TrimSpace(doc.Body) != "" {
		s.scheduleIndex(doc)
	}
	return doc, nil
}

// UpdateDoc applies an edit. Any change to body or status=published
// triggers re-indexing.
func (s *KnowledgeService) UpdateDoc(ctx context.Context, workspaceID, docID string, p knowledge.UpdateDocParams) (knowledge.Doc, error) {
	doc, err := s.Store.UpdateDoc(ctx, workspaceID, docID, p)
	if err != nil {
		return knowledge.Doc{}, err
	}
	s.publish(protocol.EventKnowledgeDocUpdated, doc.WorkspaceID, docPayload(doc))

	shouldReindex := p.Body != nil || (p.Status != nil && *p.Status == knowledge.StatusPublished)
	if shouldReindex && strings.TrimSpace(doc.Body) != "" {
		s.scheduleIndex(doc)
	}
	return doc, nil
}

// DeleteDoc removes a doc.
func (s *KnowledgeService) DeleteDoc(ctx context.Context, workspaceID, docID string) error {
	if err := s.Store.DeleteDoc(ctx, workspaceID, docID); err != nil {
		return err
	}
	s.publish(protocol.EventKnowledgeDocDeleted, workspaceID, map[string]any{
		"id":           docID,
		"workspace_id": workspaceID,
	})
	return nil
}

// --- Indexing ---

// Reindex forces re-indexing of an existing doc.
func (s *KnowledgeService) Reindex(ctx context.Context, workspaceID, docID string) error {
	doc, err := s.Store.GetDoc(ctx, workspaceID, docID)
	if err != nil {
		return err
	}
	s.scheduleIndex(doc)
	return nil
}

// scheduleIndex kicks off a background goroutine to re-index a doc. Duplicate
// calls for the same doc are coalesced.
func (s *KnowledgeService) scheduleIndex(doc knowledge.Doc) {
	s.mu.Lock()
	if s.indexing[doc.ID] {
		s.mu.Unlock()
		return
	}
	s.indexing[doc.ID] = true
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.indexing, doc.ID)
			s.mu.Unlock()
		}()
		if err := s.runIndex(context.Background(), doc); err != nil {
			slog.Error("knowledge: index failed", "doc_id", doc.ID, "error", err)
		}
	}()
}

// runIndex is the core pipeline: chunk → embed → extract → upsert entities
// and relations → link mentions.
func (s *KnowledgeService) runIndex(ctx context.Context, doc knowledge.Doc) error {
	// Mark as running and broadcast.
	_ = s.Store.SetDocIndexingStatus(ctx, doc.ID, knowledge.IndexingRunning, "", nil)
	s.publish(protocol.EventKnowledgeDocIndexing, doc.WorkspaceID, map[string]any{
		"id":           doc.ID,
		"workspace_id": doc.WorkspaceID,
	})

	fail := func(err error) error {
		_ = s.Store.SetDocIndexingStatus(ctx, doc.ID, knowledge.IndexingError, err.Error(), nil)
		s.publish(protocol.EventKnowledgeDocFailed, doc.WorkspaceID, map[string]any{
			"id":           doc.ID,
			"workspace_id": doc.WorkspaceID,
			"error":        err.Error(),
		})
		return err
	}

	// 1. Chunk
	chunks := knowledge.ChunkMarkdown(doc.Body)
	if len(chunks) == 0 {
		return fail(fmt.Errorf("no chunks produced from empty body"))
	}

	// 2. Embed
	inputs := make([]string, len(chunks))
	for i, c := range chunks {
		inputs[i] = c.Heading + "\n\n" + c.Content
	}
	embeddings, err := s.Embedder.Embed(ctx, inputs)
	if err != nil {
		return fail(fmt.Errorf("embed chunks: %w", err))
	}

	// 3. Persist chunks (replaces previous set atomically).
	if err := s.Store.ReplaceChunks(ctx, doc.ID, doc.WorkspaceID, chunks, embeddings); err != nil {
		return fail(fmt.Errorf("replace chunks: %w", err))
	}

	// 4. Drop previous relations from this doc (entities are kept and merged).
	_ = s.Store.DeleteRelationsForDoc(ctx, doc.ID)

	// 5. Extract entities + relations.
	ex, err := s.Extractor.Extract(ctx, doc.Title, string(doc.Category), doc.Body)
	if err != nil {
		// Non-fatal: we still have chunks. Record the error but mark ready.
		slog.Warn("knowledge: extract failed (continuing)", "doc_id", doc.ID, "error", err)
	}

	// 6. Upsert entities, compute their embeddings.
	entityIDs := make(map[string]string) // (kind|name) -> uuid
	entityInputs := make([]string, 0, len(ex.Entities))
	for _, e := range ex.Entities {
		entityInputs = append(entityInputs, string(e.Kind)+": "+e.Name+"\n"+e.Description)
	}
	entEmbeds, _ := s.Embedder.Embed(ctx, entityInputs) // best-effort

	for i, e := range ex.Entities {
		var emb []float32
		if i < len(entEmbeds) {
			emb = entEmbeds[i]
		}
		id, upErr := s.Store.UpsertEntity(ctx, doc.WorkspaceID, e, emb, "extracted")
		if upErr != nil {
			slog.Warn("knowledge: upsert entity failed", "name", e.Name, "error", upErr)
			continue
		}
		entityIDs[entityKey(e.Kind, e.Name)] = id
	}

	// 7. Upsert relations by resolving names to IDs.
	relCount := 0
	for _, r := range ex.Relations {
		srcID := entityIDs[entityKey(r.SourceKind, r.SourceName)]
		tgtID := entityIDs[entityKey(r.TargetKind, r.TargetName)]
		if srcID == "" || tgtID == "" {
			// Try DB lookup in case extractor referenced a pre-existing entity.
			if srcID == "" {
				if id, _ := s.Store.FindEntity(ctx, doc.WorkspaceID, r.SourceKind, r.SourceName); id != "" {
					srcID = id
				}
			}
			if tgtID == "" {
				if id, _ := s.Store.FindEntity(ctx, doc.WorkspaceID, r.TargetKind, r.TargetName); id != "" {
					tgtID = id
				}
			}
		}
		if srcID == "" || tgtID == "" {
			continue
		}
		if err := s.Store.UpsertRelation(ctx, doc.WorkspaceID, srcID, tgtID, r.Relation, doc.ID); err == nil {
			relCount++
		}
	}

	// 8. Chunk ↔ Entity mentions via simple alias matching.
	freshChunks, _ := s.Store.GetChunksForDoc(ctx, doc.ID)
	entities, _ := s.Store.ListEntities(ctx, doc.WorkspaceID, "")
	s.linkMentions(ctx, freshChunks, entities)

	// 9. Mark ready and broadcast.
	stats := knowledge.Stats{
		Chunks:    len(freshChunks),
		Entities:  len(ex.Entities),
		Relations: relCount,
	}
	_ = s.Store.SetDocIndexingStatus(ctx, doc.ID, knowledge.IndexingReady, "", &stats)
	s.publish(protocol.EventKnowledgeDocIndexed, doc.WorkspaceID, map[string]any{
		"id":           doc.ID,
		"workspace_id": doc.WorkspaceID,
		"stats":        stats,
	})
	return nil
}

// linkMentions creates chunk↔entity edges whenever a chunk's content contains
// any of an entity's name or aliases (case-insensitive substring match).
func (s *KnowledgeService) linkMentions(ctx context.Context, chunks []knowledge.Chunk, entities []knowledge.Entity) {
	for _, c := range chunks {
		lc := strings.ToLower(c.Heading + " " + c.Content)
		for _, e := range entities {
			candidates := append([]string{e.Name}, e.Aliases...)
			matched := false
			for _, name := range candidates {
				name = strings.TrimSpace(name)
				if len(name) < 2 {
					continue
				}
				if strings.Contains(lc, strings.ToLower(name)) {
					matched = true
					break
				}
			}
			if matched {
				_ = s.Store.LinkChunkToEntity(ctx, c.ID, e.ID)
			}
		}
	}
}

// --- Entities ---

// ListEntities returns entities for a workspace (optionally filtered by kind).
func (s *KnowledgeService) ListEntities(ctx context.Context, workspaceID string, kind knowledge.EntityKind) ([]knowledge.Entity, error) {
	return s.Store.ListEntities(ctx, workspaceID, kind)
}

// GetEntity fetches a single entity.
func (s *KnowledgeService) GetEntity(ctx context.Context, workspaceID, entityID string) (knowledge.Entity, error) {
	return s.Store.GetEntity(ctx, workspaceID, entityID)
}

// MergeEntities folds `dropID` into `keepID` (see Store.MergeEntities).
// Publishes a knowledge:entity_merged event so other clients invalidate
// their graph + entity-list caches.
func (s *KnowledgeService) MergeEntities(ctx context.Context, workspaceID, keepID, dropID string) error {
	if err := s.Store.MergeEntities(ctx, workspaceID, keepID, dropID); err != nil {
		return err
	}
	s.publish(protocol.EventKnowledgeEntityMerged, workspaceID, map[string]any{
		"kept_id":    keepID,
		"dropped_id": dropID,
	})
	return nil
}

// UpdateEntity applies manual edits (UI).
func (s *KnowledgeService) UpdateEntity(ctx context.Context, workspaceID, entityID string, p knowledge.UpdateEntityParams) (knowledge.Entity, error) {
	e, err := s.Store.UpdateEntity(ctx, workspaceID, entityID, p)
	if err != nil {
		return knowledge.Entity{}, err
	}
	s.publish(protocol.EventKnowledgeEntityUpdated, workspaceID, entityPayload(e))
	return e, nil
}

// EntityGraph returns a 1-hop neighbor list for an entity.
func (s *KnowledgeService) EntityGraph(ctx context.Context, workspaceID, entityID string) ([]knowledge.RelatedEntity, error) {
	return s.Store.EntityNeighbors(ctx, workspaceID, entityID)
}

// GraphResult bundles everything needed to render the workspace graph:
// explicit relations plus derived edges (co-mention + sibling) for
// richer visual structure. All derived lists are capped.
type GraphResult struct {
	Nodes         []knowledge.Entity       `json:"nodes"`
	Links         []knowledge.GraphLink    `json:"links"`
	ImplicitLinks []knowledge.ImplicitLink `json:"implicit_links"`
	SiblingLinks  []knowledge.SiblingLink  `json:"sibling_links"`
}

// Graph returns the full workspace graph ready for force-directed
// visualization. Node embeddings are never included. Derived edges are
// computed in parallel to keep p95 small on medium workspaces.
func (s *KnowledgeService) Graph(ctx context.Context, workspaceID string) (GraphResult, error) {
	const derivedCap = 200

	var (
		wg       sync.WaitGroup
		res      GraphResult
		nodesErr error
		linksErr error
		implErr  error
		sibErr   error
	)
	wg.Add(4)
	go func() { defer wg.Done(); res.Nodes, nodesErr = s.Store.ListEntities(ctx, workspaceID, "") }()
	go func() { defer wg.Done(); res.Links, linksErr = s.Store.ListRelations(ctx, workspaceID) }()
	go func() {
		defer wg.Done()
		res.ImplicitLinks, implErr = s.Store.ListImplicitLinks(ctx, workspaceID, derivedCap)
	}()
	go func() {
		defer wg.Done()
		res.SiblingLinks, sibErr = s.Store.ListSiblingLinks(ctx, workspaceID, derivedCap)
	}()
	wg.Wait()

	switch {
	case nodesErr != nil:
		return GraphResult{}, nodesErr
	case linksErr != nil:
		return GraphResult{}, linksErr
	case implErr != nil:
		return GraphResult{}, implErr
	case sibErr != nil:
		return GraphResult{}, sibErr
	}
	return res, nil
}

// --- Search ---

// Search runs hybrid retrieval against the workspace's knowledge graph.
func (s *KnowledgeService) Search(ctx context.Context, workspaceID, query string, limit int) ([]knowledge.Hit, error) {
	return s.Store.Search(ctx, s.Embedder, knowledge.SearchRequest{
		WorkspaceID: workspaceID,
		Query:       query,
		Limit:       limit,
	})
}

// RenderAgentContext wraps Search + render for use from the daemon claim flow.
// Returns an empty string when the workspace has no knowledge content.
func (s *KnowledgeService) RenderAgentContext(ctx context.Context, workspaceID, query string, limit int) string {
	md, _ := s.RenderAgentContextWithHits(ctx, workspaceID, query, limit)
	return md
}

// RenderAgentContextWithHits is like RenderAgentContext but also returns the
// underlying hits so callers can persist citations. The rendered string is
// empty when there are no hits — avoid writing an empty file.
func (s *KnowledgeService) RenderAgentContextWithHits(ctx context.Context, workspaceID, query string, limit int) (string, []knowledge.Hit) {
	hits, err := s.Search(ctx, workspaceID, query, limit)
	if err != nil || len(hits) == 0 {
		return "", nil
	}
	return knowledge.RenderAgentContext(hits, 8192), hits
}

// ClaimContext is the structured result of assembling the knowledge block
// that gets injected into .agent_context/knowledge_context.md at claim time.
// Callers can inspect HasDocs / HitCount for logging without re-parsing the
// markdown string.
type ClaimContext struct {
	Markdown string
	Hits     []knowledge.Hit
	DocCount int
	HitCount int
	HasDocs  bool
}

// RenderClaimContext builds the full knowledge payload for a task claim. It
// always includes a workspace digest (list of published docs + entity count)
// when any published doc exists, even if the query returned zero hits — so
// the agent always learns the graph exists. When the query yields hits, the
// rendered top-K chunks are appended after the digest.
//
// When the workspace has zero published docs, Markdown is empty and
// HasDocs=false, so callers can skip writing the file.
func (s *KnowledgeService) RenderClaimContext(ctx context.Context, workspaceID, query string, limit int) ClaimContext {
	out := ClaimContext{}
	if workspaceID == "" {
		return out
	}

	docs, err := s.Store.ListDocs(ctx, knowledge.ListDocsFilter{
		WorkspaceID: workspaceID,
		Status:      knowledge.StatusPublished,
	})
	if err != nil || len(docs) == 0 {
		return out
	}
	out.HasDocs = true
	out.DocCount = len(docs)

	entities, _ := s.Store.ListEntities(ctx, workspaceID, "")

	digest := knowledge.RenderDigest(toDigestDocs(docs), len(entities))

	var hitsMD string
	if strings.TrimSpace(query) != "" {
		hits, _ := s.Search(ctx, workspaceID, query, limit)
		out.Hits = hits
		out.HitCount = len(hits)
		if len(hits) > 0 {
			hitsMD = knowledge.RenderAgentContext(hits, 8192)
		}
	}

	var b strings.Builder
	b.WriteString(digest)
	if hitsMD != "" {
		b.WriteString("\n---\n\n")
		b.WriteString(hitsMD)
	} else {
		b.WriteString("## Pre-loaded Chunks\n\n")
		b.WriteString("_No direct matches for this task's title/description. Use the directory above and `search_knowledge` (if available) to pull what you need._\n\n")
	}
	out.Markdown = b.String()
	return out
}

// toDigestDocs projects full Doc rows to the compact shape needed by
// knowledge.RenderDigest.
func toDigestDocs(docs []knowledge.Doc) []knowledge.DigestDoc {
	out := make([]knowledge.DigestDoc, 0, len(docs))
	for _, d := range docs {
		out = append(out, knowledge.DigestDoc{
			Title:    d.Title,
			Slug:     d.Slug,
			Category: d.Category,
		})
	}
	return out
}

// RecordCitations stores which chunks were injected for a given task so the
// UI can later show "agent đã tham khảo tri thức gì". Best-effort — errors
// are logged but do not fail the claim.
func (s *KnowledgeService) RecordCitations(ctx context.Context, taskID, workspaceID string, hits []knowledge.Hit) {
	if len(hits) == 0 || taskID == "" {
		return
	}
	if err := s.Store.InsertCitations(ctx, taskID, workspaceID, hits); err != nil {
		slog.Warn("knowledge: record citations failed", "task_id", taskID, "error", err)
	}
}

// ListCitationsForTask returns the citations recorded for a task, enriched
// with doc + chunk metadata.
func (s *KnowledgeService) ListCitationsForTask(ctx context.Context, taskID string) ([]knowledge.Citation, error) {
	return s.Store.ListCitationsForTask(ctx, taskID)
}

// --- helpers ---

func (s *KnowledgeService) publish(eventType, workspaceID string, payload any) {
	if s == nil || s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

func docPayload(d knowledge.Doc) map[string]any {
	return map[string]any{
		"id":              d.ID,
		"workspace_id":    d.WorkspaceID,
		"title":           d.Title,
		"slug":            d.Slug,
		"category":        d.Category,
		"status":          d.Status,
		"indexing_status": d.IndexingStatus,
	}
}

func entityPayload(e knowledge.Entity) map[string]any {
	return map[string]any{
		"id":           e.ID,
		"workspace_id": e.WorkspaceID,
		"kind":         e.Kind,
		"name":         e.Name,
	}
}

func entityKey(kind knowledge.EntityKind, name string) string {
	return string(kind) + "|" + strings.ToLower(strings.TrimSpace(name))
}

// slugify produces a URL-safe, stable slug from a title.
func slugify(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	prevDash := false
	for _, r := range title {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "untitled"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// statsJSON is exposed so handlers can pretty-print if needed.
func (s *KnowledgeService) statsJSON(d knowledge.Doc) string {
	b, _ := json.Marshal(d.Stats)
	return string(b)
}
