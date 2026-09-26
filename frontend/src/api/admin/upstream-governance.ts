import { apiClient } from '../client'
export type Transport = 'openai' | 'anthropic' | 'gemini' | 'antigravity' | 'grok' | 'kimi' | 'zhipu' | 'deepseek' | 'minimax' | 'opencode_go'
export interface SiteInput {
  name: string
  platform: 'sub2api' | 'newapi'
  base_url: string
  proxy_id: number | null
  enabled: boolean
  interval_minutes: number
}
export interface Site extends SiteInput {
  id: number
  version: number
  has_credential: boolean
  status: string
  last_error: string
  last_sync_at: string | null
  balance_monitor?: BalanceMonitor
  balance_monitor_status?: BalanceMonitorStatus
}
export interface BalanceMonitor {
  enabled: boolean
  threshold: number
  unit: 'usd' | 'quota'
  recipients: string[]
  cooldown_minutes: number
}
export interface BalanceMonitorStatus {
  state: 'disabled' | 'unknown' | 'healthy' | 'low'
  last_attempt_at: string | null
  last_notified_at: string | null
  last_error: string
}
export interface ModelTemplate {
  id: string
  name: string
  platform: Transport
  models: string[]
  is_default: boolean
}
export interface ModelTemplateCollection {
  version: number
  templates: ModelTemplate[]
}
export interface ImportAccountConfig {
  concurrency: number
  model_mapping: Record<string, string>
  upstream_billing_rate_sync_enabled: boolean
  quota_daily_limit: number
  quota_weekly_limit: number
  quota_limit: number
  openai_long_context_billing_enabled: boolean
}
export interface LoginInput {
  username?: string
  password?: string
  otp?: string
  challenge_token?: string
  captcha_token?: string
  session_token?: string
  user_id?: number
}
export interface DetectedSite {
  platform: SiteInput['platform']
  name: string
  base_url: string
  captcha_required: boolean
  captcha_site_key?: string
}
export interface AccountSummary {
  user_id: number
  username: string
  email: string
  balance: number | null
  frozen_balance: number | null
  used_balance: number | null
  unit: string
  source: string
}
export interface ManagedKey {
  id: number
  site_id: number
  remote_group_id: string
  platform: Transport
  remote_key_id: string
  marker: string
  has_key: boolean
  created_at: string
  updated_at: string
}
export interface KeySelection {
  remote_group_id: string
  platform: Transport
}
export interface KeyOutcome {
  remote_group_id: string
  platform: string
  status: 'created' | 'reused' | 'failed'
  managed_key?: ManagedKey
  key?: string
  error?: string
}
export interface RemotePrice {
  model: string
  platform: string
  unit: string
  input: number | null
  output: number | null
  per_request: number | null
  details?: Record<string, unknown>
}
export interface RemoteGroup {
  id: string
  name: string
  platform: string
  rate_multiplier: number | null
  user_rate_multiplier: number | null
  resolved_rate_multiplier: number | null
  peak_rate_enabled: boolean
  peak_start?: string
  peak_end?: string
  peak_rate_multiplier?: number | null
  models: string[]
  prices: RemotePrice[]
  source: string
}
export interface Snapshot {
  id: number
  site_id: number
  site_version: number
  created_at: string
  catalog: {
    account?: AccountSummary | null
    groups: RemoteGroup[]
    channels: { name: string; group_ids: string[]; models: string[] }[]
    warnings: string[]
  }
}
export interface Selection {
  remote_group_id: string
  platform: Transport
  local_group_id: number
  account_name: string
  cost_multiplier: number
  account_config?: ImportAccountConfig
}
export interface PreviewRow {
  selection: Selection
  remote_group: RemoteGroup
  target: {
    id: number
    name: string
    platform: string
    sale_multiplier: number
  }
  existing: {
    id: number
    name: string
    group_ids: number[]
    cost_multiplier: number
  } | null
  will_create_key: boolean
  marker: string
}
export interface ApplyResult {
  preview_id: string
  items: {
    remote_group_id: string
    platform: string
    account_id?: number
    status: string
    error?: string
  }[]
}
export interface Preview {
  id: string
  site_id: number
  site_version: number
  snapshot_id: number
  rows: PreviewRow[]
  created_at: string
  expires_at: string
  result?: ApplyResult
}
export interface Binding {
  id: number
  site_id: number
  remote_group_id: string
  platform: string
  local_group_id: number
  account_id: number
  probe_enabled: boolean
  probe_model: string
  probe_interval_minutes: number
}
export interface GovernanceEvent {
  id: number
  kind: string
  resource: string
  before: string
  after: string
  acknowledged: boolean
  created_at: string
}
export interface Check {
  id: number
  binding_id: number
  model: string
  success: boolean
  latency_ms: number
  error_code?: string
  created_at: string
}
export interface Page<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}
const base = '/admin/upstream-governance/sites'
const site = (id: number) => `${base}/${id}`
const api = {
  async modelTemplates() {
    return (await apiClient.get<ModelTemplateCollection>('/admin/upstream-governance/model-templates')).data
  },
  async saveModelTemplates(input: ModelTemplateCollection) {
    return (await apiClient.put<ModelTemplateCollection>('/admin/upstream-governance/model-templates', input)).data
  },
  async balanceMonitor(id: number, input: BalanceMonitor & { version: number }) {
    return (await apiClient.put<Site>(`${site(id)}/balance-monitor`, input)).data
  },
  async list() {
    return (await apiClient.get<Site[]>(base)).data
  },
  async detect(input: { base_url: string; proxy_id: number | null }) {
    return (await apiClient.post<DetectedSite>(`${base}/detect`, input)).data
  },
  async create(input: SiteInput) {
    return (await apiClient.post<Site>(base, input)).data
  },
  async update(id: number, input: SiteInput & { version: number }) {
    return (await apiClient.put<Site>(site(id), input)).data
  },
  async remove(id: number) {
    await apiClient.delete(site(id))
  },
  async connect(id: number, input: LoginInput) {
    return (
      await apiClient.post<{
        site?: Site
        challenge?: { kind: string; token?: string }
      }>(`${site(id)}/connect`, input)
    ).data
  },
  async sync(id: number) {
    return (await apiClient.post<Snapshot>(`${site(id)}/sync`, undefined, { timeout: 300000 })).data
  },
  async catalog(id: number) {
    return (await apiClient.get<Snapshot>(`${site(id)}/catalog`)).data
  },
  async bindings(id: number) {
    return (await apiClient.get<Binding[]>(`${site(id)}/bindings`)).data
  },
  async keys(id: number) {
    return (await apiClient.get<ManagedKey[]>(`${site(id)}/keys`)).data
  },
  async createKeys(id: number, input: { snapshot_id: number; selections: KeySelection[] }) {
    return (await apiClient.post<{ items: KeyOutcome[] }>(`${site(id)}/keys`, input, { timeout: 120000 })).data
  },
  async revealKey(id: number, keyId: number) {
    return (await apiClient.post<{ managed_key: ManagedKey; key: string }>(`${site(id)}/keys/${keyId}/reveal`, {})).data
  },
  async preview(id: number, input: { selections: Selection[] }) {
    return (await apiClient.post<Preview>(`${site(id)}/previews`, input)).data
  },
  async apply(id: number, previewId: string) {
    return (
      await apiClient.post<ApplyResult>(
        `${site(id)}/previews/${encodeURIComponent(previewId)}/apply`,
        undefined,
        { timeout: 300000 },
      )
    ).data
  },
  async events(id: number, page = 1) {
    return (
      await apiClient.get<Page<GovernanceEvent>>(`${site(id)}/events`, {
        params: { page, page_size: 20 },
      })
    ).data
  },
  async acknowledge(id: number, eventId: number) {
    await apiClient.post(`${site(id)}/events/${eventId}/ack`)
  },
  async checks(id: number, page = 1) {
    return (
      await apiClient.get<Page<Check>>(`${site(id)}/checks`, {
        params: { page, page_size: 20 },
      })
    ).data
  },
  async check(id: number, bindingId: number, model: string) {
    return (
      await apiClient.post<Check>(`${site(id)}/bindings/${bindingId}/check`, {
        model,
      })
    ).data
  },
  async monitor(
    id: number,
    bindingId: number,
    input: { enabled: boolean; model: string; interval_minutes: number },
  ) {
    return (
      await apiClient.put<Binding>(
        `${site(id)}/bindings/${bindingId}/monitor`,
        input,
      )
    ).data
  },
}
export default api
