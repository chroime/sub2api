# Skip Zero-Token, Zero-Cost OpenAI Usage Logs

## Problem

An OpenAI-compatible request can finish or fail without any reported tokens or
calculated charge. The gateway currently records a usage row whenever it receives
an `OpenAIForwardResult`, so these requests appear as `0 token / 0 cost` entries in
the user and admin usage tables. Forward failures such as `stream_timeout` are
already recorded in the structured system log.

The observed example is usage-log row 101. It has a synthetic first response of
690 ms, a duration of 164981 ms, no upstream request ID, zero usage, and zero cost.
The matching application log ends with `upstream response failed: stream_timeout`.

## Decision

Do not persist an OpenAI-compatible usage row when both of the following are true:

1. All token counters are zero, including input, output, cache creation, cache read,
   image input, and image output tokens.
2. Every final fee figure is zero: calculated upstream cost (`total_cost`),
   customer charge (`actual_cost`), and account-stat cost when present.

The rule is independent of whether forwarding succeeded or failed. Failure details
remain available in the normal structured system/access logs. No synthetic
acknowledgement, latency, scheduling, response, or account-health behavior changes.

## Preserved Behavior

- Successful and failed requests follow the same zero-token, zero-cost rule.
- Requests with any nonzero token counter continue to be stored, even when pricing
  is missing and both costs are zero.
- Requests with nonzero calculated cost continue to be stored even when all token
  counters are zero, preserving per-request, image, video, search, audio, and other
  fixed-price billing.
- Non-token usage whose final calculated costs are both zero is not stored. This is
  the explicit consequence of using only the requested token-and-cost predicate.
- The OpenAI 403 counter is still reset before evaluating the storage predicate, so
  an otherwise successful zero-usage result preserves account-health behavior.
- Cyber-policy requests use the same predicate. Their rejection details remain in
  structured security/system logs even when a zero-token, zero-cost usage row is
  omitted.

## Implementation

`OpenAIGatewayService.RecordUsage` keeps its existing validation, provider-health
reset, result normalization, and cost calculation. After constructing the complete
`UsageLog` and applying account-stat pricing, but before billing or persistence, it
evaluates a small predicate over every token field and every final fee figure. A
matching result returns successfully without invoking billing or writing the usage
row. This placement is intentional: it keeps fixed-price and account-stat usage
whose cost can only be known after pricing resolution, and evaluates the same
normalized token and cost values that would otherwise be persisted.

A structured debug log records that a zero-token, zero-cost usage row was skipped,
using only request/account identifiers and no request content or credentials.

## Tests

Service tests cover:

- successful zero-token plus zero-cost result: no usage insert and no billing;
- failed zero-token plus zero-cost result: no usage insert and no billing;
- any nonzero token field plus zero cost: record remains;
- zero tokens plus nonzero `total_cost`: record remains;
- zero tokens plus nonzero `actual_cost`: record remains;
- zero tokens plus nonzero account-stat cost: record remains;
- existing OpenAI provider-health reset behavior remains;
- zero-token, zero-cost cyber-policy requests are omitted from usage storage while
  their structured security logs remain.

Existing synthetic-first-response, partial-usage, media, fixed-price, billing, and
handler tests must continue to pass.

## Existing Data

After deploying and verifying the behavior, delete only existing OpenAI-compatible
rows that match the same all-token-fields-zero and all-fee-figures-zero predicate.
The cleanup does not touch rows with any token field or fee figure nonzero.
Historical cleanup is reported separately.
