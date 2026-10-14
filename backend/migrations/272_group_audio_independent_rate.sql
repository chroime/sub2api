-- 语音独立计费倍率：默认关闭，倍率默认 1.0，保持已有分组账单不变。
ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS audio_rate_independent BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS audio_rate_multiplier DECIMAL(20,8) NOT NULL DEFAULT 1.0;
