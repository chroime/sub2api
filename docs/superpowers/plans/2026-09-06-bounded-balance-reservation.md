# Bounded Balance Reservation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent standard-balance users from starting more billable gateway work than their funded balance can cover by reserving only a proven per-request maximum, without freezing the whole balance or allowing negative balances.

**Architecture:** Keep PostgreSQL `users.balance`, `users.frozen_balance`, and the existing `balance_reservations` lifecycle as the source of truth. Parse each supported request's bounded input/output cost before forwarding, pass the positive quantized amount through request context, and let `BillingCacheService.CheckBillingEligibility` perform a short atomic reservation. Successful usage settles the reservation, failed/cancelled requests release it, and unsupported estimates fail closed instead of using the old full-balance fallback.

**Tech Stack:** Go, PostgreSQL, `database/sql`, existing `BillingService.CalculateCostUnified`, `ModelPricingResolver`, Gin handlers, sqlmock, and PostgreSQL integration tests.

**Spec:** `docs/superpowers/specs/2026-09-05-balance-reservation-design.md`

## Global Constraints

- A new reservation must have a finite, positive, `UsageBillingMonetaryScale`-quantized `HoldAmount`; zero no longer means “freeze all available balance”.
- Unknown or non-enumerable pricing/limits fail closed with a dedicated retryable billing error; never guess and never freeze all available funds.
- Reservation database transactions may briefly lock a user row but cannot span upstream I/O.
- Subscription, simple-mode, zero-cost, batch-image, count-tokens, and other unsupported paths retain their existing billing behavior unless a bounded estimate is explicitly implemented.
- PostgreSQL remains authoritative; Redis balance writes are cache invalidation only.
- Existing final `UsageBillingRepository.Apply` and legacy administrator balance APIs remain non-negative and source-compatible.

---

### Task 1: Define configuration and reservation-context contracts

