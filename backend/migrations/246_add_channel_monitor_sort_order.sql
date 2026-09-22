-- Keep the current administrator display order when introducing manual sorting.
ALTER TABLE channel_monitors
    ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0
        CHECK (sort_order >= 0);

WITH ranked AS (
    SELECT id, ROW_NUMBER() OVER (ORDER BY id DESC) - 1 AS sort_order
    FROM channel_monitors
)
UPDATE channel_monitors AS monitor
SET sort_order = ranked.sort_order
FROM ranked
WHERE monitor.id = ranked.id;

CREATE INDEX IF NOT EXISTS idx_channel_monitors_sort_order
    ON channel_monitors (sort_order);
