package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/hira-vn/cli/server/pkg/knowledge"
)

// --- Doc endpoints ---

// ListKnowledgeDocs returns all docs for the current workspace. Optional query
// param status=draft|published|archived filters by publication state.
func (h *Handler) ListKnowledgeDocs(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace required")
		return
	}
	status := knowledge.DocStatus(r.URL.Query().Get("status"))
	docs, err := h.KnowledgeService.ListDocs(r.Context(), workspaceID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if docs == nil {
		docs = []knowledge.Doc{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"docs": docs})
}

// CreateKnowledgeDocRequest is the body for POST /api/knowledge/docs.
type CreateKnowledgeDocRequest struct {
	Title    string                `json:"title"`
	Slug     string                `json:"slug"`
	Category knowledge.DocCategory `json:"category"`
	Body     string                `json:"body"`
	Tags     []string              `json:"tags"`
	Status   knowledge.DocStatus   `json:"status"`
}

// CreateKnowledgeDoc inserts a new doc and begins indexing.
func (h *Handler) CreateKnowledgeDoc(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateKnowledgeDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	doc, err := h.KnowledgeService.CreateDoc(r.Context(), knowledge.CreateDocParams{
		WorkspaceID: workspaceID,
		Title:       req.Title,
		Slug:        req.Slug,
		Category:    req.Category,
		Body:        req.Body,
		Tags:        req.Tags,
		AuthorID:    userID,
		Status:      req.Status,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a doc with this slug already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

// GetKnowledgeDoc returns a single doc by ID.
func (h *Handler) GetKnowledgeDoc(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	docID := chi.URLParam(r, "id")

	doc, err := h.KnowledgeService.GetDoc(r.Context(), workspaceID, docID)
	if errors.Is(err, knowledge.ErrNotFound) {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// UpdateKnowledgeDocRequest is the body for PUT /api/knowledge/docs/{id}.
type UpdateKnowledgeDocRequest struct {
	Title    *string                `json:"title,omitempty"`
	Category *knowledge.DocCategory `json:"category,omitempty"`
	Body     *string                `json:"body,omitempty"`
	Tags     *[]string              `json:"tags,omitempty"`
	Status   *knowledge.DocStatus   `json:"status,omitempty"`
}

// UpdateKnowledgeDoc applies a partial update.
func (h *Handler) UpdateKnowledgeDoc(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	docID := chi.URLParam(r, "id")

	var req UpdateKnowledgeDocRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}

	doc, err := h.KnowledgeService.UpdateDoc(r.Context(), workspaceID, docID, knowledge.UpdateDocParams{
		Title:    req.Title,
		Category: req.Category,
		Body:     req.Body,
		Tags:     req.Tags,
		Status:   req.Status,
	})
	if errors.Is(err, knowledge.ErrNotFound) {
		writeError(w, http.StatusNotFound, "doc not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// DeleteKnowledgeDoc removes a doc.
func (h *Handler) DeleteKnowledgeDoc(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	docID := chi.URLParam(r, "id")

	if err := h.KnowledgeService.DeleteDoc(r.Context(), workspaceID, docID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReindexKnowledgeDoc forces re-indexing of an existing doc.
func (h *Handler) ReindexKnowledgeDoc(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	docID := chi.URLParam(r, "id")

	if err := h.KnowledgeService.Reindex(r.Context(), workspaceID, docID); err != nil {
		if errors.Is(err, knowledge.ErrNotFound) {
			writeError(w, http.StatusNotFound, "doc not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

// --- Entity endpoints ---

// ListKnowledgeEntities returns entities, optionally filtered by ?kind=.
// Pass ?orphan=true to return only entities with no relation edges.
func (h *Handler) ListKnowledgeEntities(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())

	if r.URL.Query().Get("orphan") == "true" {
		entities, err := h.KnowledgeService.ListOrphanEntities(r.Context(), workspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if entities == nil {
			entities = []knowledge.Entity{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"entities": entities})
		return
	}

	kind := knowledge.EntityKind(r.URL.Query().Get("kind"))
	entities, err := h.KnowledgeService.ListEntities(r.Context(), workspaceID, kind)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entities == nil {
		entities = []knowledge.Entity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entities": entities})
}

// DeleteKnowledgeEntity removes an entity by ID (workspace-scoped).
func (h *Handler) DeleteKnowledgeEntity(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	entityID := chi.URLParam(r, "id")

	if err := h.KnowledgeService.DeleteEntity(r.Context(), workspaceID, entityID); err != nil {
		if errors.Is(err, knowledge.ErrNotFound) {
			writeError(w, http.StatusNotFound, "entity not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetKnowledgeEntity returns a single entity.
func (h *Handler) GetKnowledgeEntity(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	entityID := chi.URLParam(r, "id")

	ent, err := h.KnowledgeService.GetEntity(r.Context(), workspaceID, entityID)
	if errors.Is(err, knowledge.ErrNotFound) {
		writeError(w, http.StatusNotFound, "entity not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

// UpdateKnowledgeEntityRequest is the body for PUT /api/knowledge/entities/{id}.
type UpdateKnowledgeEntityRequest struct {
	Name        *string         `json:"name,omitempty"`
	Aliases     *[]string       `json:"aliases,omitempty"`
	Description *string         `json:"description,omitempty"`
	Attributes  *map[string]any `json:"attributes,omitempty"`
}

// UpdateKnowledgeEntity applies manual edits.
func (h *Handler) UpdateKnowledgeEntity(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	entityID := chi.URLParam(r, "id")

	var req UpdateKnowledgeEntityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	ent, err := h.KnowledgeService.UpdateEntity(r.Context(), workspaceID, entityID, knowledge.UpdateEntityParams{
		Name:        req.Name,
		Aliases:     req.Aliases,
		Description: req.Description,
		Attributes:  req.Attributes,
	})
	if errors.Is(err, knowledge.ErrNotFound) {
		writeError(w, http.StatusNotFound, "entity not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ent)
}

// GetKnowledgeGraph returns the full workspace knowledge graph
// (all entities + explicit relations + derived co-mention and sibling edges).
// Used for force-directed visualization with implicit-tier rendering.
func (h *Handler) GetKnowledgeGraph(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	res, err := h.KnowledgeService.Graph(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if res.Nodes == nil {
		res.Nodes = []knowledge.Entity{}
	}
	if res.Links == nil {
		res.Links = []knowledge.GraphLink{}
	}
	if res.ImplicitLinks == nil {
		res.ImplicitLinks = []knowledge.ImplicitLink{}
	}
	if res.SiblingLinks == nil {
		res.SiblingLinks = []knowledge.SiblingLink{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"nodes":          res.Nodes,
		"links":          res.Links,
		"implicit_links": res.ImplicitLinks,
		"sibling_links":  res.SiblingLinks,
	})
}

// MergeKnowledgeEntityRequest is the body for POST /api/knowledge/entities/{id}/merge.
type MergeKnowledgeEntityRequest struct {
	MergeFromID string `json:"merge_from_id"`
}

// MergeKnowledgeEntity folds an entity into another (same kind, same
// workspace). All relations + mentions pointing to the dropped entity are
// re-targeted to the kept entity, and the dropped entity's name/aliases
// are added to the kept entity's alias set.
func (h *Handler) MergeKnowledgeEntity(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	keepID := chi.URLParam(r, "id")

	var req MergeKnowledgeEntityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if req.MergeFromID == "" {
		writeError(w, http.StatusBadRequest, "merge_from_id is required")
		return
	}
	if err := h.KnowledgeService.MergeEntities(r.Context(), workspaceID, keepID, req.MergeFromID); err != nil {
		if errors.Is(err, knowledge.ErrNotFound) {
			writeError(w, http.StatusNotFound, "entity not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetKnowledgeEntityGraph returns 1-hop neighbors for an entity.
func (h *Handler) GetKnowledgeEntityGraph(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	entityID := chi.URLParam(r, "id")

	neighbors, err := h.KnowledgeService.EntityGraph(r.Context(), workspaceID, entityID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if neighbors == nil {
		neighbors = []knowledge.RelatedEntity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"neighbors": neighbors})
}

// --- Search ---

// ListTaskKnowledgeCitations returns the knowledge chunks that were
// injected into an agent task's context at claim time.
func (h *Handler) ListTaskKnowledgeCitations(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	citations, err := h.KnowledgeService.ListCitationsForTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if citations == nil {
		citations = []knowledge.Citation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"citations": citations})
}

// SearchKnowledge runs hybrid retrieval and returns top-K hits.
func (h *Handler) SearchKnowledge(w http.ResponseWriter, r *http.Request) {
	workspaceID := ctxWorkspaceID(r.Context())
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "query (q) is required")
		return
	}
	limit := 8
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	hits, err := h.KnowledgeService.Search(r.Context(), workspaceID, q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if hits == nil {
		hits = []knowledge.Hit{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}
