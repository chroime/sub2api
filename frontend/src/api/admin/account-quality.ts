import { apiClient } from '../client'

export type ControlMode = 'record_only' | 'record_notify' | 'auto_quarantine'
export type IsolationScope = 'model' | 'account'
export interface Policy {
  id: number
  version: number
  name: string
  enabled: boolean
  platform: string
  model: string
  api_mode: string
  effort: string
  templates: string[]
  max_output_tokens: number
  timeout_seconds: number
  interval_seconds: number
  confirm_seconds: number
  failure_threshold: number
  recovery_threshold: number
  cooldown_seconds: number
  recovery_interval_seconds: number
  daily_request_limit: number
  control_mode: ControlMode
  isolation_scope: IsolationScope
  created_at?: string
  updated_at?: string
}
export interface BindingOverrides {
  interval_seconds?: number
  failure_threshold?: number
  recovery_threshold?: number
  isolation_scope?: IsolationScope
}
export interface Binding {
  id: number
  version: number
  account_id: number
  account_name: string
  account_type?: string
  platform: string
  policy_id: number
  policy_name: string
  policy_version: number
  model: string
  auto_isolate: boolean
  control_mode?: ControlMode
  interval_seconds?: number
  failure_threshold?: number
  recovery_threshold?: number
  isolation_scope?: IsolationScope
  auto_control_eligible?: boolean
  source?: string
  overrides?: BindingOverrides
  state: string
  failures: number
  recovery_passes: number
  next_run_at?: string | null
  last_run_at?: string | null
  last_duration_ms?: number
  quality_verdict?: string
  execution_status?: string
  reason?: string
  isolated: boolean
  override_until?: string | null
  recent_runs?: Run[]
}
export interface Run {
  id: string
  binding_id: number
  account_id: number
  policy_id: number
  policy_version: number
  binding_version: number
  account_name?: string
  model: string
  template: string
  template_version: string
  prompt?: string
  expected?: string
  response_text?: string
  result_preview?: string
  requested_model?: string
  mapped_model?: string
  returned_model?: string
  api_mode?: string
  effort?: string
  max_output_tokens?: number
  output_limit_mode?: string
  execution_status: string
  quality_verdict: string
  reason?: string
  created_at?: string
  started_at?: string | null
  finished_at?: string | null
  duration_ms?: number
  input_tokens?: number | null
  output_tokens?: number | null
  reasoning_tokens?: number | null
  http_status?: number
  error_class?: string
  auto_control_eligible?: boolean
  stale?: boolean
  config?: Policy
  identity_version?: string
  credential_account_id?: number
  upstream_request_id?: string
  finish_reason?: string
  cost_usd?: number | null
}
export interface Action {
  id: number | string
  account_id: number
  binding_id: number
  action: string
  reason: string
  actor_id?: number | null
  created_at: string
}
export interface Settings {
  version: number
  concurrency: number
  daily_request_limit: number
  daily_cost_limit_usd: number
  notifications: {
    email_enabled: boolean
    email_recipients: string[]
    qq_enabled: boolean
    qq_targets: string[]
    events: string[]
    reminder_seconds: number
  }
}
export interface Template { id: string; name: string; version: string; description?: string }
export interface Capability { platform: string; api_mode: string; account_types: string[]; automatic_control: boolean; output_limit_mode?: string; reason?: string }
export interface Overview { policies: Policy[]; bindings: Binding[]; templates: Template[]; settings: Settings; capabilities: Capability[] }
export interface AccountDetail { bindings: Binding[]; runs: Run[]; actions: Action[] }
export type ControlAction = 'isolate' | 'release' | 'override' | 'confirm_false_positive' | 'set_auto'
export interface ControlInput { version: number; action: ControlAction; reason: string; duration_minutes?: number; auto_isolate?: boolean }
export interface Assignment { policy_id: number; account_ids: number[]; source?: string; overrides?: BindingOverrides }

const base = '/admin/account-quality'
const api = {
  async overview() { return (await apiClient.get<Overview>(`${base}/overview`)).data },
  async policies() { return (await apiClient.get<Policy[]>(`${base}/policies`)).data },
  async savePolicy(input: Policy) {
    return input.id
      ? (await apiClient.put<Policy>(`${base}/policies/${input.id}`, input)).data
      : (await apiClient.post<Policy>(`${base}/policies`, input)).data
  },
  async deletePolicy(id: number, version: number) { await apiClient.delete(`${base}/policies/${id}`, { params: { version } }) },
  async assign(input: Assignment) { return (await apiClient.post<Binding[]>(`${base}/bindings`, { ...input, auto_isolate: false })).data },
  async run(id: number, input: { request_id: string; version: number }) { return (await apiClient.post<Run>(`${base}/bindings/${id}/run`, input)).data },
  async control(id: number, input: ControlInput) { return (await apiClient.post<Binding>(`${base}/bindings/${id}/control`, input)).data },
  async summaries(accountIds: number[]) { return (await apiClient.post<Record<string, Binding[]>>(`${base}/accounts/summaries`, { account_ids: accountIds })).data },
  async account(id: number) { return (await apiClient.get<AccountDetail>(`${base}/accounts/${id}`)).data },
  async runs(params: { account_id?: number; binding_id?: number; limit?: number } = {}) { return (await apiClient.get<Run[]>(`${base}/runs`, { params })).data },
  async runDetail(id: string) { return (await apiClient.get<Run>(`${base}/runs/${encodeURIComponent(id)}`)).data },
  async cancelRun(id: string) { await apiClient.post(`${base}/runs/${encodeURIComponent(id)}/cancel`, {}) },
  async settings() { return (await apiClient.get<Settings>(`${base}/settings`)).data },
  async saveSettings(input: Settings) { return (await apiClient.put<Settings>(`${base}/settings`, input)).data },
}
export default api
