# Governance usability, import policy and balance monitoring

This implements the administrator's approved built-in governance design and the follow-up requirements. It extends the existing isolated governance branch and retains native login, collection, managed keys and immutable import previews.

## Interface

Use a searchable upstream list, a selected-site overview, a compact group mapping table and separate monitoring/history views. Balances and service state come first; model prices and detailed configuration expand on demand. Reuse existing teal theme, accessible controls and official platform icons. Credentials and managed keys remain visible in plaintext to authenticated administrators as requested.

All displayed governance dates use `YYYY-MM-DD HH:mm:ss`, including collection, key creation, preview expiry, events, probes and notifications. Storage remains ISO timestamps. Display uses the browser's configured system timezone, consistent with existing frontend date behavior.

## Import policy

Selections carry a complete `account_config`, frozen into the preview. Default account name is normalized upstream base URL followed by `--` and the cost multiplier. The account's notes contain the actual API key after apply; the secret does not appear in preview or event payloads.

Defaults: concurrency 5000; upstream declared billing rate synchronization enabled; daily quota 10000, weekly quota 700000, total quota 10000000; API long-context billing enabled for OpenAI. Quota values follow existing account USD and rolling-window semantics. The native billing endpoint determines whether continuous declared-rate synchronization is supported; enabling it does not invent support in New API.

Model restriction templates are shared server-persisted admin settings. Each template has an ID, name, protocol, selected models and default flag. Each protocol has at most one default. Without a default, the UI offers collected group models and the existing platform presets. An empty whitelist must not silently become unrestricted. The preview stores the resulting model mapping, not a mutable template reference.

Account compare-and-swap and retry checks include all newly managed fields, including notes and concurrency. Account updates preserve unrelated credential/extra fields and quota usage counters. Rate changes from the enabled synchronization mechanism are expected and must not break safe retries.

## Balance notifications

Each upstream wallet has its own monitor configuration: enabled, native-unit threshold, recipient override list and cooldown minutes. Monitoring is off until configured; defaults are threshold 10 and cooldown 1440 minutes. Sub2API uses USD; New API retains quota units. An unknown balance is not zero.

After a successful fresh collection, a balance at or below the threshold triggers an email through the existing system mail sender. Empty recipient override resolves enabled verified administrator notification emails, then the first active administrator. Saving config does not send mail. Repeated low-balance mail respects durable per-recipient cooldown; failed sends retry no sooner than 15 minutes. Recovery records an event and preserves cooldown to avoid flapping spam. Delivery failure remains separate from upstream connection health.

## Verification and preservation

Verify account policy persistence and CAS, template validation/version conflicts, balance threshold transitions/unknown values/cooldown/failures, and UI interactions/date formatting. Mail verification uses a synthetic transport or loopback SMTP only. Before migrating/restarting the running local fixture, back up its current database to preserve the user's intervening work. Do not automatically reimport or rewrite their existing accounts. Finish with frontend production build, embedded backend build and browser review of the running local app.
