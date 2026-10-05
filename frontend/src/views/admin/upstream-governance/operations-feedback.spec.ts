import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en/governance'
import zh from '@/i18n/locales/zh/governance'
import { operationReasonKey, operationsSeverityKey, targetSection, timelineKindKey, timelineStatusKey, workbenchKindKey, workbenchStatusKey } from './operations-feedback'

describe('bounded operations presentation', () => {
  it.each([
    ['authorization_missing', 'authorizationMissing'], ['reauth_required', 'reauthorization'],
    ['key_issue', 'keyIssue'], ['balance_low', 'balanceLow'], ['balance_unknown', 'balanceUnknown'], ['balance_stale', 'balanceStale'],
    ['key_verification_pending', 'keyVerificationPending'], ['key_verification_unknown', 'keyVerificationUnknown'], ['key_protection_failed', 'keyProtectionFailed'],
    ['collection_failed', 'collectionFailed'], ['collection_stale', 'collectionStale'], ['collection_unknown', 'collectionUnknown'], ['snapshot_outdated', 'snapshotOutdated'],
    ['observation_failed', 'observationFailed'], ['observation_rate_limited', 'observationRateLimited'], ['observation_stale', 'observationStale'], ['observation_unknown', 'observationUnknown'],
    ['unknown_cost', 'unknownCost'], ['invalid_policy', 'invalidPolicy'], ['increase_review', 'increaseReview'], ['manual_owner', 'manualOwner'], ['decrease_stability', 'decreaseStability'], ['pricing_protected', 'pricingProtected'],
    ['price_ready', 'priceReady'], ['baseline', 'baseline'], ['increase', 'increase'], ['decrease', 'decrease'], ['unchanged', 'unchanged'], ['policy_disabled', 'policyDisabled'],
    ['notification_retry', 'notificationRetry'], ['notification_accepted', 'notificationAccepted'], ['notification_pending', 'notificationPending'], ['notification_sending', 'notificationSending'],
    ['pricing_recorded', 'pricingRecorded'], ['event_recorded', 'eventRecorded'],
  ])('maps %s to a localized reason instead of raw codes', (reason, label) => {
    expect(operationReasonKey(reason)).toBe(`governance.workbench.reasons.${label}`)
  })

  it.each(['', 'unexpected upstream password=private', '__proto__', 'constructor'])('does not echo unknown reason %j', reason => {
    expect(operationReasonKey(reason)).toBe('governance.workbench.reasons.unknown')
  })

  it('uses existing localized event names without constructing unknown keys', () => {
    expect(operationReasonKey('rate_changed')).toBe('governance.rateChanged')
    expect(operationReasonKey('group_removed')).toBe('governance.groupRemoved')
    expect(operationReasonKey('auto_reauthorization_required')).toBe('governance.autoReauthorizationRequired')
  })

  it('distinguishes accepted mail and read records from verified delivery or resolution', () => {
    expect(timelineStatusKey('notification', 'sent')).toBe('governance.timeline.status.accepted')
    expect(timelineStatusKey('event', 'acknowledged')).toBe('governance.timeline.status.read')
    expect(timelineStatusKey('pricing', 'applied')).toBe('governance.timeline.status.applied')
    expect(timelineStatusKey('event', 'sent')).toBe('governance.timeline.status.unknown')
  })

  it('only maps known workbench destinations to existing sections', () => {
    expect(targetSection('connect')).toBe('overview')
    expect(targetSection('keys')).toBe('import')
    expect(targetSection('monitor')).toBe('monitor')
    expect(targetSection('https://outside.example')).toBeNull()
    expect(targetSection('__proto__')).toBeNull()
  })

  it('bounds unknown category, severity, and current status labels', () => {
    expect(workbenchKindKey('authorization')).toBe('governance.workbench.kinds.authorization')
    expect(workbenchKindKey('__proto__')).toBe('governance.workbench.kinds.unknown')
    expect(timelineKindKey('notification')).toBe('governance.timeline.kinds.notification')
    expect(timelineKindKey('new_kind')).toBe('governance.timeline.kinds.unknown')
    expect(operationsSeverityKey('critical')).toBe('governance.workbench.severity.critical')
    expect(operationsSeverityKey('healthy')).toBe('governance.workbench.severity.unknown')
    expect(workbenchStatusKey('stale')).toBe('governance.workbench.status.stale')
    expect(workbenchStatusKey('resolved')).toBe('governance.workbench.status.unknown')
  })

  it('resolves emitted dynamic translation keys in both locales', () => {
    const reasons = ['authorization_missing', 'reauth_required', 'key_issue', 'balance_low', 'balance_unknown', 'balance_stale', 'collection_failed', 'collection_stale', 'collection_unknown', 'snapshot_outdated', 'observation_failed', 'observation_rate_limited', 'observation_stale', 'observation_unknown', 'unknown_cost', 'invalid_policy', 'increase_review', 'manual_owner', 'decrease_stability', 'pricing_protected', 'price_ready', 'baseline', 'increase', 'decrease', 'unchanged', 'policy_disabled', 'notification_retry', 'notification_accepted', 'notification_pending', 'notification_sending', 'pricing_recorded', 'event_recorded', 'not_known', 'rate_changed']
    const keys = [
      ...reasons.map(operationReasonKey),
      ...['authorization', 'key', 'balance', 'collection', 'fast_observation', 'pricing', 'notification', 'unknown'].map(workbenchKindKey),
      ...['critical', 'warning', 'info', 'unknown'].map(operationsSeverityKey),
      ...['action_required', 'low', 'unknown', 'stale', 'error', 'rate_limited', 'protected', 'pending', 'sending'].map(workbenchStatusKey),
      ...['event', 'pricing', 'notification', 'unknown'].map(timelineKindKey),
      ...['recorded', 'acknowledged', 'unknown'].map(status => timelineStatusKey('event', status)),
      ...['prepared', 'applied', 'protected', 'rejected', 'conflict', 'failed', 'unknown'].map(status => timelineStatusKey('pricing', status)),
      ...['pending', 'sending', 'sent', 'unknown'].map(status => timelineStatusKey('notification', status)),
    ]
    for (const messages of [en, zh]) for (const key of keys) {
      const value = key.replace(/^governance\./, '').split('.').reduce<unknown>((value, part) => typeof value === 'object' && value ? (value as Record<string, unknown>)[part] : undefined, messages)
      expect(value, key).toEqual(expect.stringMatching(/\S/))
    }
  })
})
