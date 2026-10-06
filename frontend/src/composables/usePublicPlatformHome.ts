import {
  computed,
  getCurrentInstance,
  onBeforeUnmount,
  ref,
} from 'vue'
import type { UserAvailableChannel, UserSupportedModelPricing } from '@/api/channels'
import type { ModelPlazaGroup, ModelPlazaResponse } from '@/api/modelPlaza'
import { getPublicChannels, getPublicPricing, getPublicChannelMonitors } from '@/api/publicHome'
import type { UserMonitorView } from '@/api/channelMonitor'

export type PublicPlatformHomeStatus = 'idle' | 'loading' | 'ready' | 'unavailable' | 'error'
/** Shared status shape for public-home child components. */
export type PublicHomeDataStatus = PublicPlatformHomeStatus

export interface PublicPlatformHomeChannelRow {
  platform: string
  channelCount: number
  groupCount: number
  modelCount: number
  channelNames: string[]
  groupNames: string[]
  modelNames: string[]
}

export interface PublicPlatformHomePricingRow {
  platform: string
  model: string
  inputPrice: number | null
  outputPrice: number | null
  cacheWritePrice: number | null
  cacheReadPrice: number | null
  perRequestPrice: number | null
  billingMode: UserSupportedModelPricing['billing_mode'] | null
  pricingSource: 'configured' | 'official' | 'unavailable'
  groupCount: number
}

export interface PublicPlatformHomeOptions {
  /** Skip channel and monitoring requests for catalog-only displays. */
  includeChannels?: boolean
  /** Override API readers for isolated consumers/tests. */
  getAvailableChannels?: typeof getPublicChannels
  getModelPlaza?: typeof getPublicPricing
  getChannelMonitors?: typeof getPublicChannelMonitors
}

function uniqueSorted(values: Iterable<string>): string[] {
  return Array.from(new Set(Array.from(values).filter(Boolean))).sort((a, b) => a.localeCompare(b))
}

function isAbortError(error: unknown, signal: AbortSignal): boolean {
  if (signal.aborted) return true
  if (!error || typeof error !== 'object') return false
  const candidate = error as { name?: unknown; code?: unknown }
  return candidate.name === 'AbortError' || candidate.code === 'ERR_CANCELED'
}

interface MutableChannelRow {
  channels: Set<string>
  groups: Set<number>
  groupNames: Set<string>
  models: Set<string>
}

function channelRowsFromSections(channels: UserAvailableChannel[]): PublicPlatformHomeChannelRow[] {
  const grouped = new Map<string, MutableChannelRow>()

  for (const channel of channels) {
    for (const section of channel.platforms || []) {
      const platform = section.platform.trim()
      if (!platform) continue

      const row = grouped.get(platform) ?? {
        channels: new Set<string>(),
        groups: new Set<number>(),
        groupNames: new Set<string>(),
        models: new Set<string>(),
      }
      const channelName = channel.name.trim()
      if (channelName) row.channels.add(channelName)
      for (const group of section.groups || []) {
        row.groups.add(group.id)
        const groupName = group.name.trim()
        if (groupName) row.groupNames.add(groupName)
      }
      for (const model of section.supported_models || []) {
        const modelName = model.name.trim()
        if (modelName) row.models.add(modelName)
      }
      grouped.set(platform, row)
    }
  }

  return Array.from(grouped.entries())
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([platform, row]) => ({
      platform,
      channelCount: row.channels.size,
      groupCount: row.groups.size,
      modelCount: row.models.size,
      channelNames: uniqueSorted(row.channels),
      groupNames: uniqueSorted(row.groupNames),
      modelNames: uniqueSorted(row.models),
    }))
}

interface MutablePricingRow {
  platform: string
  model: string
  groups: Set<number>
  pricing: UserSupportedModelPricing | null
  officialPricing: ModelPlazaGroup['models'][number]['official_pricing']
}

function pricingRowsFromGroups(groups: ModelPlazaGroup[]): PublicPlatformHomePricingRow[] {
  const grouped = new Map<string, MutablePricingRow>()

  for (const group of groups) {
    for (const model of group.models || []) {
      const platform = model.platform.trim() || group.platform.trim()
      const modelName = model.name.trim()
      if (!platform || !modelName) continue

      const key = `${platform}\u0000${modelName}`
      const row = grouped.get(key) ?? {
        platform,
        model: modelName,
        groups: new Set<number>(),
        pricing: null,
        officialPricing: null,
      }
      row.groups.add(group.id)
      // Keep the first API-provided price tuple so a model remains one row
      // even when it is exposed through multiple groups.
      if (row.pricing === null && model.pricing !== null) row.pricing = model.pricing
      if (row.officialPricing === null && model.official_pricing !== null) {
        row.officialPricing = model.official_pricing
      }
      grouped.set(key, row)
    }
  }

  return Array.from(grouped.values())
    .sort((left, right) => {
      const platformOrder = left.platform.localeCompare(right.platform)
      return platformOrder || left.model.localeCompare(right.model)
    })
    .map((row) => {
      const configured = row.pricing
      const official = configured === null ? row.officialPricing : null
      return {
        platform: row.platform,
        model: row.model,
        inputPrice: configured?.input_price ?? official?.input_price ?? null,
        outputPrice: configured?.output_price ?? official?.output_price ?? null,
        cacheWritePrice: configured?.cache_write_price ?? official?.cache_write_price ?? null,
        cacheReadPrice: configured?.cache_read_price ?? official?.cache_read_price ?? null,
        perRequestPrice: configured?.per_request_price ?? null,
        billingMode: configured?.billing_mode ?? null,
        pricingSource: configured !== null
          ? 'configured' as const
          : official !== null
            ? 'official' as const
            : 'unavailable' as const,
        groupCount: row.groups.size,
      }
    })
}