**Files:**
- Modify: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`
- Modify: `backend/internal/service/usage_billing_reservation.go`
- Test: `backend/internal/service/usage_billing_reservation_test.go`
- Modify: `backend/internal/pkg/ctxkey/ctxkey.go` (or the existing context-key file)

**Interfaces:**
- Add `BillingConfig.ReservationMaxOutputTokens int` with `mapstructure:"reservation_max_output_tokens"`.
- Add `WithUsageBalanceReservationHold(context.Context, float64) context.Context` and `UsageBalanceReservationHoldFromContext(context.Context) (float64, bool)`.
- Add `ErrUsageBalanceReservationEstimateUnavailable` as the public service sentinel for fail-closed estimation.

- [ ] **Step 1: Write failing tests** for positive/zero configuration validation and context round-trip/rejection of zero, negative, NaN, and Inf amounts.
- [ ] **Step 2: Run the focused config/service tests** and confirm they fail because the field/helpers do not exist.
- [ ] **Step 3: Implement the field, validation, sentinel, and context helpers** using the existing config validation and context-key conventions. A zero configuration is accepted for backward-compatible startup but makes estimation unavailable; a negative value is a configuration error.
- [ ] **Step 4: Run the focused tests** and confirm they pass.
- [ ] **Step 5: Commit** with `feat: add bounded reservation configuration contracts`.

### Task 2: Make repository reservation semantics require an explicit positive hold

**Files:**
- Modify: `backend/internal/service/usage_billing_reservation.go`
- Modify: `backend/internal/repository/usage_billing_repo.go`
- Modify: `backend/internal/repository/usage_billing_repo_unit_test.go`
- Modify: `backend/internal/repository/usage_billing_repo_integration_test.go`

**Interfaces:**
- `UsageBalanceReservationCommand.Validate` rejects `HoldAmount <= 0` for new reservations while still allowing settlement/release commands to carry zero actual amount.
- `ReserveUsageBalance` returns `ErrUsageBalanceReservationInvalidAmount` for missing/invalid holds and never performs an all-balance update.

- [ ] **Step 1: Add failing sqlmock/integration assertions** for zero-hold reserve rejection, explicit hold capacity (balance 10, hold 2 gives five successes and a sixth `ErrInsufficientBalance`), refund of 1.2 after actual 0.8, over-hold settlement with no balance mutation, duplicate idempotency, release, and expiry recovery.
- [ ] **Step 2: Run the focused repository tests** and confirm the zero-hold test fails under the current “all available” behavior.
- [ ] **Step 3: Update validation and repository SQL** to require a positive finite hold, preserve row-lock/transaction boundaries, keep the corrected `Scan(&balance)` behavior, and leave existing settlement/release/`Apply` idempotency intact.
- [ ] **Step 4: Run repository unit and integration tests**, including the concurrent-capacity test, and confirm no negative balance is possible.
- [ ] **Step 5: Commit** with `fix: require explicit balance reservation holds`.

### Task 3: Parse protocol output limits and estimate a bounded maximum cost

**Files:**
- Create: `backend/internal/service/usage_billing_reservation_estimator.go`
- Test: `backend/internal/service/usage_billing_reservation_estimator_test.go`
- Modify: `backend/internal/service/gateway_request.go`
- Modify: `backend/internal/service/billing_service.go` only when a small reusable input helper is required

**Interfaces:**
- Extend `ParsedRequest` with the normalized output limit and presence metadata needed by the estimator without breaking existing callers.
- Add `EstimateUsageBalanceReservation(ctx context.Context, request *ParsedRequest, billing *BillingService, resolver *ModelPricingResolver, metadata ReservationEstimateMetadata) (float64, error)` or an equivalent service-owned estimator with explicit dependencies.
- The estimator must use `CalculateCostUnified`, existing resolver prices/multipliers, request-body token counts, and the configured hard output cap.

- [ ] **Step 1: Write failing estimator tests** for OpenAI `max_completion_tokens`, Responses `max_output_tokens`, Chat/Anthropic `max_tokens`, Gemini `generationConfig.maxOutputTokens`, configured fallback cap, malformed/missing cap, quantization, invalid numeric results, service-tier/reasoning/cache/long-context multipliers, and unenumerable media/failover pricing.
- [ ] **Step 2: Run the estimator tests** and confirm failure because the normalized field and estimator are absent.
- [ ] **Step 3: Extend request parsing** to accept the protocol-specific fields in deterministic precedence order, preserve `MaxTokens` compatibility for existing transforms, and distinguish an omitted limit from an explicit invalid limit.
- [ ] **Step 4: Implement the estimator** around existing pricing APIs. Bound all known input content, use the explicit output cap or positive configured hard cap, select the maximum over enumerable mapped/channel prices, require finite media counts/sizes/durations, quantize to eight decimals, and return `ErrUsageBalanceReservationEstimateUnavailable` for unsupported cases.
- [ ] **Step 5: Run estimator and existing gateway-request tests** and confirm all pass.
- [ ] **Step 6: Commit** with `feat: estimate bounded request reservation cost`.

### Task 4: Replace full-balance preflight with context-provided reservation

**Files:**
- Modify: `backend/internal/service/billing_cache_service.go`
- Modify: `backend/internal/service/usage_billing_reservation.go`
- Test: `backend/internal/service/billing_cache_service_balance_test.go`
- Test: `backend/internal/service/billing_cache_service_reservation_test.go`

**Interfaces:**
- `CheckBillingEligibility` reads the positive hold from context and passes it to `UsageBalanceReservationCommand.HoldAmount`.
- Standard-balance requests with a reservation repository but no valid hold return `ErrUsageBalanceReservationEstimateUnavailable`; subscription/simple/zero-cost paths continue without a reservation.

- [ ] **Step 1: Add failing service tests** for passing the exact hold, rejecting missing/invalid holds without calling the repository, preserving subscription/simple behavior, and releasing a held request on cancellation.
- [ ] **Step 2: Run focused service tests** and verify current behavior freezes the whole balance or allows an unreserved request.
- [ ] **Step 3: Implement context-driven hold handling** and remove the zero-hold full-balance path from the gateway preflight. Preserve cache invalidation, short DB timeout behavior, and idempotent cancellation cleanup.
- [ ] **Step 4: Run all billing-cache reservation tests** and confirm they pass.
- [ ] **Step 5: Commit** with `feat: reserve only bounded request cost`.

### Task 5: Integrate estimation into supported gateway entrypoints

**Files:**
- Modify: `backend/internal/handler/gateway_handler.go`
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`
- Modify: `backend/internal/handler/gateway_handler_responses.go`
- Modify: `backend/internal/handler/openai_gateway_handler.go`
- Modify: `backend/internal/handler/openai_chat_completions.go`
- Modify: `backend/internal/handler/gemini_v1beta_handler.go`
- Modify: `backend/internal/handler/gateway_anthropic.go` or the existing Anthropic passthrough entrypoint
- Test: corresponding handler/service billing lifecycle tests

