DROP INDEX IF EXISTS knowledge_entity_source_doc_idx;
ALTER TABLE knowledge_entity DROP COLUMN IF EXISTS source_doc_id;
