# Channel monitor token-weighted cache rate

The cache card now uses summed cache reads divided by summed complete actual
input tokens, multiplied by 100. Complete input includes uncached upstream
input, cache reads and cache writes. Valid zero-hit input is included.

The sample remains the newest 24 successful streaming requests for the exact
bound group, requested model and optional account, ordered by timestamp then
ID. Here success retains the existing actual_cost > 0 criterion. It is a
request-count window, not a 24-hour window; synchronous requests do not enter.
Availability and mean first-token latency keep their separate existing rules.

Actual upstream input must be recorded; adjusted billing input is never a
substitute. Persisted upstream cache counters take precedence over legacy
cache fields. Legacy cache fields remain compatible when upstream cache
breakdown was not persisted, matching the usage table's existing semantics.
Cache creation uses the total or the sum of TTL buckets once. Conflicting
positive totals, negative measurements, missing input and ambiguous zero
input with positive billed input are excluded after choosing the newest 24.
They are not replaced by older cache hits. No measurable input produces an
unavailable metric; valid uncached input produces measured zero.

The 2026-11-28 13:43 UTC+8 server 15 snapshot illustrates the distinction:

| Sample | Previous hit-only request mean | Actual token-weighted rate | Requests with cache reads |
| --- | ---: | ---: | ---: |
| GPT Max, newest 24 streams | 47.0874% | 83.3438% | 16/24 |
| Grok Ultra, newest 24 streams | 54.4538% | 96.1324% | 24/24 |

For Grok the latest 24 requests of either response mode comprised 23
synchronous and one streaming request, with a separate raw-token weighted
rate of 44.0781%. Mixing the samples would change what the card describes.
The rate can rise or fall with real traffic; no counters are fabricated.

The user explicitly chose actual-token weighting. PostgreSQL regressions
cover unequal request sizes, uncached denominator input, upstream/billing
differences, TTL deduplication, missing/conflicting telemetry, no backfill,
and retained stream/group/model/account isolation and ordered indexes.
