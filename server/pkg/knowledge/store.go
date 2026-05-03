package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps raw pgx queries for the knowledge graph. sqlc does not have a
// native pgvector type mapping, so we keep these queries outside of sqlc and
// work with []float32 directly.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore wraps a pgx pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// ErrNotFound is returned when a lookup misses.
var ErrNotFound = errors.New("knowledge: not found")

// --- Docs ---

// ListDocsFilter controls ListDocs scoping.
type ListDocsFilter struct {
	WorkspaceID string
	Status      DocStatus // empty = any
	Category    DocCategory
}

// ListDocs returns docs in a workspace.
func (s *Store) ListDocs(ctx context.Context, f ListDocsFilter) ([]Doc, error) {
	sb := strings.Builder{}
	sb.WriteString(`SELECT id, workspace_id, title, slug, category, body, tags,
		author_id, status, indexing_status, indexing_error, stats, extracted_at,
		created_at, updated_at
		FROM knowledge_doc WHERE workspace_id = $1`)
	args := []any{f.WorkspaceID}
	if f.Status != "" {
		sb.WriteString(" AND status = $" + strconv.Itoa(len(args)+1))
		args = append(args, string(f.Status))
	}
	if f.Category != "" {
		sb.WriteString(" AND category = $" + strconv.Itoa(len(args)+1))
		args = append(args, string(f.Category))
	}
	sb.WriteString(" ORDER BY updated_at DESC")

	rows, err := s.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Doc
	for rows.Next() {
		d, err := scanDoc(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDoc fetches a single doc by ID (scoped to workspace).
func (s *Store) GetDoc(ctx context.Context, workspaceID, docID string) (Doc, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, workspace_id, title, slug, category, body, tags,
		author_id, status, indexing_status, indexing_error, stats, extracted_at,
		created_at, updated_at
		FROM knowledge_doc WHERE id = $1 AND workspace_id = $2`, docID, workspaceID)
	d, err := scanDoc(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Doc{}, ErrNotFound
	}
	return d, err
}

// CreateDocParams is the input for Store.CreateDoc.
type CreateDocParams struct {
	WorkspaceID string
	Title       string
	Slug        string
	Category    DocCategory
	Body        string
	Tags        []string
	AuthorID    string
	Status      DocStatus
}

// CreateDoc inserts a new doc.
func (s *Store) CreateDoc(ctx context.Context, p CreateDocParams) (Doc, error) {
	if p.Category == "" {
		p.Category = CategoryGeneral
	}
	if p.Status == "" {
		p.Status = StatusDraft
	}
	// Postgres text[] NOT NULL DEFAULT '{}' — a nil slice binds as SQL NULL
	// through pgx and triggers a constraint violation, so normalize here.
	if p.Tags == nil {
		p.Tags = []string{}
	}
	var authorID any
	if p.AuthorID != "" {
		authorID = p.AuthorID
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO knowledge_doc
		(workspace_id, title, slug, category, body, tags, author_id, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, workspace_id, title, slug, category, body, tags,
		  author_id, status, indexing_status, indexing_error, stats, extracted_at,
		  created_at, updated_at`,
		p.WorkspaceID, p.Title, p.Slug, string(p.Category), p.Body,
		p.Tags, authorID, string(p.Status))
	return scanDoc(row)
}

// UpdateDocParams carries optional field updates.
type UpdateDocParams struct {
	Title    *string
	Category *DocCategory
	Body     *string
	Tags     *[]string
	Status   *DocStatus
}

// UpdateDoc applies a partial update and returns the new row.
func (s *Store) UpdateDoc(ctx context.Context, workspaceID, docID string, p UpdateDocParams) (Doc, error) {
	setParts := []string{"updated_at = now()"}
	args := []any{docID, workspaceID}
	add := func(col string, v any) {
		args = append(args, v)
		setParts = append(setParts, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Title != nil {
		add("title", *p.Title)
	}
	if p.Category != nil {
		add("category", string(*p.Category))
	}
	if p.Body != nil {
		add("body", *p.Body)
	}
	if p.Tags != nil {
		tags := *p.Tags
		if tags == nil {
			tags = []string{}
		}
		add("tags", tags)
	}
	if p.Status != nil {
		add("status", string(*p.Status))
	}
	query := fmt.Sprintf(`UPDATE knowledge_doc SET %s
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, title, slug, category, body, tags,
		  author_id, status, indexing_status, indexing_error, stats, extracted_at,
		  created_at, updated_at`, strings.Join(setParts, ", "))
	row := s.pool.QueryRow(ctx, query, args...)
	d, err := scanDoc(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Doc{}, ErrNotFound
	}
	return d, err
}

// SetDocIndexingStatus updates the indexing status fields.
func (s *Store) SetDocIndexingStatus(ctx context.Context, docID string, status IndexingStatus, errMsg string, stats *Stats) error {
	var statsJSON []byte
	if stats != nil {
		b, err := json.Marshal(stats)
		if err != nil {
			return err
		}
		statsJSON = b
	}
	var extractedAt any
	if status == IndexingReady {
		extractedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `UPDATE knowledge_doc
		SET indexing_status = $2,
		    indexing_error  = NULLIF($3, ''),
		    stats           = COALESCE($4::jsonb, stats),
		    extracted_at    = COALESCE($5::timestamptz, extracted_at),
		    updated_at      = now()
		WHERE id = $1`, docID, string(status), errMsg, statsJSON, extractedAt)
	return err
}

// DeleteDoc removes a doc (cascades to chunks + mentions).
func (s *Store) DeleteDoc(ctx context.Context, workspaceID, docID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM knowledge_doc
		WHERE id = $1 AND workspace_id = $2`, docID, workspaceID)
	return err
}

// --- Chunks ---

// ReplaceChunks wipes existing chunks for the doc and inserts the new set.
// Mentions are deleted automatically via ON DELETE CASCADE. Runs in a single
// transaction so callers never observe a partially-reindexed doc.
func (s *Store) ReplaceChunks(ctx context.Context, docID, workspaceID string, chunks []Chunk, embeddings [][]float32) error {
	if len(embeddings) > 0 && len(embeddings) != len(chunks) {
		return fmt.Errorf("chunks/embeddings length mismatch: %d vs %d", len(chunks), len(embeddings))
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // safe no-op if Commit succeeds

	if _, err := tx.Exec(ctx, `DELETE FROM knowledge_chunk WHERE doc_id = $1`, docID); err != nil {
		return err
	}
	for i, c := range chunks {
		var emb any
		if i < len(embeddings) && embeddings[i] != nil {
			emb = vectorLiteral(embeddings[i])
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO knowledge_chunk
			(doc_id, workspace_id, ordinal, heading, content, embedding)
			VALUES ($1, $2, $3, $4, $5, $6::vector)`,
			docID, workspaceID, c.Ordinal, c.Heading, c.Content, emb)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// GetChunksForDoc returns all chunks of a doc, ordered by ordinal.
func (s *Store) GetChunksForDoc(ctx context.Context, docID string) ([]Chunk, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, doc_id, ordinal, COALESCE(heading, ''), content
		 FROM knowledge_chunk WHERE doc_id = $1 ORDER BY ordinal`, docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Chunk
	for rows.Next() {
		var c Chunk
		if err := rows.Scan(&c.ID, &c.DocID, &c.Ordinal, &c.Heading, &c.Content); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Entities ---

// UpsertEntity creates or updates an entity, merging aliases with any existing set.
// sourceDocID, when non-empty, is stored on INSERT so we can prune stale entities
// on reindex. It is intentionally NOT updated on conflict: if two docs produce the
// same (workspace, kind, name) entity the first writer's doc-link is preserved.
func (s *Store) UpsertEntity(ctx context.Context, workspaceID string, e ExtractedEntity, embedding []float32, source, sourceDocID string) (string, error) {
	// Normalize nil maps/slices so pgx binds '{}' / '[]' instead of SQL NULL
	// (both columns are NOT NULL DEFAULT '{}').
	if e.Attributes == nil {
		e.Attributes = map[string]any{}
	}
	if e.Aliases == nil {
		e.Aliases = []string{}
	}
	attrs, err := json.Marshal(e.Attributes)
	if err != nil || len(attrs) == 0 {
		attrs = []byte("{}")
	}
	var emb any
	if embedding != nil {
		emb = vectorLiteral(embedding)
	}
	var docIDArg any
	if sourceDocID != "" {
		docIDArg = sourceDocID
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO knowledge_entity
		(workspace_id, kind, name, aliases, description, attributes, embedding, source, source_doc_id)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::vector, $8, $9)
		ON CONFLICT (workspace_id, kind, name) DO UPDATE SET
		  aliases     = ARRAY(SELECT DISTINCT unnest(knowledge_entity.aliases || EXCLUDED.aliases)),
		  description = CASE WHEN EXCLUDED.description <> '' THEN EXCLUDED.description ELSE knowledge_entity.description END,
		  attributes  = knowledge_entity.attributes || EXCLUDED.attributes,
		  embedding   = COALESCE(EXCLUDED.embedding, knowledge_entity.embedding),
		  updated_at  = now()
		RETURNING id`,
		workspaceID, string(e.Kind), e.Name, e.Aliases, e.Description, attrs, emb, source, docIDArg)
	var id string
	if err := row.Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

// FindEntity returns the entity ID for (workspace, kind, name), or "" if not found.
func (s *Store) FindEntity(ctx context.Context, workspaceID string, kind EntityKind, name string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM knowledge_entity
		 WHERE workspace_id = $1 AND kind = $2 AND name = $3`,
		workspaceID, string(kind), name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// ListEntities returns entities filtered by kind (optional).
func (s *Store) ListEntities(ctx context.Context, workspaceID string, kind EntityKind) ([]Entity, error) {
	var rows pgx.Rows
	var err error
	if kind == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, workspace_id, kind, name, aliases, description,
			        attributes, source, created_at, updated_at
			 FROM knowledge_entity WHERE workspace_id = $1
			 ORDER BY kind, name`, workspaceID)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, workspace_id, kind, name, aliases, description,
			        attributes, source, created_at, updated_at
			 FROM knowledge_entity WHERE workspace_id = $1 AND kind = $2
			 ORDER BY name`, workspaceID, string(kind))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEntity returns a single entity by ID.
func (s *Store) GetEntity(ctx context.Context, workspaceID, entityID string) (Entity, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, workspace_id, kind, name, aliases, description,
		        attributes, source, created_at, updated_at
		 FROM knowledge_entity WHERE id = $1 AND workspace_id = $2`, entityID, workspaceID)
	e, err := scanEntity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entity{}, ErrNotFound
	}
	return e, err
}

// UpdateEntityParams carries optional user edits.
type UpdateEntityParams struct {
	Name        *string
	Aliases     *[]string
	Description *string
	Attributes  *map[string]any
}

// UpdateEntity applies a manual edit from the UI. Marks source='manual'.
func (s *Store) UpdateEntity(ctx context.Context, workspaceID, entityID string, p UpdateEntityParams) (Entity, error) {
	setParts := []string{"updated_at = now()", "source = 'manual'"}
	args := []any{entityID, workspaceID}
	add := func(col string, v any) {
		args = append(args, v)
		setParts = append(setParts, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if p.Name != nil {
		add("name", *p.Name)
	}
	if p.Aliases != nil {
		aliases := *p.Aliases
		if aliases == nil {
			aliases = []string{}
		}
		add("aliases", aliases)
	}
	if p.Description != nil {
		add("description", *p.Description)
	}
	if p.Attributes != nil {
		b, _ := json.Marshal(*p.Attributes)
		if len(b) == 0 {
			b = []byte("{}")
		}
		add("attributes", b)
	}
	query := fmt.Sprintf(`UPDATE knowledge_entity SET %s
		WHERE id = $1 AND workspace_id = $2
		RETURNING id, workspace_id, kind, name, aliases, description,
		          attributes, source, created_at, updated_at`,
		strings.Join(setParts, ", "))
	row := s.pool.QueryRow(ctx, query, args...)
	e, err := scanEntity(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entity{}, ErrNotFound
	}
	return e, err
}

// --- Relations ---

// UpsertRelation is idempotent via the UNIQUE (workspace, source, rel, target).
func (s *Store) UpsertRelation(ctx context.Context, workspaceID, sourceID, targetID string, rel RelationKind, evidenceDocID string) error {
	var ev any
	if evidenceDocID != "" {
		ev = evidenceDocID
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO knowledge_relation
		(workspace_id, source_id, target_id, relation, evidence_doc_id)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (workspace_id, source_id, relation, target_id) DO UPDATE
		  SET evidence_doc_id = COALESCE(EXCLUDED.evidence_doc_id, knowledge_relation.evidence_doc_id)`,
		workspaceID, sourceID, targetID, string(rel), ev)
	return err
}

// DeleteRelationsForDoc removes any relations whose evidence doc was this doc.
// Used when a doc is re-indexed.
func (s *Store) DeleteRelationsForDoc(ctx context.Context, docID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM knowledge_relation WHERE evidence_doc_id = $1`, docID)
	return err
}

// EntityNeighbors returns entities connected to entityID via any relation
// (either direction).
func (s *Store) EntityNeighbors(ctx context.Context, workspaceID, entityID string) ([]RelatedEntity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.kind, e.name, e.description, r.relation, 'out' AS dir
		  FROM knowledge_relation r
		  JOIN knowledge_entity e ON e.id = r.target_id
		 WHERE r.workspace_id = $1 AND r.source_id = $2
		UNION
		SELECT e.id, e.kind, e.name, e.description, r.relation, 'in' AS dir
		  FROM knowledge_relation r
		  JOIN knowledge_entity e ON e.id = r.source_id
		 WHERE r.workspace_id = $1 AND r.target_id = $2
		LIMIT 20`, workspaceID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RelatedEntity
	for rows.Next() {
		var re RelatedEntity
		var dir string
		if err := rows.Scan(&re.ID, &re.Kind, &re.Name, &re.Description, &re.Relation, &dir); err != nil {
			return nil, err
		}
		if dir == "in" {
			re.Relation = "←" + re.Relation
		}
		out = append(out, re)
	}
	return out, rows.Err()
}

// GraphLink is a serializable edge used by the graph API.
type GraphLink struct {
	SourceID string       `json:"source_id"`
	TargetID string       `json:"target_id"`
	Relation RelationKind `json:"relation"`
}

// ListRelations returns every relation in a workspace as lightweight links.
// Used by the full-workspace graph endpoint.
func (s *Store) ListRelations(ctx context.Context, workspaceID string) ([]GraphLink, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT source_id, target_id, relation
		 FROM knowledge_relation WHERE workspace_id = $1`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GraphLink
	for rows.Next() {
		var l GraphLink
		if err := rows.Scan(&l.SourceID, &l.TargetID, &l.Relation); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ImplicitLink is a derived edge: two entities mentioned in the same chunk
// (soft contextual association). Weight = count of shared chunks.
type ImplicitLink struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	Weight   int    `json:"weight"`
}

// ListImplicitLinks returns co-mention edges between entities that share at
// least one chunk. Capped at `limit` strongest pairs to avoid DoS on large
// workspaces. A cap of 0 means "no cap" (tests only).
func (s *Store) ListImplicitLinks(ctx context.Context, workspaceID string, limit int) ([]ImplicitLink, error) {
	q := `
		SELECT a.entity_id, b.entity_id, COUNT(*) AS weight
		  FROM knowledge_chunk_mention a
		  JOIN knowledge_chunk_mention b
		    ON a.chunk_id = b.chunk_id AND a.entity_id < b.entity_id
		  JOIN knowledge_chunk c ON c.id = a.chunk_id
		 WHERE c.workspace_id = $1
		 GROUP BY a.entity_id, b.entity_id
		 ORDER BY weight DESC`
	args := []any{workspaceID}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ImplicitLink
	for rows.Next() {
		var l ImplicitLink
		if err := rows.Scan(&l.SourceID, &l.TargetID, &l.Weight); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SiblingLink is a derived edge between two entities that share the same
// `part_of` parent. Visual cue that they belong to the same scope.
type SiblingLink struct {
	SourceID string `json:"source_id"`
	TargetID string `json:"target_id"`
	ParentID string `json:"parent_id"`
}

// ListSiblingLinks returns pairs (A, B) such that both A and B have a
// `part_of` relation pointing to the same parent entity. Capped like
// ListImplicitLinks to avoid DoS.
func (s *Store) ListSiblingLinks(ctx context.Context, workspaceID string, limit int) ([]SiblingLink, error) {
	q := `
		SELECT r1.source_id, r2.source_id, r1.target_id
		  FROM knowledge_relation r1
		  JOIN knowledge_relation r2
		    ON r1.target_id = r2.target_id
		   AND r1.source_id < r2.source_id
		   AND r1.relation = 'part_of' AND r2.relation = 'part_of'
		 WHERE r1.workspace_id = $1 AND r2.workspace_id = $1`
	args := []any{workspaceID}
	if limit > 0 {
		q += " LIMIT $2"
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SiblingLink
	for rows.Next() {
		var l SiblingLink
		if err := rows.Scan(&l.SourceID, &l.TargetID, &l.ParentID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// MergeEntities folds `dropID` into `keepID`: all relations and mentions
// that pointed at dropID now point at keepID, dropID's name and aliases
// are added to keepID's alias set, and dropID is deleted. Runs inside a
// single transaction so callers never observe an inconsistent graph.
//
// Returns ErrNotFound if either entity is missing. Returns an error when
// the two entities belong to different workspaces or different kinds —
// merging across kinds is almost always a mistake.
func (s *Store) MergeEntities(ctx context.Context, workspaceID, keepID, dropID string) error {
	if keepID == dropID {
		return fmt.Errorf("cannot merge entity into itself")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Sanity check: both entities live in this workspace and share kind.
	var keepKind, dropKind, keepName, dropName string
	var dropAliases []string
	err = tx.QueryRow(ctx,
		`SELECT kind, name FROM knowledge_entity
		 WHERE id = $1 AND workspace_id = $2`, keepID, workspaceID).Scan(&keepKind, &keepName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	err = tx.QueryRow(ctx,
		`SELECT kind, name, aliases FROM knowledge_entity
		 WHERE id = $1 AND workspace_id = $2`, dropID, workspaceID).Scan(&dropKind, &dropName, &dropAliases)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if keepKind != dropKind {
		return fmt.Errorf("cannot merge across kinds (%s vs %s)", keepKind, dropKind)
	}

	// 1. Re-point outgoing relations. Delete duplicates that would violate
	//    the (workspace, source, relation, target) unique constraint.
	if _, err := tx.Exec(ctx, `
		DELETE FROM knowledge_relation r
		 USING knowledge_relation other
		 WHERE r.source_id = $2
		   AND other.source_id = $1
		   AND r.relation = other.relation
		   AND r.target_id = other.target_id
		   AND r.workspace_id = $3`, keepID, dropID, workspaceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE knowledge_relation SET source_id = $1
		 WHERE source_id = $2 AND workspace_id = $3`, keepID, dropID, workspaceID); err != nil {
		return err
	}

	// 2. Re-point incoming relations with the same dedup step.
	if _, err := tx.Exec(ctx, `
		DELETE FROM knowledge_relation r
		 USING knowledge_relation other
		 WHERE r.target_id = $2
		   AND other.target_id = $1
		   AND r.relation = other.relation
		   AND r.source_id = other.source_id
		   AND r.workspace_id = $3`, keepID, dropID, workspaceID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE knowledge_relation SET target_id = $1
		 WHERE target_id = $2 AND workspace_id = $3`, keepID, dropID, workspaceID); err != nil {
		return err
	}

	// 3. Re-point chunk mentions. Duplicates get deleted first.
	if _, err := tx.Exec(ctx, `
		DELETE FROM knowledge_chunk_mention m
		 WHERE m.entity_id = $2
		   AND EXISTS (
		     SELECT 1 FROM knowledge_chunk_mention keep
		      WHERE keep.chunk_id = m.chunk_id AND keep.entity_id = $1
		   )`, keepID, dropID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE knowledge_chunk_mention SET entity_id = $1 WHERE entity_id = $2`,
		keepID, dropID); err != nil {
		return err
	}

	// 4. Self-referential edges become no-ops — delete.
	if _, err := tx.Exec(ctx,
		`DELETE FROM knowledge_relation
		 WHERE source_id = $1 AND target_id = $1 AND workspace_id = $2`,
		keepID, workspaceID); err != nil {
		return err
	}

	// 5. Merge aliases: keep's alias set gets dropName + dropAliases,
	//    deduped, with existing aliases preserved.
	newAliases := append([]string{dropName}, dropAliases...)
	if _, err := tx.Exec(ctx, `
		UPDATE knowledge_entity
		   SET aliases = ARRAY(SELECT DISTINCT unnest(aliases || $2::text[])),
		       updated_at = now()
		 WHERE id = $1`, keepID, newAliases); err != nil {
		return err
	}

	// 6. Delete the dropped entity.
	if _, err := tx.Exec(ctx,
		`DELETE FROM knowledge_entity WHERE id = $1 AND workspace_id = $2`,
		dropID, workspaceID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// --- Citations (task ↔ chunk) ---

// Citation records one chunk that was injected into an agent task's
// context. Rows are created at claim time when the knowledge search
// returns hits.
type Citation struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	ChunkID     string    `json:"chunk_id"`
	DocID       string    `json:"doc_id"`
	DocTitle    string    `json:"doc_title"`
	DocCategory string    `json:"doc_category"`
	Heading     string    `json:"heading"`
	Content     string    `json:"content"`
	Rank        int       `json:"rank"`
	Score       float64   `json:"score"`
	CreatedAt   time.Time `json:"created_at"`
}

// InsertCitations writes one citation row per hit inside a transaction so
// callers never observe a partially-recorded set.
func (s *Store) InsertCitations(ctx context.Context, taskID, workspaceID string, hits []Hit) error {
	if len(hits) == 0 || taskID == "" {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for i, h := range hits {
		if h.ChunkID == "" || h.DocID == "" {
			continue
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO knowledge_citation
			(task_id, chunk_id, doc_id, workspace_id, rank, score)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			taskID, h.ChunkID, h.DocID, workspaceID, i+1, h.Score)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListCitationsForTask returns citations for a task, enriched with doc +
// chunk metadata so the UI can render them without extra round-trips.
func (s *Store) ListCitationsForTask(ctx context.Context, taskID string) ([]Citation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.task_id, c.chunk_id, c.doc_id,
		       d.title, d.category,
		       COALESCE(ch.heading, ''), ch.content,
		       c.rank, c.score, c.created_at
		  FROM knowledge_citation c
		  JOIN knowledge_doc d ON d.id = c.doc_id
		  JOIN knowledge_chunk ch ON ch.id = c.chunk_id
		 WHERE c.task_id = $1
		 ORDER BY c.rank ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Citation
	for rows.Next() {
		var c Citation
		if err := rows.Scan(&c.ID, &c.TaskID, &c.ChunkID, &c.DocID,
			&c.DocTitle, &c.DocCategory, &c.Heading, &c.Content,
			&c.Rank, &c.Score, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// --- Chunk ↔ Entity mentions ---

// LinkChunkToEntity inserts into knowledge_chunk_mention (idempotent).
func (s *Store) LinkChunkToEntity(ctx context.Context, chunkID, entityID string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO knowledge_chunk_mention (chunk_id, entity_id)
		 VALUES ($1, $2) ON CONFLICT DO NOTHING`, chunkID, entityID)
	return err
}

// EntitiesForChunks returns the set of entities mentioned in any of chunkIDs.
func (s *Store) EntitiesForChunks(ctx context.Context, chunkIDs []string) (map[string][]RelatedEntity, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.chunk_id, e.id, e.kind, e.name, e.description
		FROM knowledge_chunk_mention m
		JOIN knowledge_entity e ON e.id = m.entity_id
		WHERE m.chunk_id = ANY($1::uuid[])`, chunkIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]RelatedEntity)
	for rows.Next() {
		var chunkID string
		var re RelatedEntity
		if err := rows.Scan(&chunkID, &re.ID, &re.Kind, &re.Name, &re.Description); err != nil {
			return nil, err
		}
		out[chunkID] = append(out[chunkID], re)
	}
	return out, rows.Err()
}

// --- helpers ---

// vectorLiteral serializes a []float32 in the literal format pgvector
// accepts: "[v1,v2,...]". Returning nil for an empty/nil slice lets callers
// pass a SQL NULL via the any-typed bind parameter.
func vectorLiteral(v []float32) any {
	if len(v) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// rowScanner is satisfied by both *pgx.Rows and pgx.Row.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDoc(r rowScanner) (Doc, error) {
	var d Doc
	var (
		authorID    *string
		indexingErr *string
		statsBytes  []byte
		extractedAt *time.Time
	)
	err := r.Scan(
		&d.ID, &d.WorkspaceID, &d.Title, &d.Slug, &d.Category, &d.Body, &d.Tags,
		&authorID, &d.Status, &d.IndexingStatus, &indexingErr, &statsBytes, &extractedAt,
		&d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		return Doc{}, err
	}
	d.AuthorID = authorID
	d.IndexingError = indexingErr
	d.ExtractedAt = extractedAt
	if len(statsBytes) > 0 {
		_ = json.Unmarshal(statsBytes, &d.Stats)
	}
	return d, nil
}

// DeleteEntity removes a single entity by ID (workspace-scoped).
func (s *Store) DeleteEntity(ctx context.Context, workspaceID, entityID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM knowledge_entity WHERE id = $1 AND workspace_id = $2`,
		entityID, workspaceID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListOrphanEntities returns extracted entities that have no relation edges —
// neither as source nor as target. These are candidates for cleanup after a
// doc is updated or replaced.
func (s *Store) ListOrphanEntities(ctx context.Context, workspaceID string) ([]Entity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.workspace_id, e.kind, e.name, e.aliases, e.description,
		       e.attributes, e.source, e.created_at, e.updated_at
		FROM knowledge_entity e
		LEFT JOIN knowledge_relation r1 ON e.id = r1.source_id
		LEFT JOIN knowledge_relation r2 ON e.id = r2.target_id
		WHERE e.workspace_id = $1
		  AND r1.source_id IS NULL
		  AND r2.target_id IS NULL
		ORDER BY e.created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entity
	for rows.Next() {
		e, err := scanEntity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteStaleDocEntities removes extracted entities that were previously linked
// to docID via source_doc_id but are not in keepIDs. Called after reindex to
// prune entities from the previous version of a doc. Entities with
// source_doc_id = NULL (legacy) or source = 'manual' are never touched.
func (s *Store) DeleteStaleDocEntities(ctx context.Context, docID string, keepIDs []string) error {
	if len(keepIDs) == 0 {
		// Delete all extracted entities for this doc (body became empty or extraction failed).
		_, err := s.pool.Exec(ctx,
			`DELETE FROM knowledge_entity WHERE source_doc_id = $1 AND source = 'extracted'`,
			docID)
		return err
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM knowledge_entity
		 WHERE source_doc_id = $1
		   AND source = 'extracted'
		   AND id != ALL($2::uuid[])`,
		docID, keepIDs)
	return err
}

func scanEntity(r rowScanner) (Entity, error) {
	var e Entity
	var attrBytes []byte
	err := r.Scan(
		&e.ID, &e.WorkspaceID, &e.Kind, &e.Name, &e.Aliases, &e.Description,
		&attrBytes, &e.Source, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		return Entity{}, err
	}
	if len(attrBytes) > 0 {
		_ = json.Unmarshal(attrBytes, &e.Attributes)
	}
	if e.Attributes == nil {
		e.Attributes = map[string]any{}
	}
	return e, nil
}