function isCurrentRequest(requestId: number, currentRequestId: number): boolean {
  return requestId === currentRequestId
}

/**
 * Load the small public platform summary used by the home page.
 *
 * Both sources are anonymous public-group projections, independent of console
 * feature gates. Keep their error states separate so one failure does not hide
 * the other summary.
 */
export function usePublicPlatformHome(options: PublicPlatformHomeOptions = {}) {
  const channelStatus = ref<PublicPlatformHomeStatus>('idle')
  const pricingStatus = ref<PublicPlatformHomeStatus>('idle')
  const channelError = ref<unknown>(null)
  const pricingError = ref<unknown>(null)
  const channels = ref<UserAvailableChannel[]>([])
  const plaza = ref<ModelPlazaResponse | null>(null)
  const monitors = ref<UserMonitorView[]>([])

  const channelRows = computed(() => channelRowsFromSections(channels.value))
  const pricingRows = computed(() => pricingRowsFromGroups(plaza.value?.groups || []))

  const channelCount = computed(() => channels.value.length)
  const modelCount = computed(() => {
    if (pricingStatus.value === 'ready') {
      return pricingRows.value.length
    }
    return channelRows.value.reduce((total, row) => total + row.modelCount, 0)
  })
  const platformCount = computed(() => {
    const platforms = new Set<string>()
    for (const row of channelRows.value) platforms.add(row.platform)
    for (const row of pricingRows.value) platforms.add(row.platform)
    return platforms.size
  })

  let activeController: AbortController | null = null
  let requestId = 0

  function abort(): void {
    requestId += 1
    activeController?.abort()
    activeController = null
    if (channelStatus.value === 'loading') channelStatus.value = 'idle'
    if (pricingStatus.value === 'loading') pricingStatus.value = 'idle'
  }

  async function load(): Promise<void> {
    abort()
    const controller = new AbortController()
    activeController = controller
    const currentRequestId = ++requestId
    const loadChannels = options.getAvailableChannels ?? getPublicChannels
    const loadPlaza = options.getModelPlaza ?? getPublicPricing
    const loadMonitors = options.getChannelMonitors ?? getPublicChannelMonitors

    channels.value = []
    plaza.value = null
    monitors.value = []
    channelError.value = null
    pricingError.value = null

    channelStatus.value = options.includeChannels === false ? 'idle' : 'loading'
    pricingStatus.value = 'loading'

    const channelTask = options.includeChannels === false ? Promise.resolve() : Promise.resolve()
          .then(() => loadChannels({ signal: controller.signal }))
          .then((value) => {
            if (!isCurrentRequest(currentRequestId, requestId)) return
            channels.value = value
            channelStatus.value = 'ready'
          })
          .catch((error: unknown) => {
            if (!isCurrentRequest(currentRequestId, requestId) || isAbortError(error, controller.signal)) return
            channelError.value = error
            channelStatus.value = 'error'
          })

    const pricingTask = Promise.resolve()
      .then(() => loadPlaza({ signal: controller.signal }))
      .then((value) => {
        if (!isCurrentRequest(currentRequestId, requestId)) return
        plaza.value = value
        pricingStatus.value = 'ready'
      })
      .catch((error: unknown) => {
        if (!isCurrentRequest(currentRequestId, requestId) || isAbortError(error, controller.signal)) return
        pricingError.value = error
        pricingStatus.value = 'error'
      })

    const monitorTask = options.includeChannels === false ? Promise.resolve() : Promise.resolve()
      .then(() => loadMonitors({ signal: controller.signal }))
      .then((value) => {
        if (!isCurrentRequest(currentRequestId, requestId)) return
        monitors.value = value.items || []
      })
      .catch((error: unknown) => {
        if (!isCurrentRequest(currentRequestId, requestId) || isAbortError(error, controller.signal)) return
        // Monitoring is additive to the public coverage summary. A monitor
        // endpoint failure must not hide the channel list or model catalog.
      })

    await Promise.all([channelTask, pricingTask, monitorTask])
    if (isCurrentRequest(currentRequestId, requestId) && activeController === controller) {
      activeController = null
    }
  }

  if (getCurrentInstance()) onBeforeUnmount(abort)

  return {
    channelStatus,
    pricingStatus,
    channelError,
    pricingError,
    channelRows,
    pricingRows,
    monitors,
    channelCount,
    modelCount,
    platformCount,
    load,
    abort,
  }
}

export { channelRowsFromSections as _channelRowsFromSections, pricingRowsFromGroups as _pricingRowsFromGroups }
