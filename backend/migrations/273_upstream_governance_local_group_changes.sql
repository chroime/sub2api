-- Durable local-group mutation events. These events are intentionally kept
-- separate from the site-scoped upstream_governance_change_notifications
-- queue: local groups have no upstream site_id to reference.
CREATE TABLE IF NOT EXISTS upstream_governance_local_group_changes (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('created', 'updated', 'deleted')),
    before_value JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_value JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_upstream_governance_local_group_changes_due
    ON upstream_governance_local_group_changes(status, next_attempt_at, id)
    WHERE status IN ('pending', 'sending');
CREATE INDEX IF NOT EXISTS idx_upstream_governance_local_group_changes_group
    ON upstream_governance_local_group_changes(group_id, created_at DESC, id DESC);

-- Capture all writes at the shared database mutation layer. This covers admin
-- service writes, repository maintenance jobs, and direct SQL migrations while
-- preserving transaction atomicity with the groups mutation.
CREATE OR REPLACE FUNCTION record_upstream_governance_local_group_change()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    event_kind TEXT;
    before_payload JSONB := '{}'::jsonb;
    after_payload JSONB := '{}'::jsonb;
    group_id_value BIGINT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        event_kind := 'created';
        group_id_value := NEW.id;
        after_payload := to_jsonb(NEW);
    ELSIF TG_OP = 'DELETE' THEN
        event_kind := 'deleted';
        group_id_value := OLD.id;
        before_payload := to_jsonb(OLD);
    ELSE
        -- Ent's soft-delete interceptor issues a DELETE for normal group
        -- removal. Keep this branch for explicit deleted_at updates from
        -- maintenance scripts and classify those as deletion events.
        IF OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL THEN
            event_kind := 'deleted';
        ELSIF (to_jsonb(OLD) - 'updated_at') IS NOT DISTINCT FROM (to_jsonb(NEW) - 'updated_at') THEN
            -- Ignore timestamp-only writes and no-op updates. This prevents
            -- the monitor from producing noise when a cache refresh touches
            -- updated_at without changing group configuration.
            RETURN NEW;
        ELSE
            event_kind := 'updated';
        END IF;
        group_id_value := NEW.id;
        before_payload := to_jsonb(OLD);
        after_payload := to_jsonb(NEW);
    END IF;

    INSERT INTO upstream_governance_local_group_changes
        (group_id, kind, before_value, after_value)
    VALUES (group_id_value, event_kind, before_payload, after_payload);

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS upstream_governance_local_group_change_trigger ON groups;
CREATE TRIGGER upstream_governance_local_group_change_trigger
AFTER INSERT OR UPDATE OR DELETE ON groups
FOR EACH ROW
EXECUTE FUNCTION record_upstream_governance_local_group_change();
