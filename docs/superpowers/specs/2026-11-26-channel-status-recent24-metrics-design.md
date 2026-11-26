# Channel Status Recent-24 Metrics Design

## Goal

Make the V1 channel status card metrics describe recent activity consistently: availability uses the latest 24 displayed monitor checks, while cache hit rate and first-token time use recent successful streaming requests. Remove the 7/15/30-day selector from the card-page header.

## Current Behavior

- The card timeline displays up to 24 recent probe checks.
- Availability is selected from 7-, 15-, or 30-day probe aggregates.
- Cache hit rate and first-token time already use the latest 24 successful streaming usage records, but cache rate currently includes records with no cache-read tokens in its mean.
- The header selector changes the availability period and triggers detail requests for longer periods.

## Proposed Behavior

- Compute the card availability percentage from the same latest 24 probe points that are rendered in its timeline. `operational` and `degraded` count as available; `failed` and `error` count as unavailable. Empty timeline placeholders are excluded from the denominator. When there are no probe points, show the existing empty value.
- Keep request metrics bounded to the latest 24 successful streamed usage records for the matching group, model, and account scope. Compute each request's hit percentage as `cache_read_tokens / (input_tokens + cache_creation_tokens + cache_read_tokens) * 100`, then average only records where `cache_read_tokens > 0`. If none of the sampled records has cache reads, show the existing empty value rather than zero.
- Keep first-token averaging over the sampled 24 streamed requests, ignoring missing and negative measurements.
- Remove only the V1 header's 7/15/30-day selector and the page state/data fetching used solely by that selector. Keep manual refresh, auto-refresh, and the monitor detail dialog's existing historical data.
- Keep the existing analytics response shape to avoid an unnecessary public API contract change; the V1 card reads the fixed recent sample rather than presenting time-range choices.

## Implementation Boundary

Use the monitor timeline already returned by the list endpoint to calculate availability, so the percentage and visible bars share one sample. Apply the cache-read eligibility filter after selecting the latest 24 successful streaming records so an older cache-bearing request cannot displace a newer streamed request with no cache. Leave V2 time-range filters and long-term monitor aggregates unchanged.

## Verification

- Frontend unit tests cover availability using only the latest 24 timeline points, the operational/degraded availability rule, failed/error handling, and empty points.
- Repository PostgreSQL tests prove that cache rate excludes no-cache requests from the average without changing which 24 streaming records form the sample; first-token values continue to use that full sample.
- Component tests confirm the V1 header has no 7/15/30 selector and manual/automatic refresh controls remain available.
