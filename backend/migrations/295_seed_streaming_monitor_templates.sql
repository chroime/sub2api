-- Reusable templates selected through the existing request-template picker.
-- Preserve user edits if this seed is replayed.
INSERT INTO channel_monitor_request_templates (
    name, provider, api_mode, description, extra_headers, body_override_mode, body_override
)
SELECT
    '低 Token 流式可用性探测模板' || CASE WHEN api_mode = 'responses' THEN '（Responses）' ELSE '' END,
    provider,
    api_mode,
    '流式可用性探测，输出上限 16 Token；沿用监控配置的模型及随机算术校验。',
    '{}'::jsonb,
    'merge',
    jsonb_build_object('stream', TRUE, token_key, 16)
FROM (VALUES
    ('openai', 'chat_completions', 'max_tokens'),
    ('openai', 'responses', 'max_output_tokens'),
    ('anthropic', 'chat_completions', 'max_tokens'),
    ('grok', 'chat_completions', 'max_tokens'),
    ('kimi', 'chat_completions', 'max_tokens'),
    ('zhipu', 'chat_completions', 'max_tokens'),
    ('deepseek', 'chat_completions', 'max_tokens'),
    ('minimax', 'chat_completions', 'max_tokens')
) AS preset(provider, api_mode, token_key)
ON CONFLICT (provider, name) DO NOTHING;
