# Balance Reservation Design

## Goal

Prevent standard-balance users from starting more billable gateway requests than their funded balance can cover, while preserving the existing atomic final deduction as a defense-in-depth check. Each request reserves only a proven upper bound for its own cost instead of freezing the user's entire available balance.

## Constraints

- Administrator balance adjustments and the legacy `UserRepository.DeductBalance` contract remain unchanged.
- Subscription, simple-mode, zero-cost, and unsupported-estimate requests do not reserve balance.
- Existing `users.balance` and `users.frozen_balance` remain the money ledger; no change to their meaning.
- Every reservation transition is atomic and idempotent across processes and service restarts.
- Expired reservations must be recoverable without knowing request-local memory state.
- A reservation is created only when the gateway can prove a finite upper bound that is at least the final billable amount.
- Missing, malformed, or ambiguous limits never fall back to freezing all available balance. They fail closed with a retryable billing error.
- The reservation transaction may briefly wait for the user's row lock, but it must never hold that lock while an upstream request is running.

## Data Model

Add migration `235_add_balance_reservations.sql` with table `balance_reservations`:

- `id BIGSERIAL PRIMARY KEY`
- `request_id VARCHAR(255) NOT NULL`
- `api_key_id BIGINT NOT NULL`
- `user_id BIGINT NOT NULL`
- `hold_amount NUMERIC(20,8) NOT NULL CHECK (hold_amount > 0)`
- `actual_amount NUMERIC(20,8)`
- `status VARCHAR(16) NOT NULL CHECK (status IN ('held', 'settled', 'released'))`
- `expires_at TIMESTAMPTZ NOT NULL`
- `created_at`, `updated_at` timestamps
- unique `(request_id, api_key_id)`
- index `(status, expires_at)` for recovery

The row is the durable idempotency record. A reservation is created in the same transaction as the atomic transfer `balance -> frozen_balance`. `hold_amount` is always a positive, finite, quantized estimate supplied by the gateway; `0` is invalid for new reservations and no longer means "all available balance". Settlement and release lock the row with `FOR UPDATE`, verify its state, and apply the corresponding balance/frozen-balance delta in the same transaction.

## Lifecycle

1. `ReserveUsageBalance(ctx, cmd)` validates a positive finite hold and expiry, inserts or locks the reservation row, and atomically subtracts `hold_amount` from `balance` while adding it to `frozen_balance`. Duplicate calls return the existing state without moving money twice. The user row is locked only for the short database transaction.
2. `SettleUsageBalance(ctx, cmd)` locks the reservation, accepts `actual_amount <= hold_amount`, moves the actual amount from frozen funds to spent balance, and returns the unused difference to available balance. It is a no-op for an already settled/released row.
3. `ReleaseUsageBalance(ctx, cmd)` locks the reservation, returns the full hold to available balance, and marks it released. It is a no-op for an already settled/released row.
4. `RecoverExpiredUsageBalances(ctx, limit)` locks expired `held` rows, releases each hold, and marks it released. The worker is safe to run concurrently on multiple instances.

## Upper-bound estimation

Add a request-scoped estimator that returns either a finite amount or an explicit unsupported-estimate result:

- Reuse the existing `BillingService.CalculateCostUnified` and `ModelPricingResolver` price sources. Do not copy pricing tables or multiplier rules.
- Parse protocol-specific output limits in this order: OpenAI `max_completion_tokens`, OpenAI `max_output_tokens` (Responses), `max_tokens` (Chat Completions/Anthropic), and Gemini `generationConfig.maxOutputTokens`.
- When the request omits a limit, use `Billing.ReservationMaxOutputTokens` as the uniform hard upper bound. The configuration must be positive; zero means the estimator is unavailable and the request is rejected for reservation.
- Bound input using the received request body plus any gateway-injected content that is known before forwarding. Include the highest applicable service-tier, reasoning, cache, long-context, user/group, channel, peak, and account-mapping multipliers.
- For image, video, audio, and search charges, require explicit finite counts, sizes, durations, or call limits. If the final route or unit price cannot be bounded before account selection, return unsupported-estimate rather than guessing.
- If failover or model mapping can choose multiple priced models, use the maximum price across all candidates. If the candidate set cannot be enumerated, return unsupported-estimate.
- Quantize the resulting amount to `UsageBillingMonetaryScale` and reject non-positive, NaN, infinite, or otherwise invalid results.

The estimator is run after request parsing and route metadata are available but before the billing eligibility check. The amount is carried in a request context value consumed by `CheckBillingEligibility`, which passes it to `UsageBalanceReservationCommand.HoldAmount`.

## Gateway Integration

Introduce a request-scoped reservation handle in the gateway billing service. Handlers call it after concurrency acquisition and billing eligibility checks but before forwarding. The handle is settled after a successful usage result and released on upstream error, cancellation, timeout, or any path that does not record successful billable usage. The final `UsageBillingRepository.Apply` call remains active; it must recognize a settled reservation and avoid double-deducting the reserved actual amount.

Only request paths with a deterministic upper-bound estimate use the handle. Standard balance requests with an unsupported estimate receive a retryable billing error and are not forwarded. Subscription, simple-mode, zero-cost, count-tokens, and existing batch-image flows retain their current behavior; batch image holds remain independent and are not nested with the new reservation.

## Errors and Cache

- Insufficient available balance returns the existing `service.ErrInsufficientBalance` before forwarding.
- Unsupported or unavailable estimates return a dedicated billing validation error mapped to HTTP 503/429 semantics; the error does not expose pricing internals or request contents.
- Missing user returns `service.ErrUserNotFound`.
- Settlement greater than the hold returns `ErrUsageBalanceSettlementExceedsHold` and leaves the reservation held for recovery. It must never silently skip a charge or make the balance negative.
- Every successful reserve, settle, release, or recovery invalidates the user balance cache; no Redis write is treated as authoritative.
- A short database statement/transaction timeout bounds lock waits. Upstream latency is outside the transaction and cannot serialize other users' requests.

## Verification

- Repository unit tests cover reserve, duplicate reserve, settlement refund, release, invalid settlement, and expired recovery.
- PostgreSQL integration tests cover concurrent reserve capacity, restart-safe recovery, and idempotent transitions.
- Estimator tests cover all supported protocol limit fields, the configured hard upper bound, unknown limits, model/channel/group multipliers, long-context/reasoning/cache pricing, and unsupported media routes.
- Gateway service tests verify reservation is released on failed forwarding, settled once on success, and that rejected reservations never invoke forwarding.
- A concurrency test with balance `10` and hold `2` verifies five successful reservations and an immediate sixth `ErrInsufficientBalance`; settlement of `0.8` verifies the `1.2` refund.
