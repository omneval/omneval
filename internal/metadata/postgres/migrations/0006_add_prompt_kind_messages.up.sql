-- Issue #232/#241: Add chat-type (multi-message, role-tagged) PromptVersion support.
-- Adds a `kind` discriminator (default 'text') and a JSONB `messages` column
-- to store ordered {role, content} messages for chat-type prompts.

ALTER TABLE prompt_versions
  ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'text',
  ADD COLUMN IF NOT EXISTS messages JSONB;