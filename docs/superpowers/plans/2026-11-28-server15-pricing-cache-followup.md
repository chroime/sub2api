# Server 15 pricing and weighted cache metrics implementation plan

> Execute task by task in this existing isolated worktree. User authorized server 15 fixes and explicitly selected actual-token-weighted cache rates. No new agent delegation is needed.

**Goal:** Restore the DeepSeek V4.1 public name's official reference quote and display genuine token-weighted cache rates over the existing latest-24-stream sample.

**Architecture:** Keep the sample, group/account/model isolation and TTFT calculation. Normalize actual upstream input/read/write counters after selecting the latest 24; include valid zero-hit input in the denominator, count TTL writes once, omit missing/conflicting measurements. Resolve explicitly declared verified price aliases to verified canonical catalog cards for official display only; preserve routing names and site billing.

**Tech stack:** Go, PostgreSQL, Vue/TypeScript, JSON catalog, Python release workers.

## Task 1: Token-weighted monitor rates

- [ ] Update PostgreSQL regressions first: unequal sizes, zero-hit input, upstream vs billed tokens, TTL deduplication, missing/conflicting telemetry, no older sample backfill, model/group/account isolation.
- [ ] Run them against a disposable local PostgreSQL cluster and observe old aggregation failures.
- [ ] Modify `backend/internal/repository/channel_monitor_metrics.go`; keep TTFT and selection/index paths unchanged.
- [ ] Update metric comments, translations and frontend assertions to state the actual formula; retain the requested cache-rate label.
- [ ] Run repository integration/unit and affected frontend tests.

## Task 2: DeepSeek V4.1 official reference

- [ ] Add a regression for the `deepseek-v4.1-flash` public name and explicit canonical reference, including missing/unverified canonical rejection and unchanged billing catalog values.
- [ ] Observe failure before implementation.
- [ ] Add catalog metadata for an explicitly verified price alias to `deepseek-flash`; preserve the site's original call name and billing values. Read canonical prices only for the official view.
- [ ] Update audit follow-up notes with current official sources and status semantics. Run pricing/plaza/billing regressions and import tests.

## Task 3: Frozen release and server 15 update

- [ ] Review final diff, run relevant builds/checks, commit and push the full immutable commit.
- [ ] Rediscover server 15 runtime and protected state; create a fresh complete PG/Redis/config/source/previous-image backup, verify it and its private offsite copy.
- [ ] Build that exact source in an isolated release directory; validate image identity/version without mounting production data.
- [ ] Replace only Sub2API after gates pass. Verify DeepSeek V4.1 CNY quote and explicit zero write, current raw-token aggregate equivalence, TTFT behavior, public health and protected containers/configuration.
- [ ] Save actual release/backup identities and a concise receipt. Preserve all previous recovery sets; leave server 38 unchanged at its recorded quota-blocked state.
