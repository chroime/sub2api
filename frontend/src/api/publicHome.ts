import { apiClient } from './client'
import type { UserAvailableChannel } from './channels'
import type { UserMonitorListResponse } from './channelMonitor'
import type { ModelPlazaResponse } from './modelPlaza'

export async function getPublicChannels(options?: { signal?: AbortSignal }): Promise<UserAvailableChannel[]> {
  const { data } = await apiClient.get<UserAvailableChannel[]>('/public/home/channels', { signal: options?.signal })
  return data
}

export async function getPublicPricing(options?: { signal?: AbortSignal }): Promise<ModelPlazaResponse> {
  const { data } = await apiClient.get<ModelPlazaResponse>('/public/home/pricing', { signal: options?.signal })
  return data
}

export async function getPublicChannelMonitors(options?: { signal?: AbortSignal }): Promise<UserMonitorListResponse> {
  const { data } = await apiClient.get<UserMonitorListResponse>('/public/home/channel-monitors', { signal: options?.signal })
  return data
}
