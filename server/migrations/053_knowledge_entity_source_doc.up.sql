-- Track which doc an entity was extracted from. Enables automatic cascade
-- delete of stale entities when a doc is deleted, and targeted cleanup of
-- entities from the previous version of a doc on reindex.
--
-- Nullable: existing entities keep source_doc_id = NULL and are unaffected.
-- ON DELETE CASCADE: deleting a doc removes its exclusively-owned entities
-- at the DB level without any application-layer code.
ALTER TABLE knowledge_entity
    ADD COLUMN source_doc_id UUID REFERENCES knowledge_doc(id) ON DELETE CASCADE;

CREATE INDEX knowledge_entity_source_doc_idx
    ON knowledge_entity (source_doc_id)
    WHERE source_doc_id IS NOT NULL;
