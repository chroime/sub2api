-- Per-group customer billing token adjustments. Existing groups retain the
-- previous behavior because both multipliers default to 1.0.
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS billing_input_token_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS billing_output_token_multiplier DECIMAL(10,4) NOT NULL DEFAULT 1.0;

COMMENT ON COLUMN groups.billing_input_token_multiplier IS
    '下游计费输入 Token 修正倍率；1.0 表示沿用上游/网关计量';
COMMENT ON COLUMN groups.billing_output_token_multiplier IS
    '下游计费输出 Token 修正倍率；1.0 表示沿用上游/网关计量';

-- Token billing adjustments affect the API-key scheduling/billing snapshot.
-- Keep the durable invalidation backstop aligned with the latest trigger body.
CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_group_id BIGINT;
BEGIN
    target_group_id := OLD.id;
    IF TG_OP = 'UPDATE'
       AND OLD.status IS NOT DISTINCT FROM NEW.status
       AND OLD.is_exclusive IS NOT DISTINCT FROM NEW.is_exclusive
       AND OLD.allow_image_generation IS NOT DISTINCT FROM NEW.allow_image_generation
       AND OLD.platform IS NOT DISTINCT FROM NEW.platform
       AND OLD.subscription_type IS NOT DISTINCT FROM NEW.subscription_type
       AND OLD.rate_multiplier IS NOT DISTINCT FROM NEW.rate_multiplier
       AND OLD.billing_input_token_multiplier IS NOT DISTINCT FROM NEW.billing_input_token_multiplier
       AND OLD.billing_output_token_multiplier IS NOT DISTINCT FROM NEW.billing_output_token_multiplier
       AND OLD.peak_rate_enabled IS NOT DISTINCT FROM NEW.peak_rate_enabled
       AND OLD.peak_start IS NOT DISTINCT FROM NEW.peak_start
       AND OLD.peak_end IS NOT DISTINCT FROM NEW.peak_end
       AND OLD.peak_rate_multiplier IS NOT DISTINCT FROM NEW.peak_rate_multiplier
       AND OLD.profit_control_enabled IS NOT DISTINCT FROM NEW.profit_control_enabled
       AND OLD.profit_min_margin IS NOT DISTINCT FROM NEW.profit_min_margin
       AND OLD.profit_safety_buffer IS NOT DISTINCT FROM NEW.profit_safety_buffer
       AND OLD.streaming_ack_enabled IS NOT DISTINCT FROM NEW.streaming_ack_enabled
       AND OLD.deleted_at IS NOT DISTINCT FROM NEW.deleted_at THEN
        RETURN NEW;
    END IF;

    INSERT INTO auth_cache_invalidation_outbox (cache_key)
    SELECT encode(sha256(convert_to(k.key, 'UTF8')), 'hex')
    FROM api_keys AS k
    WHERE k.group_id = target_group_id
      AND k.deleted_at IS NULL
      AND k.key <> '';
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
