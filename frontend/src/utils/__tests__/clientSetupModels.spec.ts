import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ClientSetupModelUnavailableError, clientSetupModelIds, clientV1BaseUrl, fetchClientSetupCatalog,
  normalizeClientBaseUrl, selectClientSetupModel
} from '@/utils/clientSetupModels'

afterEach(() => vi.unstubAllGlobals())

describe('client setup model selection', () => {
  it.each(['https://example.com', 'https://example.com/', 'https://example.com/v1', 'https://example.com/v1/'])('normalizes API paths idempotently for %s', base => {
    expect(clientV1BaseUrl(base)).toBe('https://example.com/v1')
    expect(normalizeClientBaseUrl(base)).toBe('https://example.com')
  })

  it('prefers supported current defaults, then the actual catalog without inventing availability', () => {
    expect(selectClientSetupModel('grok', ['grok-4.3', 'grok-4.7'])).toBe('grok-4.7')
    expect(() => selectClientSetupModel('openai', ['gpt-6-sol'])).toThrow('Required model')
    expect(selectClientSetupModel('composite', ['gpt-6-sol'])).toBe('gpt-6-sol')
    expect(selectClientSetupModel('anthropic', ['claude-opus-5-5'])).toBe('claude-opus-5-5')
    expect(selectClientSetupModel('gemini')).toBe('gemini-3.8-flash')
    expect(() => selectClientSetupModel('openai', [])).toThrow()
    expect(() => selectClientSetupModel('grok', ['grok-imagine-image', 'grok-imagine-video'])).toThrow()
  })

  it('accepts standard and manifest model lists while removing duplicate/media entries', () => {
    expect(clientSetupModelIds({ data: [{ id: 'gpt-6.1-sol' }, { id: 'gpt-6.1-sol' }, { id: 'gpt-image-2' }, { id: 'grok-imagine-video' }] })).toEqual(['gpt-6.1-sol'])
    expect(clientSetupModelIds({ models: [{ slug: 'gemini-3.8-flash' }, { slug: '' }, null] })).toEqual(['gemini-3.8-flash'])
  })

  it.each(['registered-provider', 'constructor'])('uses discovered text models for the new %s platform', platform => {
    expect(selectClientSetupModel(platform, ['provider-image', 'provider-text'])).toBe('provider-text')
    expect(selectClientSetupModel(platform, ['provider-text', 'preferred-text'], 'preferred-text')).toBe('preferred-text')
    expect(selectClientSetupModel(platform, ['provider-text'], 'missing-text')).toBe('provider-text')
  })

  it.each([undefined, [], ['provider-image']])('reports an unavailable model for an unknown platform without a usable catalog: %s', available => {
    expect(() => selectClientSetupModel('registered-provider', available)).toThrow(ClientSetupModelUnavailableError)
  })

  it('enforces requested versions and native client families', () => {
    expect(() => selectClientSetupModel('gemini', ['gemini-2.5-pro'])).toThrow('Gemini 3.8')
    expect(selectClientSetupModel('gemini', ['gemini-3.5-pro', 'gemini-4-argon'])).toBe('gemini-4-argon')
    expect(() => selectClientSetupModel('gemini', ['claude-sonnet-5-5'])).toThrow('Gemini 3.8')
    expect(() => selectClientSetupModel('anthropic', ['gemini-3.8-flash'])).toThrow('Claude 5.5')
    expect(() => selectClientSetupModel('anthropic', ['claude-opus-4-6'])).toThrow('Claude 5.5')
    expect(() => selectClientSetupModel('grok', ['grok-4.5'])).toThrow('grok-4.7')
    expect(selectClientSetupModel('antigravity', ['claude-sonnet-4-6', 'gemini-3.8-flash'])).toBe('gemini-3.8-flash')
  })

  it.each([
    { data: [{ id: 'grok-4.7' }] },
    { models: [{ slug: 'grok-4.7', supported_reasoning_levels: [] }] }
  ])('fetches the current key catalog without changing the key', async payload => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => JSON.stringify(payload) })
    vi.stubGlobal('fetch', fetch)
    const catalog = await fetchClientSetupCatalog('https://example.com/v1/', 'sk-fixture')
    expect(catalog.modelIds).toEqual(['grok-4.7'])
    expect(fetch).toHaveBeenCalledWith(expect.stringMatching(/^https:\/\/example\.com\/v1\/models\?/), expect.objectContaining({ method: 'GET', cache: 'no-store', headers: { Accept: 'application/json', Authorization: 'Bearer sk-fixture' } }))
  })

  it('uses the same models endpoint when the configured URL includes /v1beta', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => '{"data":[{"id":"gemini-3.8-flash"}]}' })
    vi.stubGlobal('fetch', fetch)
    await fetchClientSetupCatalog('https://example.com/v1beta/', 'sk-fixture')
    expect(fetch).toHaveBeenCalledWith(expect.stringMatching(/^https:\/\/example\.com\/v1\/models\?/), expect.anything())
  })

  it('uses the native model list without a Codex Responses filter for Claude clients', async () => {
    const fetch = vi.fn().mockResolvedValue({ ok: true, text: async () => '{"data":[{"id":"native-route"}]}' })
    vi.stubGlobal('fetch', fetch)
    expect((await fetchClientSetupCatalog('https://example.com', 'sk-fixture', undefined, 'native')).modelIds).toEqual(['native-route'])
    expect(fetch).toHaveBeenCalledWith('https://example.com/v1/models?endpoint=messages', expect.anything())
  })
})
