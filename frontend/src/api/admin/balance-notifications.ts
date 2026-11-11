import apiClient from '../client'

export type BalanceNotificationStatus = 'pending' | 'sending' | 'sent' | 'failed' | 'suppressed'
export type BalanceNotificationTrigger = 'automatic' | 'manual_batch'
export type UserNotificationEvent = 'balance.low' | 'system.health_snapshot' | 'payment.recharge_success_admin'

export interface BalanceNotification {
  id: number
  notification_id: string
  event_type?: UserNotificationEvent
  trigger_type: BalanceNotificationTrigger
  batch_id: string
  user_id: number
  user_email: string
  user_name: string
  recipient: string
  balance: number | null
  threshold: number | null
  subject: string
  status: BalanceNotificationStatus
  suppression_reason: string
  attempts: number
  last_attempt_at: string | null
  sent_at: string | null
  last_error: string
  created_at: string
  updated_at: string
  expires_at: string
}

export interface BalanceNotificationDetail extends BalanceNotification {
  body?: string
  delivery_key?: string
}

export interface BalanceNotificationQuery {
  page?: number
  page_size?: number
  user_id?: number
  user_email?: string
  recipient?: string
  status?: BalanceNotificationStatus
  trigger_type?: BalanceNotificationTrigger
  event_type?: UserNotificationEvent
  from?: string
  to?: string
}

export interface BalanceNotificationPage {
  items: BalanceNotification[]
  total: number
  page: number
  page_size: number
  pages: number
  summary: { total: number; pending: number; sent: number; failed: number; suppressed: number }
  evaluated_at: string
}

const base = '/admin/upstream-governance/notifications'
export default {
  async list(params: BalanceNotificationQuery) {
    return (await apiClient.get<BalanceNotificationPage>(base, { params })).data
  },
  async detail(id: number) {
    return (await apiClient.get<BalanceNotificationDetail>(`${base}/${id}`)).data
  },
}
