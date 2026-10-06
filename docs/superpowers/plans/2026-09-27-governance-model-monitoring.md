# Governance Model Monitoring Implementation Plan

> **For agentic workers:** Use subagent-driven development with disjoint file ownership and integration review. Track each deliverable below.

**Goal:** Implement administrator-only candy/pelican evaluation, configurable parallel batches, input/output token audits and persistent scheduled monitoring.

**Architecture:** Extend upstream governance through optional model-store and runner ports. Independent persistent policies and individual run records use leased background execution, keeping long inference away from collection/balance locks. The administrator UI shows configuration, progress, historical results and isolated HTML previews.

**Tech Stack:** Go, PostgreSQL, existing tiktoken-go/tokenizer, Vue 3, TypeScript, Vitest.

**Spec:** `docs/upstream-governance-model-monitoring-design.md`, approved by the user; add manual batch concurrency and explicit input/output usage audit.

## Global Constraints

- Preserve both exact benchmark prompts; candy reference is 21, while proof review is manual.
- Select an existing managed group Key; never create a Key or change production routing during evaluation.
- All APIs are admin-only. Synthetic requests never enter public channel logs.
- Low/medium/high are separate request configurations. Unsupported reasoning never silently disappears.
- Manual concurrency is deliberate within a batch; independent batches for the same target cannot overlap. Scheduled runs use the same executor and accounting.
- Store raw requests without auth headers, original answer and upstream usage alongside local token counts, hashes, byte/character counts and comparison limitations.
- Unknown tokenizer, hidden reasoning or message overhead do not become a verified fraud verdict. Truncation probes use randomized markers at multiple input locations.
- Back up the local binary and database immediately before replacement, preserve data fingerprints and verify health after upgrade.

## Task 1: Request runner, benchmarks and token audit

Files: `backend/internal/upstreamgovernance/model_types.go`, `model_runner.go`, `model_tokens.go`, `model_runner_test.go`, `model_tokens_test.go`.

Interface: `ModelRunner.RunModel(context.Context, Site, RemoteKey, ModelRunRequest) (ModelRunResult, error)`. Concrete implementation is an optional method of platformConnector. Config has managed_key_id, model, api_mode, efforts, templates, samples, concurrency, max_output_tokens, timeout_seconds, input_tokens, tokenizer and token_tolerance_percent. A run carries exactly one template, effort and sample.

- [x] Add shared config/result definitions before parallel consumers implement.
- [x] Test streaming heartbeat versus first text, incomplete stream, declared/missing usage, reasoning mappings, exact prompts, request serialization and response-size limits with local HTTP fixtures.
- [x] Implement Chat Completions, Responses, Anthropic and Gemini request/stream adapters using the existing public-host/explicit-proxy factory and independent inference timeout.
- [x] Test token mismatches with known encoded strings, absent usage, multilingual text and known/unknown tokenizer. Generate randomized first/middle/last context markers and retain expected answer separately.
- [x] Implement local visible-text counts, SHA256 evidence, upstream fields, tolerance-based suspicion and unknown/incomparable statuses. Preserve raw HTML separately from isolated preview extraction.
- [x] Run `go test ./internal/upstreamgovernance -run 'TestModel(Runner|Token|Prompt)'`.

## Task 2: Durable policy, queue, concurrency and service

Files: `backend/migrations/257_upstream_governance_model_monitoring.sql`, `backend/internal/upstreamgovernance/model_store.go`, `model_service.go`, `model_worker.go` and associated tests. Minimal hooks in Service and Start/Stop.

Interfaces: Service methods ListModelPolicies, SaveModelPolicy, DeleteModelPolicy, StartModelBatch, ListModelRuns, GetModelRun, CancelModelBatch, ReviewModelRun. Site ID scopes every method. Model policy stores enabled, interval_minutes, daily_request_limit and alert configuration in addition to test config. Each run is independently persisted with batch_id, status, sequence, config snapshot, result and timing.

- [x] Test validation, cross-site Key rejection, atomic request reservation, duplicate schedule slots, requested parallelism, cancel, crash recovery and stale site/Key rejection using an isolated PostgreSQL schema.
- [x] Add database constraints, policy revision and leased individual run claims; never repeat a request whose prior result is unknown.
- [x] Implement manual batches and automatic due policies with a target-level exclusion lock that permits the chosen within-batch concurrency.
- [x] Persist samples and artifacts outside production usage; expose paginated summaries and detailed responses on demand.
- [x] Include notification state and incident deduplication hooks; pause old matching probes when new scheduling explicitly takes over.
- [x] Run isolated PostgreSQL package tests and concurrent executor tests. The race-detector attempt was unavailable because CGO is disabled and GCC is absent; no race-detector pass is claimed.

## Task 3: Administrator model monitoring UI

Files: `frontend/src/api/admin/upstream-model-monitoring.ts`, `frontend/src/views/admin/upstream-governance/ModelMonitorPanel.vue`, `ModelRunDetail.vue`, tests; update governance view and zh/en locales.

- [x] Add separate model-monitoring tab and compact setup panel: group Key, model, protocol, effort selection, templates, samples, manual concurrency, token input size and output allowance.
- [x] Add run preview count, validation, start/cancel, progress polling, policy save/enable/disable, recent results, timing and token comparison.
- [x] Show candy numerical result separately from manual proof review; show pelican original response/download and isolated sandbox animation preview. Render untrusted model output only as text or isolated HTML.
- [x] Cover request payloads, stale site/polling responses, errors, disabled states and preview isolation with Vitest, then typecheck/lint/build.

## Task 4: Integration, notifications and local acceptance

Files: admin model handlers/routes, service notifier adapter, provider wiring, route tests, validation docs.

- [x] Bind strict admin endpoints to the service; reject invalid IDs/configuration before remote effects.
- [x] Reuse configured SMTP/admin recipient resolution for incidents and recovery; save failure state without leaking credentials.
- [x] Verify the complete package tests, frontend checks and embedded binary build. Confirm ordinary users cannot read new routes.
- [x] Use fixtures for concurrency, usage tampering and truncated input scenarios. Do not enable real upstream schedules, send real email or spend upstream credit just to accept the UI.
- [x] Back up and replace the local service, compare existing data fingerprints, inspect desktop/mobile UI and console, and commit the completed change.
