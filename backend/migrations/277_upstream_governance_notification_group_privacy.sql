-- Preserve group identity in the durable notification outbox. NULL means a
-- legacy row could not identify its group; do not guess from rendered content.
ALTER TABLE upstream_governance_change_notifications
    ADD COLUMN group_resource TEXT,
    ADD COLUMN local_group_id BIGINT CHECK (local_group_id > 0);

-- Fast collection keeps only its latest catalog. Short visibility audit events
-- retain the evidence needed to suppress older messages after a private group
-- becomes public again, without retaining credentials or complete snapshots.
CREATE INDEX IF NOT EXISTS idx_upstream_governance_group_visibility
    ON upstream_governance_events(site_id, resource, created_at DESC)
    WHERE kind = 'group_visibility_changed';

-- Removing a binding must not erase the privacy evidence of queued notices.
-- No foreign keys: a site or binding can be removed while these cutoffs still
-- protect older local notifications. Future public notices remain eligible.
CREATE TABLE upstream_governance_notification_visibility_barriers (
    scope TEXT NOT NULL CHECK (scope IN ('local', 'remote')),
    site_id BIGINT NOT NULL DEFAULT 0,
    resource TEXT NOT NULL DEFAULT '',
    local_group_id BIGINT NOT NULL DEFAULT 0,
    invalidated_before TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope, site_id, resource, local_group_id),
    CHECK ((scope = 'local' AND site_id = 0 AND resource = '' AND local_group_id > 0)
        OR (scope = 'remote' AND site_id > 0 AND resource <> '' AND local_group_id = 0))
);

CREATE FUNCTION upstream_governance_remote_privacy_cutoff(
    source_site_id BIGINT, source_resource TEXT, detached_at TIMESTAMPTZ
) RETURNS TIMESTAMPTZ LANGUAGE sql AS $$
WITH facts AS (
    SELECT g.value AS payload, s.created_at AS observed_at
    FROM upstream_governance_snapshots s
    CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN jsonb_typeof(s.catalog->'groups') = 'array' THEN s.catalog->'groups' ELSE '[]'::jsonb END
    ) g(value)
    WHERE s.site_id = source_site_id AND g.value->>'id' = source_resource
    UNION ALL
    SELECT g.value, f.observed_at
    FROM upstream_governance_fast_observations f
    CROSS JOIN LATERAL jsonb_array_elements(
        CASE WHEN jsonb_typeof(f.groups) = 'array' THEN f.groups ELSE '[]'::jsonb END
    ) g(value)
    WHERE f.site_id = source_site_id AND g.value->>'id' = source_resource
    UNION ALL
    SELECT jsonb_build_object('is_exclusive', e.after_value = 'true'), e.created_at
    FROM upstream_governance_events e
    WHERE e.site_id = source_site_id AND e.resource = source_resource
        AND e.kind = 'group_visibility_changed' AND e.after_value IN ('true', 'false')
), latest AS (
    SELECT payload FROM facts
    ORDER BY observed_at DESC, (payload->'is_exclusive' = 'true'::jsonb) DESC NULLS LAST LIMIT 1
), private_times AS (
    SELECT observed_at FROM facts WHERE payload->'is_exclusive' = 'true'::jsonb
    UNION ALL
    SELECT created_at FROM upstream_governance_events
    WHERE site_id = source_site_id AND resource = source_resource
        AND kind = 'group_visibility_changed' AND (before_value = 'true' OR after_value = 'true')
)
SELECT CASE
    WHEN (SELECT payload FROM latest) IS NULL
        OR jsonb_typeof((SELECT payload FROM latest)) <> 'object'
        OR ((SELECT payload FROM latest) ? 'is_exclusive'
            AND (SELECT payload->'is_exclusive' FROM latest) IS DISTINCT FROM 'false'::jsonb)
    THEN detached_at
    ELSE (SELECT MAX(observed_at) FROM private_times)
END;
$$;

CREATE FUNCTION record_upstream_governance_binding_notification_barrier()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    detached_at TIMESTAMPTZ := clock_timestamp();
    source_changed BOOLEAN;
    new_targets BIGINT[] := ARRAY[]::BIGINT[];
    old_target BIGINT;
    source_cutoff TIMESTAMPTZ;
    source_checked BOOLEAN := false;
    target_cutoff TIMESTAMPTZ;
    target_exclusive BOOLEAN;
BEGIN
    IF TG_OP = 'DELETE' THEN
        source_changed := true;
    ELSE
        source_changed := NEW.site_id IS DISTINCT FROM OLD.site_id
            OR NEW.remote_group_id IS DISTINCT FROM OLD.remote_group_id;
        SELECT ARRAY_AGG(DISTINCT target.value::BIGINT) INTO new_targets
        FROM jsonb_array_elements_text(jsonb_build_array(NEW.local_group_id)
            || COALESCE(NEW.local_group_ids, '[]'::jsonb)) target(value);
    END IF;

    FOR old_target IN
        SELECT DISTINCT target.value::BIGINT
        FROM jsonb_array_elements_text(jsonb_build_array(OLD.local_group_id)
            || COALESCE(OLD.local_group_ids, '[]'::jsonb)) target(value)
    LOOP
        IF NOT source_changed AND old_target = ANY(new_targets) THEN
            CONTINUE;
        END IF;
        IF NOT source_checked THEN
            source_cutoff := upstream_governance_remote_privacy_cutoff(OLD.site_id, OLD.remote_group_id, detached_at);
            source_checked := true;
        END IF;
        IF source_cutoff IS NOT NULL THEN
            INSERT INTO upstream_governance_notification_visibility_barriers(scope, local_group_id, invalidated_before)
            VALUES ('local', old_target, source_cutoff)
            ON CONFLICT (scope, site_id, resource, local_group_id) DO UPDATE
                SET invalidated_before = GREATEST(upstream_governance_notification_visibility_barriers.invalidated_before, EXCLUDED.invalidated_before);
        END IF;

        -- A removed private local target also invalidates its remote group's
        -- older notices, even if the current binding list is now public.
        SELECT is_exclusive INTO target_exclusive FROM groups WHERE id = old_target AND deleted_at IS NULL;
        IF target_exclusive IS DISTINCT FROM false THEN
            target_cutoff := detached_at;
        ELSE
            SELECT MAX(created_at) INTO target_cutoff
            FROM upstream_governance_local_group_changes
            WHERE group_id = old_target AND (before_value->'is_exclusive' = 'true'::jsonb
                OR before_value->'IsExclusive' = 'true'::jsonb OR after_value->'is_exclusive' = 'true'::jsonb
                OR after_value->'IsExclusive' = 'true'::jsonb);
        END IF;
        IF target_cutoff IS NOT NULL THEN
            INSERT INTO upstream_governance_notification_visibility_barriers(scope, site_id, resource, invalidated_before)
            VALUES ('remote', OLD.site_id, OLD.remote_group_id, target_cutoff)
            ON CONFLICT (scope, site_id, resource, local_group_id) DO UPDATE
                SET invalidated_before = GREATEST(upstream_governance_notification_visibility_barriers.invalidated_before, EXCLUDED.invalidated_before);
        END IF;
    END LOOP;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER upstream_governance_binding_notification_barrier_trigger
BEFORE DELETE OR UPDATE ON upstream_governance_bindings
FOR EACH ROW EXECUTE FUNCTION record_upstream_governance_binding_notification_barrier();
