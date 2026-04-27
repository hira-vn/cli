-- Business knowledge graph. Stores markdown docs (SOPs, brand guides,
-- policies...) plus an extracted graph of entities and their relations.
-- Powers semantic context injection for agent tasks.

-- 1. Documents
CREATE TABLE knowledge_doc (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    category TEXT NOT NULL DEFAULT 'general',
    body TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT '{}',
    author_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    indexing_status TEXT NOT NULL DEFAULT 'idle',
    indexing_error TEXT,
    stats JSONB NOT NULL DEFAULT '{}',
    extracted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, slug)
);
CREATE INDEX knowledge_doc_workspace_idx ON knowledge_doc (workspace_id, status);

-- 2. Chunks (retrieval unit)
CREATE TABLE knowledge_chunk (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doc_id UUID NOT NULL REFERENCES knowledge_doc(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    ordinal INT NOT NULL,
    heading TEXT,
    content TEXT NOT NULL,
    embedding vector(1536),
    text_search tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(heading, '') || ' ' || content)
    ) STORED,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX knowledge_chunk_doc_idx ON knowledge_chunk (doc_id);
CREATE INDEX knowledge_chunk_workspace_idx ON knowledge_chunk (workspace_id);
CREATE INDEX knowledge_chunk_text_search_idx ON knowledge_chunk USING GIN (text_search);
CREATE INDEX knowledge_chunk_embedding_idx ON knowledge_chunk
    USING hnsw (embedding vector_cosine_ops);

-- 3. Entities (graph nodes)
CREATE TABLE knowledge_entity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    aliases TEXT[] NOT NULL DEFAULT '{}',
    description TEXT NOT NULL DEFAULT '',
    attributes JSONB NOT NULL DEFAULT '{}',
    embedding vector(1536),
    source TEXT NOT NULL DEFAULT 'extracted',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, kind, name)
);
CREATE INDEX knowledge_entity_workspace_kind_idx
    ON knowledge_entity (workspace_id, kind);
CREATE INDEX knowledge_entity_embedding_idx ON knowledge_entity
    USING hnsw (embedding vector_cosine_ops);

-- 4. Relations (typed edges)
CREATE TABLE knowledge_relation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    source_id UUID NOT NULL REFERENCES knowledge_entity(id) ON DELETE CASCADE,
    target_id UUID NOT NULL REFERENCES knowledge_entity(id) ON DELETE CASCADE,
    relation TEXT NOT NULL,
    evidence_doc_id UUID REFERENCES knowledge_doc(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, source_id, relation, target_id)
);
CREATE INDEX knowledge_relation_source_idx
    ON knowledge_relation (source_id, relation);
CREATE INDEX knowledge_relation_target_idx
    ON knowledge_relation (target_id, relation);

-- 5. Chunk ↔ Entity mentions (many-to-many)
CREATE TABLE knowledge_chunk_mention (
    chunk_id UUID NOT NULL REFERENCES knowledge_chunk(id) ON DELETE CASCADE,
    entity_id UUID NOT NULL REFERENCES knowledge_entity(id) ON DELETE CASCADE,
    PRIMARY KEY (chunk_id, entity_id)
);
