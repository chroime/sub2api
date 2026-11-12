-- Identity is optional display metadata. Existing groups stay unconfigured;
-- no identity is inferred from their names, platforms or exclusive status.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS identity JSONB NOT NULL DEFAULT '{}'::jsonb;

COMMENT ON COLUMN groups.identity IS 'Optional group display identity: label and color; empty object hides the identity';
