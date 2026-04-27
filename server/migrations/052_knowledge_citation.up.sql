-- Records which chunks were injected as context for a given agent task.
-- Lets us audit "what did the agent consult?" after a task finishes.
CREATE TABLE knowledge_citation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES agent_task_queue(id) ON DELETE CASCADE,
    chunk_id UUID NOT NULL REFERENCES knowledge_chunk(id) ON DELETE CASCADE,
    doc_id UUID NOT NULL REFERENCES knowledge_doc(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    rank INT NOT NULL,
    score DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX knowledge_citation_task_idx ON knowledge_citation (task_id);
CREATE INDEX knowledge_citation_chunk_idx ON knowledge_citation (chunk_id);