**Interfaces:**
- After request parsing, route metadata, and concurrency admission are available, handlers call the estimator and attach the returned hold with `WithUsageBalanceReservationHold` before `CheckBillingEligibility`.
- On unsupported media/search/WebSocket/count-token paths, handlers either implement a complete finite estimator or return the dedicated retryable estimation error; they must not pass zero and trigger a full-balance hold.

- [ ] **Step 1: Add failing lifecycle tests** proving five balance-10/hold-2 requests reach forwarding, the sixth is rejected before forwarding, cancellation releases the hold, successful usage settles once, and an unsupported estimate never forwards.
- [ ] **Step 2: Run lifecycle tests** and confirm they fail because handlers do not attach a hold.
- [ ] **Step 3: Wire the estimator into the ordinary Chat Completions, Responses, and Anthropic text paths** using existing parsed bodies and route/resolver metadata. Keep batch-image and subscription behavior unchanged.
- [ ] **Step 4: Add explicit fail-closed handling** for remaining handlers so no path can call `CheckBillingEligibility` with an implicit zero hold when standard balance reservation is enabled.
- [ ] **Step 5: Run focused handler/service tests** and verify rejected reservations do not invoke upstream forwarding and cleanup is idempotent.
- [ ] **Step 6: Commit** with `feat: enforce bounded reservations in gateway handlers`.

### Task 6: Verify final billing settlement and integration behavior

**Files:**
- Modify: `backend/internal/service/gateway_usage_billing.go` only if lifecycle wiring needs a narrow settlement hook
- Modify: `backend/internal/repository/usage_billing_repo.go` only if final `Apply` needs a compatibility guard
- Test: existing usage billing and repository integration tests

- [ ] **Step 1: Add/adjust tests** for settled reservations avoiding double deduction in `Apply`, actual cost below hold refunding the difference, actual cost above hold retaining the held row for recovery, release/expiry not double-refunding, and cache invalidation after every transition.
- [ ] **Step 2: Run the focused tests** and fix only lifecycle or compatibility defects revealed by them.
- [ ] **Step 3: Run the complete verification set:**
  - `go test -tags=unit ./internal/service -run 'Test(CheckBillingEligibility|UsageBalanceReservation|EstimateUsageBalance)' -count=1`
  - `go test -tags=unit ./internal/repository -run 'Test(UsageBillingRepositoryBalanceReservation|ReserveUsageBalance)' -count=1`
  - `go test -tags=integration ./internal/repository -run 'TestUsageBillingRepositoryBalanceReservation' -count=1`
  - `go test ./cmd/server`
  - `git diff --check`
- [ ] **Step 4: Inspect `git diff` and `git status`** for unrelated or ignored artifacts, then commit and push only after all required checks are green.

