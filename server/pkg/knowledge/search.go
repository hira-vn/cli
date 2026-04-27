package knowledge

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
)

// lenientTokenRE extracts runs of letters or digits (Unicode-aware) for
// building a relaxed `to_tsquery` variant when the strict plainto_tsquery
// lane returns too few results. Keeps Vietnamese diacritics intact.
var lenientTokenRE = regexp.MustCompile(`[\p{L}\p{N}]{2,}`)

// SearchRequest parameters for hybrid search.
type SearchRequest struct {
	WorkspaceID string
	Query       string
	Limit       int
}

// Search runs a hybrid BM25 + vector cosine search across chunks within a
// workspace, fuses the ranks with RRF, and enriches the top hits with the
// entities they mention. Only chunks from `published` docs are considered.
func (s *Store) Search(ctx context.Context, embedder Embedder, req SearchRequest) ([]Hit, error) {
	if req.Limit <= 0 {
		req.Limit = 8
	}
	q := strings.TrimSpace(req.Query)
	if q == "" || req.WorkspaceID == "" {
		return nil, nil
	}

	const poolSize = 50 // per-lane pool before fusion

	// Lane 1: BM25.
	bm25Rows, err := s.pool.Query(ctx, `
		SELECT c.id, ts_rank_cd(c.text_search, plainto_tsquery('simple', $2)) AS rank
		FROM knowledge_chunk c
		JOIN knowledge_doc d ON d.id = c.doc_id
		WHERE c.workspace_id = $1 AND d.status = 'published'
		  AND c.text_search @@ plainto_tsquery('simple', $2)
		ORDER BY rank DESC
		LIMIT $3`, req.WorkspaceID, q, poolSize)
	if err != nil {
		return nil, fmt.Errorf("bm25: %w", err)
	}
	bm25Ranks := map[string]int{}
	for bm25Rows.Next() {
		var id string
		var rank float64
		if err := bm25Rows.Scan(&id, &rank); err != nil {
			bm25Rows.Close()
			return nil, err
		}
		bm25Ranks[id] = len(bm25Ranks) + 1
	}
	bm25Rows.Close()

	// Lane 1b: Lenient BM25. When the strict `plainto_tsquery` (which ANDs
	// all terms) returns few results AND the query has 2+ tokens, fall
	// back to a `to_tsquery` with OR-joined tokens so multi-word queries
	// like "brand voice policy" still surface chunks containing ANY of
	// those words. Runs always; RRF-fused so it doesn't hurt precision of
	// strict matches.
	lenientRanks := map[string]int{}
	if tsQ := buildLenientTSQuery(q); tsQ != "" {
		lRows, err := s.pool.Query(ctx, `
			SELECT c.id, ts_rank_cd(c.text_search, to_tsquery('simple', $2)) AS rank
			FROM knowledge_chunk c
			JOIN knowledge_doc d ON d.id = c.doc_id
			WHERE c.workspace_id = $1 AND d.status = 'published'
			  AND c.text_search @@ to_tsquery('simple', $2)
			ORDER BY rank DESC
			LIMIT $3`, req.WorkspaceID, tsQ, poolSize)
		if err == nil {
			for lRows.Next() {
				var id string
				var rank float64
				if err := lRows.Scan(&id, &rank); err == nil {
					lenientRanks[id] = len(lenientRanks) + 1
				}
			}
			lRows.Close()
		}
	}

	// Lane 2: Vector (cosine). Skip cleanly if embedder produces zero vector.
	vecRanks := map[string]int{}
	vec, err := embedder.Embed(ctx, []string{q})
	vectorEnabled := err == nil && len(vec) == 1 && !isZeroVector(vec[0])
	if vectorEnabled {
		vRows, err := s.pool.Query(ctx, `
			SELECT c.id
			FROM knowledge_chunk c
			JOIN knowledge_doc d ON d.id = c.doc_id
			WHERE c.workspace_id = $1 AND d.status = 'published'
			  AND c.embedding IS NOT NULL
			ORDER BY c.embedding <=> $2::vector
			LIMIT $3`, req.WorkspaceID, vectorLiteral(vec[0]), poolSize)
		if err == nil {
			for vRows.Next() {
				var id string
				if err := vRows.Scan(&id); err == nil {
					vecRanks[id] = len(vecRanks) + 1
				}
			}
			vRows.Close()
		}
	} else {
		// One line per call is noisy, but this fires only on
		// zero-vector/error paths — you want to see it to diagnose why
		// retrieval is BM25-only in your workspace.
		slog.Warn("knowledge.search.vector_lane_skipped",
			"workspace_id", req.WorkspaceID,
			"query_bytes", len(q),
			"reason", zeroVectorReason(err, vec),
		)
	}

	// RRF fusion.
	const kRRF = 60.0
	scores := map[string]float64{}
	for id, r := range bm25Ranks {
		scores[id] += 1.0 / (kRRF + float64(r))
	}
	for id, r := range lenientRanks {
		// Half weight so strict matches still dominate the top of the ranking.
		scores[id] += 0.5 / (kRRF + float64(r))
	}
	for id, r := range vecRanks {
		scores[id] += 1.0 / (kRRF + float64(r))
	}
	if len(scores) == 0 {
		return nil, nil
	}

	// Sort and take top-N.
	type scored struct {
		id    string
		score float64
	}
	ranked := make([]scored, 0, len(scores))
	for id, sc := range scores {
		ranked = append(ranked, scored{id, sc})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > req.Limit {
		ranked = ranked[:req.Limit]
	}

	topIDs := make([]string, len(ranked))
	for i, r := range ranked {
		topIDs[i] = r.id
	}

	// Fetch chunk + doc metadata.
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.doc_id, COALESCE(c.heading, ''), c.content,
		       d.title, d.category
		FROM knowledge_chunk c
		JOIN knowledge_doc d ON d.id = c.doc_id
		WHERE c.id = ANY($1::uuid[])`, topIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := make(map[string]Hit, len(topIDs))
	for rows.Next() {
		var h Hit
		var cat string
		if err := rows.Scan(&h.ChunkID, &h.DocID, &h.Heading, &h.Content, &h.DocTitle, &cat); err != nil {
			return nil, err
		}
		h.DocCategory = DocCategory(cat)
		byID[h.ChunkID] = h
	}

	// Entity enrichment (one-hop mentions).
	entitiesByChunk, _ := s.EntitiesForChunks(ctx, topIDs)

	out := make([]Hit, 0, len(ranked))
	for _, r := range ranked {
		h, ok := byID[r.id]
		if !ok {
			continue
		}
		h.Score = r.score
		h.RelatedEntities = entitiesByChunk[r.id]
		out = append(out, h)
	}
	return out, nil
}

func isZeroVector(v []float32) bool {
	for _, f := range v {
		if f != 0 {
			return false
		}
	}
	return true
}

// buildLenientTSQuery extracts letters/digits runs from the raw query and
// joins them with ` | ` so `to_tsquery('simple', …)` treats the query as
// OR-of-terms. Returns an empty string when no usable token is found.
func buildLenientTSQuery(q string) string {
	tokens := lenientTokenRE.FindAllString(strings.ToLower(q), -1)
	if len(tokens) == 0 {
		return ""
	}
	// Dedup while preserving order.
	seen := map[string]bool{}
	clean := tokens[:0]
	for _, t := range tokens {
		if !seen[t] {
			seen[t] = true
			clean = append(clean, t)
		}
	}
	return strings.Join(clean, " | ")
}

// zeroVectorReason classifies why the vector lane is unavailable so the
// warn log helps ops figure out whether the key is missing, rate-limited,
// or the embedder is simply a no-op (no API key configured).
func zeroVectorReason(err error, vec [][]float32) string {
	if err != nil {
		return "embed_error: " + err.Error()
	}
	if len(vec) != 1 {
		return fmt.Sprintf("unexpected_vec_count: %d", len(vec))
	}
	return "zero_vector_returned"
}
