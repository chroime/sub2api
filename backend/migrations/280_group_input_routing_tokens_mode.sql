-- Preserve existing interval rules when adding an explicit all/range condition.
-- Only absent mode fields receive a default; existing data and rule order stay.
WITH converted AS (
    SELECT g.id,
           jsonb_set(
               g.input_routing,
               '{rules}',
               (
                   SELECT jsonb_agg(
                       CASE
                           WHEN rule ? 'input_tokens_mode' THEN rule
                           ELSE rule || jsonb_build_object('input_tokens_mode', 'range')
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
