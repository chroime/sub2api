# Skip Empty Failed OpenAI Usage Logs

## Problem

An OpenAI-compatible streaming request can receive the gateway's synthetic SSE
acknowledgement and then fail before the upstream reports any usage. The forwarder
returns a partial `OpenAIForwardResult`, so the handler currently records a usage
row even when every token, media unit, search call, audio unit, and calculated cost
is zero. The row looks like a successful `0 token / 0 cost` request in the user and
admin usage tables, while the actual `stream_timeout` is already recorded in the
system log.

The observed example is usage-log row 101. It has a synthetic first response of
690 ms, a duration of 164981 ms, no upstream request ID, zero usage, and zero cost.
The matching application log ends with `upstream response failed: stream_timeout`.

## Decision

Do not persist a usage row when all of the following are true:

1. The OpenAI-compatible forward operation ended with an error.
2. All token counters are zero, including input, output, cache creation, cache read,
   image input, and image output tokens.
3. All non-token usage evidence is absent: image count, video count, OpenAI alpha
   web-search calls, Grok search calls, and audio usage.
4. Both the calculated upstream cost and customer cost are zero.

The request failure remains available in the normal structured system/access logs.
No synthetic acknowledgement, latency, scheduling, or response behavior changes.

## Preserved Behavior

- Successful requests with zero reported usage continue to be stored. This retains
  the existing success/audit semantics and OpenAI 403-counter reset behavior.
- Failed partial streams with any token usage continue to be stored and billed.
- Failed media, video, search, and audio operations with usage evidence continue to
  be stored, even when a pricing rule is missing and their calculated cost is zero.
- Requests with nonzero calculated cost continue to be stored even when their token
  counters are zero, preserving per-request and other fixed-price billing.
- Requests with nonzero tokens but zero cost continue to be stored so missing-price
  conditions remain visible and are not silently discarded.
- Cyber-policy usage recording is unchanged because it is a separate explicit audit
  path and does not represent the streaming forward-error submission addressed here.

## Implementation

Add a `ForwardFailed` flag to `OpenAIRecordUsageInput`. The Responses,
Chat Completions, and Anthropic Messages compatibility handlers pass `true` only
when they submit a partial result returned alongside a terminal forwarding error.

`OpenAIGatewayService.RecordUsage` keeps its existing validation, provider-health
reset, result normalization, and cost calculation. Immediately after final cost
resolution, it evaluates a small predicate over the result and calculated cost. A
failed, completely empty result returns successfully before constructing or writing
the usage row and before invoking billing. This placement is intentional: it keeps
fixed-price usage whose cost can only be known after pricing resolution.

A structured debug log records that an empty failed usage row was skipped, using
only request/account identifiers and no request content or credentials.

## Tests

Service tests cover:

- failed plus completely empty result: no usage insert and no billing application;
- successful plus completely empty result: existing usage row remains;
- failed plus token usage: record remains;
- failed plus each non-token usage category: record remains;
- failed plus nonzero calculated cost: record remains;
- nonzero tokens plus zero price: record remains.

Handler contract tests verify that all three OpenAI-compatible HTTP streaming entry
points mark error-path partial usage as failed and do not mark success-path usage as
failed. Existing synthetic-first-response and partial-usage tests must continue to
pass.

## Existing Data

After deploying and verifying the behavior, delete only existing rows that match the
same complete-empty predicate. The cleanup does not touch rows with any tokens,
media, search, audio-derived cost, or nonzero cost. Because the current schema does
not store a success/failure flag, historical cleanup is limited to the exact empty
shape and is reported separately.
