import { apiClient } from '@/api/client'
import { isBalancePrechargeMoney } from '@/utils/balancePrecharge'

export const MAX_PRECHARGE_REVIEW_NOTE_LENGTH = 2000

export type BalancePrechargeReviewFilter = 'pending' | 'resolved' | 'settled' | 'all'

export interface BalancePrechargeReview {
  id: string
  user_id: number
  user_email: string
  api_key_id: number
  group_id: number
  group_name: string
  amount: number
  reason: string
  request_id: string
  account_id: number
  model: string
  status: 'pending' | 'resolved' | 'settled'
  created_at: string
  updated_at: string
  resolved_at?: string | null
  resolved_by?: number | null
  resolution?: 'release' | 'charge' | ''
  actual_cost?: number | null
  note?: string
}

export interface BalancePrechargeReviewPage {
  items: BalancePrechargeReview[]
  total: number
}

export interface BalancePrechargeResolution {
  action: 'release' | 'charge'
  actual_cost: number
  note: string
}

export async function getBalancePrechargeReviews(query: {
  status: BalancePrechargeReviewFilter
  limit: number
  offset: number
}): Promise<BalancePrechargeReviewPage> {
  const { data } = await apiClient.get<BalancePrechargeReviewPage>('/admin/settings/balance-precharge/reviews', { params: query })
  return data
}

export async function resolveBalancePrechargeReview(id: string, payload: BalancePrechargeResolution): Promise<BalancePrechargeReview> {
  const note = payload.note.trim()
  if (!id || !isBalancePrechargeMoney(payload.actual_cost) || !note || [...note].length > MAX_PRECHARGE_REVIEW_NOTE_LENGTH ||
    (payload.action !== 'release' && payload.action !== 'charge') ||
    (payload.action === 'release' ? payload.actual_cost !== 0 : payload.actual_cost <= 0)) {
    throw new Error('Invalid balance precharge resolution')
  }
  const { data } = await apiClient.post<BalancePrechargeReview>(
    `/admin/settings/balance-precharge/reviews/${encodeURIComponent(id)}/resolve`,
    { action: payload.action, actual_cost: payload.actual_cost, note }
  )
  return data
}
