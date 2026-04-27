-- Enable pgvector for semantic search on knowledge graph.
-- The dev/CI container image (pgvector/pgvector:pg17) ships this extension.
CREATE EXTENSION IF NOT EXISTS vector;
