ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS streaming_ack_ms INTEGER;

COMMENT ON COLUMN usage_logs.streaming_ack_ms IS
    'Server-side SSE comment ACK flush latency; independent of real first_token_ms';
