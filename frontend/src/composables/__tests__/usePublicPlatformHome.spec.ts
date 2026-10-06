import { beforeEach, describe, expect, it, vi } from 'vitest'
import { usePublicPlatformHome } from '@/composables/usePublicPlatformHome'
import type { UserAvailableChannel } from '@/api/channels'
import type { ModelPlazaResponse } from '@/api/modelPlaza'

const { getAvailable, getModelPlaza, getMonitors } = vi.hoisted(() => ({
  getAvailable: vi.fn(),
  getModelPlaza: vi.fn(),
  getMonitors: vi.fn(),
}))

vi.mock('@/api/publicHome', () => ({ getPublicChannels: getAvailable, getPublicPricing: getModelPlaza, getPublicChannelMonitors: getMonitors }))

function makeChannels(): UserAvailableChannel[] {
  return [
    {
      name: 'Primary gateway',
      description: 'Main gateway',
      platforms: [
        {
          platform: 'openai',
          groups: [
            {
              id: 1,
              name: 'Public GPT',
              platform: 'openai',
              subscription_type: 'standard',
              rate_multiplier: 1,
              peak_rate_enabled: false,
              peak_start: '',
              peak_end: '',
              peak_rate_multiplier: 1,
              is_exclusive: false,
            },
          ],
          supported_models: [
            { name: 'gpt-4.1', platform: 'openai', pricing: null },
            { name: 'gpt-4.1', platform: 'openai', pricing: null },
          ],
        },
      ],
    },
    {
      name: 'Secondary gateway',
      description: '',
      platforms: [
        {
          platform: 'openai',
          groups: [
            {
              id: 2,
              name: 'Premium GPT',
              platform: 'openai',
              subscription_type: 'subscription',
              rate_multiplier: 1.2,
              peak_rate_enabled: false,
              peak_start: '',
              peak_end: '',
              peak_rate_multiplier: 1,
              is_exclusive: true,
            },
          ],
          supported_models: [
            { name: 'gpt-4.1-mini', platform: 'openai', pricing: null },
          ],
        },
        {
          platform: 'anthropic',
          groups: [],
          supported_models: [
            { name: 'claude-sonnet', platform: 'anthropic', pricing: null },
          ],
        },
      ],
    },
  ]
}

function makePlaza(): ModelPlazaResponse {
  return {
    description: '',
    groups: [
      {
        id: 1,
        name: 'Public GPT',
        description: '',
        platform: 'openai',
        subscription_type: 'standard',
        rate_multiplier: 1,
        peak_rate_enabled: false,
        peak_start: '',
        peak_end: '',
        peak_rate_multiplier: 1,
        is_exclusive: false,
        image_rate_independent: false,
        image_rate_multiplier: 1,
        long_context_pricing_enabled: false,
        models: [
          {
            name: 'gpt-4.1',
            platform: 'openai',
            pricing: {
              billing_mode: 'token',
              input_price: 0.000002,
              output_price: 0.000008,
              cache_write_price: null,
              cache_read_price: 0.000001,
              image_input_price: null,
              image_output_price: null,
              per_request_price: null,
              intervals: [],
            },
            official_pricing: null,
          },
          {
            name: 'claude-sonnet',
            platform: 'anthropic',
            pricing: null,
            official_pricing: {
              input_price: 0.000003,
              output_price: 0.000015,
              cache_write_price: null,
              cache_read_price: 0.0000015,
            },
          },
        ],
      },
    ],
  }
}

