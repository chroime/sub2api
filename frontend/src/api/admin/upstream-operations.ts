import { apiClient } from '../client'

export type OperationsSeverity = 'critical' | 'warning' | 'info'
export type WorkbenchKind = 'authorization' | 'key' | 'balance' | 'collection' | 'fast_observation' | 'pricing' | 'notification'
export type TimelineKind = 'event' | 'pricing' | 'notification'
export type TimelineFilter = 'all' | TimelineKind
export type OperationsSection = 'overview' | 'import' | 'models' | 'monitor' | 'history'

export interface WorkbenchItem {
  id: string
  site_id: number
  site_name: string
  base_url: string
  kind: WorkbenchKind
  severity: OperationsSeverity
  status: string
  reason: string
  resource_id: string
  resource_name: string
  impact_count: number
  observed_at: string | null
  next_attempt_at: string | null
  shared: boolean
  target_tab: 'connect' | 'keys' | 'monitor'
}

export interface OperationsPage<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  evaluated_at: string
}

export interface WorkbenchPage extends OperationsPage<WorkbenchItem> {
  summary: Record<OperationsSeverity, number>
}

/** A whitelist of persisted history facts, never raw payloads or mail contents. */
export interface TimelineItem {
  id: string
  record_id: string
  kind: TimelineKind
  site_id: number
  site_name: string
  base_url: string
  resource_id: string
  resource_name: string
  severity: OperationsSeverity
  status: string
  reason: string
  created_at: string
  shared: boolean
  acknowledged: boolean | null
  before_rate: number | null
  after_rate: number | null
  before_cost: number | null
  after_cost: number | null
  /** The queue increments this counter on failed sends, not on all attempts. */
  attempts: number | null
  next_attempt_at: string | null
  sent_at: string | null
  related_record_id: string | null
}

const base = '/admin/upstream-governance'
const api = {
  async workbench(input: { site_id?: number; page?: number; page_size?: number } = {}) {
    const params = { ...input, page: input.page ?? 1, page_size: input.page_size ?? 20 }
    return (await apiClient.get<WorkbenchPage>(`${base}/workbench`, { params })).data
  },
  async timeline(siteId: number, input: { kind?: TimelineFilter; page?: number; page_size?: number } = {}) {
    const params = { kind: input.kind ?? 'all', page: input.page ?? 1, page_size: input.page_size ?? 20 }
    return (await apiClient.get<OperationsPage<TimelineItem>>(`${base}/sites/${siteId}/timeline`, { params })).data
  },
}
export default api
