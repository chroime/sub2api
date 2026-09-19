CREATE TABLE IF NOT EXISTS balance_precharges_archive (LIKE balance_precharges INCLUDING ALL);
CREATE TABLE IF NOT EXISTS balance_precharge_reviews_archive (LIKE balance_precharge_reviews INCLUDING ALL);
CREATE OR REPLACE VIEW balance_precharges_history AS
    SELECT * FROM balance_precharges UNION ALL SELECT * FROM balance_precharges_archive;
CREATE OR REPLACE VIEW balance_precharge_reviews_history AS
    SELECT * FROM balance_precharge_reviews UNION ALL SELECT * FROM balance_precharge_reviews_archive;
CREATE INDEX IF NOT EXISTS idx_balance_precharges_terminal_retention
    ON balance_precharges (updated_at,id) WHERE state<>'reserved';
