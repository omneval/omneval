-- Issue #143: allow a user-supplied display name on any API key (project or
-- service scoped). Service keys previously reused service_name for display;
-- project keys had no way to be distinguished from one another.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS name TEXT;

-- Backfill existing unnamed keys with a label derived from their creation
-- date and key ID suffix so old keys aren't all identical-looking
-- (e.g. "Project Key (2026-05-01, ...ab12)").
UPDATE api_keys
SET name = 'Project Key (' || to_char(created_at, 'YYYY-MM-DD') || ', ...' || right(key_id, 4) || ')'
WHERE name IS NULL AND kind = 'project';
