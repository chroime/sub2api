# Upstream v0.2.24 integration

Date: 2026-11-20 (Asia/Shanghai)

- Release: https://github.com/Wei-Shaw/sub2api/releases/tag/v0.2.24
- Upstream commit: `7515db6dc56c2a6120dfb7abed70ef089ebc1457`.
- Local parent: `70fa1732a609cadeb74a83c762f463c15eadc180`.
- Recovery branch: `codex/backup-main-before-v0.2.24-20261120`.

## Integration decisions

- Request field conflict validation runs after the group allowlist and before local input routing and composite routing. Balance precharge and downstream usage projection remain in every applicable gateway chain.
- Anthropic long-context model listing is combined with local Grok voice aliases.
- Wire dependency injection is regenerated, retaining local bulk email and governance services and adding the upstream administrator user platform quota dependency. Local constructor tests are updated accordingly.
- The upstream model ID copy action is ported to the active vendor price table; the superseded model table remains deleted.
- The release tag still contains `0.2.23` in `backend/cmd/server/VERSION`; this merge sets it to `0.2.24`.
- Local uncommitted work is preserved separately and restored after integration. Local PostgreSQL and configuration backups are verified before restarting services. This source merge does not deploy a remote server.

## Validation

- 2,887 backend regression tests, including subtests, covering changed upstream packages and local channel monitoring. Two repository tests requiring a separate PostgreSQL test configuration were skipped.
- 224 frontend regression tests covering account usage, bulk editing, account views, model ID copying, and locale keys.
- Frontend type checking and ESLint for changed files.
- Git conflict and whitespace checks.

The restored local changes, production frontend and backend builds, and local service startup are verified after the merge as a separate integration step.
