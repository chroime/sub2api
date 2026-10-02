-- Match native API-key account platforms. Retain legacy platform values and
-- stable markers; platform expansion must not recreate existing imports/keys.
ALTER TABLE upstream_governance_bindings
 DROP CONSTRAINT upstream_governance_bindings_platform_check,
 ADD CONSTRAINT upstream_governance_bindings_platform_check
 CHECK (platform IN ('openai','anthropic','gemini','antigravity','grok','kimi','zhipu','deepseek','minimax','opencode_go'));

ALTER TABLE upstream_governance_keys
 DROP CONSTRAINT upstream_governance_keys_platform_check,
 ADD CONSTRAINT upstream_governance_keys_platform_check
 CHECK (platform IN ('openai','anthropic','gemini','antigravity','grok','kimi','zhipu','deepseek','minimax','opencode_go'));
