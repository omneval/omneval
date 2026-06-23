-- Issue #232/#241: Rollback chat-type PromptVersion support.
ALTER TABLE prompt_versions
  DROP COLUMN IF EXISTS kind,
  DROP COLUMN IF EXISTS messages;