describe('usePublicPlatformHome', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getAvailable.mockResolvedValue(makeChannels())
    getModelPlaza.mockResolvedValue(makePlaza())
    getMonitors.mockResolvedValue({ items: [] })
  })

  it('loads both sources and aggregates typed rows by platform', async () => {
    const home = usePublicPlatformHome()

    await home.load()

    expect(getAvailable).toHaveBeenCalledOnce()
    expect(getModelPlaza).toHaveBeenCalledOnce()
    expect(home.channelStatus.value).toBe('ready')
    expect(home.pricingStatus.value).toBe('ready')
    expect(home.channelCount.value).toBe(2)
    expect(home.modelCount.value).toBe(2)
    expect(home.platformCount.value).toBe(2)
    expect(home.channelRows.value).toEqual([
      {
        platform: 'anthropic',
        channelCount: 1,
        groupCount: 0,
        modelCount: 1,
        channelNames: ['Secondary gateway'],
        groupNames: [],
        modelNames: ['claude-sonnet'],
      },
      {
        platform: 'openai',
        channelCount: 2,
        groupCount: 2,
        modelCount: 2,
        channelNames: ['Primary gateway', 'Secondary gateway'],
        groupNames: ['Premium GPT', 'Public GPT'],
        modelNames: ['gpt-4.1', 'gpt-4.1-mini'],
      },
    ])
    expect(home.pricingRows.value).toEqual([
      {
        platform: 'anthropic',
        model: 'claude-sonnet',
        inputPrice: 0.000003,
        outputPrice: 0.000015,
        cacheWritePrice: null,
        cacheReadPrice: 0.0000015,
        perRequestPrice: null,
        billingMode: null,
        pricingSource: 'official',
        groupCount: 1,
      },
      {
        platform: 'openai',
        model: 'gpt-4.1',
        inputPrice: 0.000002,
        outputPrice: 0.000008,
        cacheWritePrice: null,
        cacheReadPrice: 0.000001,
        perRequestPrice: null,
        billingMode: 'token',
        pricingSource: 'configured',
        groupCount: 1,
      },
    ])
  })

  it('marks models without configured or official prices as unavailable', async () => {
    const plaza = makePlaza()
    plaza.groups[0].models[0].pricing = null
    plaza.groups[0].models[0].official_pricing = null
    getModelPlaza.mockResolvedValue(plaza)
    const home = usePublicPlatformHome()

    await home.load()

    expect(home.pricingRows.value.find((row) => row.model === 'gpt-4.1')).toEqual({
      platform: 'openai',
      model: 'gpt-4.1',
      inputPrice: null,
      outputPrice: null,
      cacheWritePrice: null,
      cacheReadPrice: null,
      perRequestPrice: null,
      billingMode: null,
      pricingSource: 'unavailable',
      groupCount: 1,
    })
  })

  it('loads channels without authentication and isolates a failed pricing endpoint', async () => {
    getModelPlaza.mockRejectedValue({ status: 404 })
    const home = usePublicPlatformHome()

    await home.load()

    expect(getAvailable).toHaveBeenCalledOnce()
    expect(getModelPlaza).toHaveBeenCalledOnce()
    expect(home.channelStatus.value).toBe('ready')
    expect(home.pricingStatus.value).toBe('error')
    expect(home.channelCount.value).toBe(2)
    expect(home.modelCount.value).toBe(3)
    expect(home.platformCount.value).toBe(2)
  })

  it('loads only the model catalog when channel display is disabled', async () => {
    const home = usePublicPlatformHome({ includeChannels: false })
    await home.load()
    expect(getAvailable).not.toHaveBeenCalled()
    expect(getMonitors).not.toHaveBeenCalled()
    expect(getModelPlaza).toHaveBeenCalledOnce()
    expect(home.pricingStatus.value).toBe('ready')
    expect(home.channelStatus.value).toBe('idle')
    expect(home.modelCount.value).toBeGreaterThan(0)
    expect(home.platformCount.value).toBeGreaterThan(0)
  })

  it('aborts an in-flight request and does not publish stale results', async () => {
    let resolvePlaza!: (value: ModelPlazaResponse) => void
    getModelPlaza.mockReturnValueOnce(new Promise<ModelPlazaResponse>((resolve) => {
      resolvePlaza = resolve
    }))

    const home = usePublicPlatformHome()
    const pending = home.load()
    home.abort()
    resolvePlaza(makePlaza())
    await pending

    expect(home.pricingStatus.value).toBe('idle')
    expect(home.pricingRows.value).toEqual([])
  })
})
