ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS local_api_key_id BIGINT,
    ADD COLUMN IF NOT EXISTS group_id BIGINT,
    ADD COLUMN IF NOT EXISTS group_binding_mode VARCHAR(16) NOT NULL DEFAULT 'legacy',
    ADD COLUMN IF NOT EXISTS group_binding_changed_at TIMESTAMPTZ;

-- Local credentials are encrypted and must be identified by the application.
-- Preserve a unique existing association while that one-time conversion runs.
WITH matches AS (
    SELECT m.id, MIN(g.id) AS group_id
    FROM channel_monitors m
    JOIN groups g ON g.name = m.group_name AND g.platform = m.provider
        AND g.deleted_at IS NULL AND g.status = 'active'
    WHERE m.group_binding_mode = 'legacy' AND m.group_id IS NULL
        AND m.group_name <> ''
    GROUP BY m.id HAVING COUNT(*) = 1
)
UPDATE channel_monitors m SET group_id = matches.group_id
FROM matches WHERE m.id = matches.id;

CREATE INDEX IF NOT EXISTS channelmonitor_local_api_key_id ON channel_monitors (local_api_key_id);
CREATE INDEX IF NOT EXISTS channelmonitor_group_id ON channel_monitors (group_id);

COMMENT ON COLUMN channel_monitors.local_api_key_id IS 'Local probe credential identity; retained when a key is deleted so association fails closed';
COMMENT ON COLUMN channel_monitors.group_id IS 'Explicit manual group or last resolved current group of local_api_key_id';
COMMENT ON COLUMN channel_monitors.group_binding_changed_at IS 'Current association history boundary; older probe records remain stored';
