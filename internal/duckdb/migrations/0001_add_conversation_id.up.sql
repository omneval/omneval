ALTER TABLE spans ADD COLUMN IF NOT EXISTS conversation_id VARCHAR;
CREATE INDEX IF NOT EXISTS idx_spans_conversation ON spans (project_id, conversation_id);
