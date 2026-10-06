-- Local-only recharge policies and simulations. This table is not a payment
-- ledger: no funds, external orders or financial reservations are created here.
CREATE TABLE upstream_governance_recharge_plans (
 site_id BIGINT PRIMARY KEY REFERENCES upstream_governance_sites(id) ON DELETE CASCADE,
 version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
 policy JSONB NOT NULL CHECK (jsonb_typeof(policy)='object' AND COALESCE(policy->>'mode' IN ('disabled','plan_only'),FALSE)),
 state JSONB NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(state)='object'),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
