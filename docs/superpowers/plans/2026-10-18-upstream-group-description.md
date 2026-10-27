# 上游分组描述导入 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (inline execution is authorized for this task). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在智能运维分组导入中读取、展示并编辑上游分组 description，并把编辑后的值冻结到本次导入预览。

**Architecture:** 为后端 `RemoteGroup` 和导入 `Selection` 增加 description 字段；Sub2API 目录与快速观察读取 `description`，New API 仅读取独立的 `description` 字段。前端在远程分组草稿中维护可编辑 description，编辑会清除旧预览，重新预览后由现有服务端 `Preview` 规范化并持久化 JSON；`Apply` 只消费已冻结预览，不写本地分组。

**Tech Stack:** Go、Gin/JSON、PostgreSQL JSONB 预览存储、Vue 3、TypeScript、Vitest、Go test。

**Spec:** `docs/superpowers/specs/2026-10-18-upstream-group-description-design.md`

## Global Constraints

- 不修改工作区已有的 Grok、IQ 页面冲突和无关未跟踪产物。
- 不新增数据库迁移、接口端点或本地分组写入。
- description 规范化后最多 300 个字符；旧客户端缺省时回退快照值。
- 生产代码每一步都先有一个会正确失败的测试，再实现最小改动。

---

### Task 1: 后端远程目录保留 description

**Files:**
- Modify: `backend/internal/upstreamgovernance/types.go`
- Modify: `backend/internal/upstreamgovernance/fast_observation.go`
- Modify: `backend/internal/upstreamgovernance/connector_catalog.go`
- Test: `backend/internal/upstreamgovernance/fast_observation_test.go`
- Test: `backend/internal/upstreamgovernance/connector_test.go` or a focused new test beside the connector catalog tests

**Interfaces:**
- Produces `RemoteGroup.Description string` serialized as `description`.
- Produces `fastSubGroups` and `subCatalog` values populated from Sub2API `description`.
- Produces New API values populated only from a dedicated `description` response field while preserving `desc` as the display name.

- [ ] **Step 1: Write failing tests** asserting Sub2API fast observation and full catalog preserve `description`, and New API keeps `desc` as `Name` while copying an independent `description`.
- [ ] **Step 2: Run focused Go tests and verify they fail because `RemoteGroup` and response decoders drop the field.**
- [ ] **Step 3: Add the field and decode it in both Sub2API paths and the New API group response.** Keep existing length and unsupported-contract checks, applying the 300-character limit to descriptions.
- [ ] **Step 4: Run focused Go tests and verify they pass.**

### Task 2: Backend preview accepts and freezes editable description

**Files:**
- Modify: `backend/internal/upstreamgovernance/types.go`
- Modify: `backend/internal/upstreamgovernance/service.go`
- Test: `backend/internal/upstreamgovernance/import_config_test.go`

**Interfaces:**
- `Selection.Description string` carries the reviewed draft.
- `Preview()` trims and validates the draft, falling back to `RemoteGroup.Description` when omitted, then stores it in `PreviewRow.Selection`.

- [ ] **Step 1: Add failing tests** for fallback, trim-and-freeze, and over-limit rejection.
- [ ] **Step 2: Run `go test ./internal/upstreamgovernance -run 'TestPreview.*Description'` and verify the new tests fail for the missing field/behavior.**
- [ ] **Step 3: Implement normalization in `Preview()` with a single 300-character validation rule; do not alter account matching or local group behavior.**
- [ ] **Step 4: Run the focused tests and the existing import configuration tests.**

### Task 3: Frontend type, draft editing, and preview rendering

**Files:**
- Modify: `frontend/src/api/admin/upstream-governance.ts`
- Modify: `frontend/src/views/admin/upstream-governance/ImportPanel.vue`
- Modify: the governance locale files containing `governance.*` strings
- Test: `frontend/src/views/admin/upstream-governance/ImportPanel.spec.ts`

**Interfaces:**
- `RemoteGroup.description?: string` and `Selection.description?: string` are represented in the frontend API types.
- Each `GroupChoice` initializes `description` from its remote group and sends it with `api.preview`.

- [ ] **Step 1: Add failing Vitest cases** for displaying a remote description, editing it in the expanded row, including it in the preview request, invalidating an existing preview after edit, and rendering the frozen value in the preview.
- [ ] **Step 2: Run the focused Vitest file and verify the new assertions fail.**
- [ ] **Step 3: Implement the draft field, edit invalidation, preview request mapping, and frozen preview rendering with a 300-character textarea limit.**
- [ ] **Step 4: Add concise localized labels and run the focused Vitest file.**

### Task 4: Integration verification and review

**Files:**
- No additional production files unless a test exposes a contract mismatch.

- [ ] **Step 1: Run backend package tests covering connector discovery and import preview.**
- [ ] **Step 2: Run the frontend `ImportPanel.spec.ts` and the relevant type/build check.**
- [ ] **Step 3: Inspect `git diff --stat` and `git status --short` to confirm only description-related files plus the design/plan docs changed.**
- [ ] **Step 4: Report tests and the fact that no database migration or local group mutation was introduced.**
