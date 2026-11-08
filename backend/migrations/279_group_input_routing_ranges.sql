-- Legacy rules matched estimated_input_tokens < max_input_tokens. Preserve
-- that boundary when introducing inclusive [min_input_tokens, max_input_tokens].
-- A present min_input_tokens marks a rule that already uses inclusive ranges.
-- Invalid legacy thresholds stay invalid, matching the JSON compatibility decoder.
-- Updating input_routing reuses the existing auth-cache invalidation trigger.
WITH converted AS (
    SELECT g.id,
           jsonb_set(
               g.input_routing,
               '{rules}',
               (
                   SELECT jsonb_agg(
                       CASE
                           WHEN rule ? 'min_input_tokens' THEN rule
                           ELSE rule || jsonb_build_object(
                               'min_input_tokens', 0,
                               'max_input_tokens', CASE
                                   WHEN (rule->>'max_input_tokens')::bigint BETWEEN 1 AND 10000000
                                       THEN (rule->>'max_input_tokens')::bigint - 1
                                   WHEN (rule->>'max_input_tokens')::bigint <= 0 THEN -1
                                   ELSE (rule->>'max_input_tokens')::bigint
                               END
                           )
                       END ORDER BY ordinal
                   )
                   FROM jsonb_array_elements(g.input_routing->'rules')
                       WITH ORDINALITY AS entries(rule, ordinal)
               )
           ) AS input_routing
    FROM groups AS g
    WHERE jsonb_typeof(g.input_routing->'rules') = 'array'
      AND g.input_routing->'rules' <> '[]'::jsonb
)
UPDATE groups AS g
SET input_routing = converted.input_routing
FROM converted
WHERE g.id = converted.id
  AND g.input_routing IS DISTINCT FROM converted.input_routing;
