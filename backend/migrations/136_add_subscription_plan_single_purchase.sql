ALTER TABLE subscription_plans
ADD COLUMN IF NOT EXISTS single_purchase BOOLEAN NOT NULL DEFAULT false;
