import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en/governance'
import zh from '@/i18n/locales/zh/governance'
import { observationStatusKey, pricingFeedback, pricingStatusKey } from './pricing-feedback'

const reasonCases = [
  ['unknown_cost', 'unknownCost'],
  ['invalid_policy', 'invalidPolicy'],
  ['increase_review', 'increaseReview'],
  ['decrease_stability', 'decreaseStability'],
  ['manual_owner', 'manualOwner'],
  ['policy_disabled', 'policyDisabled'],
  ['unchanged', 'unchanged'],
  ['baseline', 'baseline'],
  ['increase', 'increase'],
  ['decrease', 'decrease'],
  ['price_ready', 'priceReady'],
  ['version_conflict', 'versionConflict'],
  ['stale_preview', 'versionConflict'],
  ['site_busy', 'siteBusy'],
  ['unsupported', 'unsupported'],
  ['unsupported_contract', 'unsupported'],
  ['reauth_required', 'reauthorization'],
  ['invalid_input', 'invalidInput'],
  ['not_found', 'notFound'],
  ['operation_failed', 'failed'],
  ['timeout', 'timeout'],
  ['rate_limited', 'rateLimited'],
  ['persistent_encryption_required', 'encryptionRequired'],
] as const

function translation(messages: unknown, key: string): unknown {
  return key.replace(/^governance\./, '').split('.').reduce<unknown>((value, part) => {
    if (!value || typeof value !== 'object') return undefined
    return (value as Record<string, unknown>)[part]
  }, messages)
}

describe('pricing feedback', () => {
  it.each(reasonCases)('gives %s a specific explanation and next step', (reason, message) => {
    expect(pricingFeedback(reason)).toEqual({
      titleKey: `governance.reliability.feedback.${message}.title`,
      detailKey: `governance.reliability.feedback.${message}.detail`,
      actionKey: `governance.reliability.feedback.${message}.action`,
    })
  })

  it.each(['', 'new_unknown_reason', 'Unhandled failure with private upstream detail', '__proto__', 'constructor'])('keeps unknown reason %j out of the main feedback', reason => {
    expect(pricingFeedback(reason)).toEqual({
      titleKey: 'governance.reliability.feedback.unknown.title',
      detailKey: 'governance.reliability.feedback.unknown.detail',
      actionKey: 'governance.reliability.feedback.unknown.action',
    })
  })

  it('resolves every feedback field in both supported locales', () => {
    for (const reason of [...reasonCases.map(([reason]) => reason), 'unknown']) {
      for (const messages of [en, zh]) {
        for (const key of Object.values(pricingFeedback(reason))) {
          expect(translation(messages, key), key).toEqual(expect.stringMatching(/\S/))
        }
      }
    }
  })
})

describe('pricing policy status', () => {
  it('does not label a disabled policy as automatically managed', () => {
    expect(pricingStatusKey({ enabled: false })).toBe('governance.reliability.status.disabled')
    expect(pricingStatusKey({ enabled: false, manual_owner: true, protected: true })).toBe('governance.reliability.status.disabled')
  })

  it('keeps manual ownership distinct from generic protection', () => {
    expect(pricingStatusKey({ enabled: true, manual_owner: true, protected: true })).toBe('governance.reliability.status.manual')
    expect(pricingStatusKey({ enabled: true, manual_owner: true })).toBe('governance.reliability.status.manual')
  })

  it('labels an enabled non-manual protected policy as protected', () => {
    expect(pricingStatusKey({ enabled: true, protected: true })).toBe('governance.reliability.status.protected')
  })

  it('labels only enabled non-manual unprotected policies as managed', () => {
    expect(pricingStatusKey({ enabled: true })).toBe('governance.reliability.status.managed')
  })
})

describe('observation status', () => {
  it.each([
    ['idle', 'idle'],
    ['running', 'running'],
    ['healthy', 'healthy'],
    ['rate_limited', 'rateLimited'],
    ['reauth_required', 'reauthRequired'],
    ['error', 'error'],
    ['disabled', 'disabled'],
    ['', 'unknown'],
    ['not_a_known_status', 'unknown'],
    ['__proto__', 'unknown'],
  ])('renders %j as a bounded localized status', (status, message) => {
    expect(observationStatusKey(status)).toBe(`governance.reliability.observation.${message}`)
  })

  it('resolves every observation and policy status in both locales', () => {
    const keys = [
      ...['idle', 'running', 'healthy', 'rate_limited', 'reauth_required', 'error', 'disabled', 'unknown'].map(observationStatusKey),
      pricingStatusKey({ enabled: false }),
      pricingStatusKey({ enabled: true, manual_owner: true }),
      pricingStatusKey({ enabled: true, protected: true }),
      pricingStatusKey({ enabled: true }),
    ]
    for (const messages of [en, zh]) {
      for (const key of keys) expect(translation(messages, key), key).toEqual(expect.stringMatching(/\S/))
    }
  })
})
