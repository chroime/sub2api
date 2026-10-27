-- Percentage thresholds may be smaller than the ledger's eight decimal places.
-- Preserve the value used by the trigger instead of rounding it to zero.
ALTER TABLE user_balance_notification_records ALTER COLUMN threshold TYPE NUMERIC;
