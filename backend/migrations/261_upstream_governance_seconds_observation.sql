-- Second-based schedules for cheap group/rate observations.  The existing
-- minute column remains for compatibility with older API clients; all new
-- scheduling state is persisted in BIGINT seconds.
ALTER TABLE upstream_governance_sites
 ADD COLUMN IF NOT EXISTS fast_interval_seconds BIGINT,
 ADD COLUMN IF NOT EXISTS full_interval_seconds BIGINT,
 ADD COLUMN IF NOT EXISTS next_fast_observe_at TIMESTAMPTZ,
 ADD COLUMN IF NOT EXISTS last_fast_observe_at TIMESTAMPTZ,
 ADD COLUMN IF NOT EXISTS fast_observe_status TEXT NOT NULL DEFAULT 'idle',
 ADD COLUMN IF NOT EXISTS fast_observe_error TEXT NOT NULL DEFAULT '',
 ADD COLUMN IF NOT EXISTS fast_observe_reserved_at TIMESTAMPTZ,
 ADD COLUMN IF NOT EXISTS fast_observe_source_user_id BIGINT NOT NULL DEFAULT 0,
 ADD COLUMN IF NOT EXISTS fast_observe_complete BOOLEAN NOT NULL DEFAULT FALSE,
 ADD COLUMN IF NOT EXISTS fast_observe_revision BIGINT NOT NULL DEFAULT 0;

-- Existing minute values are exact in BIGINT seconds, including the largest
-- value allowed by the legacy INTEGER column.  A fast observation initially
-- follows the full collection deadline until configured explicitly.
UPDATE upstream_governance_sites
 SET full_interval_seconds = COALESCE(full_interval_seconds, interval_minutes::BIGINT * 60),
     fast_interval_seconds = COALESCE(fast_interval_seconds, interval_minutes::BIGINT * 60),
     next_fast_observe_at = COALESCE(next_fast_observe_at, next_sync_at);

ALTER TABLE upstream_governance_sites
 ALTER COLUMN fast_interval_seconds SET DEFAULT 900,
 ALTER COLUMN fast_interval_seconds SET NOT NULL,
 ALTER COLUMN full_interval_seconds SET DEFAULT 900,
 ALTER COLUMN full_interval_seconds SET NOT NULL,
 ALTER COLUMN next_fast_observe_at SET DEFAULT NOW(),
 ALTER COLUMN next_fast_observe_at SET NOT NULL;

DO $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='upstream_governance_sites_fast_interval_seconds_check') THEN
  ALTER TABLE upstream_governance_sites ADD CONSTRAINT upstream_governance_sites_fast_interval_seconds_check CHECK (fast_interval_seconds BETWEEN 1 AND 128849018820);
 END IF;
 IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='upstream_governance_sites_full_interval_seconds_check') THEN
  ALTER TABLE upstream_governance_sites ADD CONSTRAINT upstream_governance_sites_full_interval_seconds_check CHECK (full_interval_seconds BETWEEN 1 AND 128849018820);
 END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_upstream_governance_sites_fast_due
 ON upstream_governance_sites(next_fast_observe_at,id)
 WHERE enabled AND session_cipher <> '';

-- One compact row per site is enough for the fast worker.  The payload stores
-- only validated visible groups/rates and is intentionally independent from
-- full catalog snapshots/import previews.
CREATE TABLE IF NOT EXISTS upstream_governance_fast_observations (
 site_id BIGINT PRIMARY KEY REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 revision BIGINT NOT NULL DEFAULT 0 CHECK (revision >= 0),
 fingerprint TEXT NOT NULL DEFAULT '',
 source_user_id BIGINT NOT NULL DEFAULT 0,
 groups_complete BOOLEAN NOT NULL DEFAULT FALSE,
 groups JSONB NOT NULL DEFAULT '[]'::jsonb,
 observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
