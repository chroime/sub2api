import { apiClient } from '../client'
import type { Transport } from './upstream-governance'

export type ModelAPIMode = 'chat_completions' | 'responses' | 'anthropic' | 'gemini'
export type ModelTargetType = 'upstream' | 'local_group'
export type ModelEffort = 'low' | 'medium' | 'high'
export type ModelTestTemplate = 'candy' | 'pelican' | 'token_audit' | 'context' | 'probe'
export type ModelReview = 'pending' | 'pass' | 'fail'
export interface ModelTestConfig {
  target_type?: ModelTargetType
  managed_key_id: number
  local_group_id?: number
  local_api_key_id?: number
  target_owner_user_id?: number
  platform: Transport
  model: string
  api_mode: ModelAPIMode
  efforts: ModelEffort[]
  templates: ModelTestTemplate[]
  samples: number
  concurrency: number
  max_output_tokens: number
  timeout_seconds: number
  first_content_timeout_seconds?: number
  idle_timeout_seconds?: number
  input_tokens: number
  tokenizer: 'auto' | 'o200k_base' | 'cl100k_base' | 'none'
  token_tolerance_percent: number
}
export interface LocalModelTarget {
  group_id: number
  group_name: string
  platform: Transport
  api_key_id: number
  api_key_name: string
  status?: string
  expires_at?: string | null
}
export interface LocalModelWorkspace {
  site: {
    id: number
    name: string
    version: number
  }
  targets: LocalModelTarget[]
}
export interface ModelPolicy {
  id: number
  site_id: number
  name: string
  config: ModelTestConfig
  enabled: boolean
  interval_minutes: number
  daily_request_limit: number
  notify_enabled: boolean
  recipients: string[]
  failure_threshold: number
  take_over_legacy: boolean
  version: number
  next_run_at?: string
  last_error?: string
  notify_error?: string
  notify_at?: string | null
  created_at?: string
  updated_at?: string
}
export interface ModelBatchInput { request_id: string; policy_id?: number; config: ModelTestConfig }
export interface ModelBatch { id: string; total: number; concurrency: number }
export interface ModelUsage {
  input_tokens: number | null
  output_tokens: number | null
  cached_tokens: number | null
  reasoning_tokens: number | null
  cache_creation_tokens?: number | null
}
export interface ModelTokenAudit {
  tokenizer: string
  source: string
  input_local: number | null
  input_declared_total?: number | null
  input_overhead_margin?: number
  output_local: number | null
  input_state: string
  output_state: string
  input_delta_percent: number | null
  output_delta_percent: number | null
  input_hash: string
  output_hash: string
  input_bytes: number
  output_bytes: number
  input_chars: number
  output_chars: number
  markers_passed?: boolean | null
  markers?: { position: string; label: string; expected: string; found: boolean }[]
  echo_expected?: string
  echo_passed?: boolean | null
  input_requested_tokens?: number
  output_declared_visible?: number | null
  tokenizer_version?: string
  note: string
}
export interface ModelRunResult {
  success: boolean
  streamed?: boolean
  completed?: boolean
  error_code: string
  http_status: number
  response_model: string
  effort_support: string
  sent_parameters?: Record<string, unknown>
  request_body?: unknown
  input_text?: string
  response_text?: string
  html?: string
  finish_reason: string
  ttft_ms: number | null
  duration_ms: number
  usage: ModelUsage
  raw_usage?: unknown
  tokens: ModelTokenAudit
  candy_verdict: string
  candy_answer?: number | null
  template_version?: string
  adapter_version?: string
}
export interface ModelRun {
  id: string
  batch_id: string
  site_id: number
  policy_id: number | null
  sequence: number
  request: { config: ModelTestConfig; template: ModelTestTemplate; effort: ModelEffort; sample: number }
  status: string
  result?: ModelRunResult | null
  review: ModelReview
  review_note: string
  reviewer_id?: number | null
  reviewed_at?: string | null
  review_version?: number
  created_at: string
  started_at: string | null
  finished_at: string | null
}
export interface ModelRunPage { items: ModelRun[]; total: number; page: number; page_size: number; counts?: Record<string, number> }
export interface ModelStatsGroup {
  model: string
  api_mode: ModelAPIMode
  managed_key_id: number
  target_type?: ModelTargetType
  local_group_id?: number
  local_api_key_id?: number
  template: ModelTestTemplate
  effort: ModelEffort
  config_hash: string
  samples: number
  successes: number
  failures: number
  unknown: number
  ttft_samples?: number
  comparable?: boolean
  template_version?: string
  adapter_version?: string
  p50_ttft_ms: number | null
  p95_ttft_ms: number | null
  avg_duration_ms: number | null
  last_run_at: string | null
  points: { at: string; samples: number; successes: number; failures: number; ttft_ms: number | null }[]
}
export interface ModelStats { days: number; groups: ModelStatsGroup[]; truncated?: boolean }
const site = (id: number) => `/admin/upstream-governance/sites/${id}`
const api = {
  async localWorkspace() { return (await apiClient.get<LocalModelWorkspace>('/admin/upstream-governance/local-model-workspace')).data },
  async localTargets() { return (await apiClient.get<LocalModelTarget[]>('/admin/upstream-governance/local-model-targets')).data },
  async policies(id: number) { return (await apiClient.get<ModelPolicy[]>(`${site(id)}/model-policies`)).data },
  async savePolicy(id: number, input: ModelPolicy) {
    const { id: policyId, name, config, enabled, interval_minutes, daily_request_limit, notify_enabled, recipients, failure_threshold, take_over_legacy, version } = input
    return (await apiClient.post<ModelPolicy>(`${site(id)}/model-policies`, { id: policyId, name, config, enabled, interval_minutes, daily_request_limit, notify_enabled, recipients, failure_threshold, take_over_legacy, version })).data
  },
  async deletePolicy(id: number, policyId: number, version: number) { await apiClient.delete(`${site(id)}/model-policies/${policyId}`, { params: { version } }) },
  async startBatch(id: number, input: ModelBatchInput) { return (await apiClient.post<ModelBatch>(`${site(id)}/model-batches`, input)).data },
  async cancelBatch(id: number, batchId: string) { await apiClient.post(`${site(id)}/model-batches/${encodeURIComponent(batchId)}/cancel`, {}) },
  async runs(id: number, page = 1, batchId = '') { return (await apiClient.get<ModelRunPage>(`${site(id)}/model-runs`, { params: { page, page_size: 20, ...(batchId ? { batch_id: batchId } : {}) } })).data },
  async run(id: number, runId: string) { return (await apiClient.get<ModelRun>(`${site(id)}/model-runs/${encodeURIComponent(runId)}`)).data },
  async deleteRun(id: number, runId: string) { await apiClient.delete(`${site(id)}/model-runs/${encodeURIComponent(runId)}`) },
  async review(id: number, runId: string, review: ModelReview, note: string) { return (await apiClient.put<ModelRun>(`${site(id)}/model-runs/${encodeURIComponent(runId)}/review`, { review, note })).data },
  async stats(id: number, days = 7) { return (await apiClient.get<ModelStats>(`${site(id)}/model-stats`, { params: { days } })).data },
}
export default api
