import type { OperationsSection } from '@/api/admin/upstream-operations'
import { eventKeys } from './feedback'

const reasons = new Map<string, string>([
  ['authorization_missing', 'authorizationMissing'], ['reauth_required', 'reauthorization'],
  ['key_issue', 'keyIssue'], ['balance_low', 'balanceLow'], ['balance_unknown', 'balanceUnknown'], ['balance_stale', 'balanceStale'],
  ['key_verification_pending', 'keyVerificationPending'], ['key_verification_unknown', 'keyVerificationUnknown'], ['key_protection_failed', 'keyProtectionFailed'],
  ['collection_failed', 'collectionFailed'], ['collection_stale', 'collectionStale'], ['collection_unknown', 'collectionUnknown'], ['snapshot_outdated', 'snapshotOutdated'],
  ['observation_failed', 'observationFailed'], ['observation_rate_limited', 'observationRateLimited'], ['observation_stale', 'observationStale'], ['observation_unknown', 'observationUnknown'],
  ['unknown_cost', 'unknownCost'], ['invalid_policy', 'invalidPolicy'], ['increase_review', 'increaseReview'], ['manual_owner', 'manualOwner'], ['decrease_stability', 'decreaseStability'], ['pricing_protected', 'pricingProtected'],
  ['price_ready', 'priceReady'], ['baseline', 'baseline'], ['increase', 'increase'], ['decrease', 'decrease'], ['unchanged', 'unchanged'], ['policy_disabled', 'policyDisabled'],
  ['notification_retry', 'notificationRetry'], ['notification_accepted', 'notificationAccepted'], ['notification_pending', 'notificationPending'], ['notification_sending', 'notificationSending'],
  ['pricing_recorded', 'pricingRecorded'], ['event_recorded', 'eventRecorded'],
])
const workbenchKinds = new Set(['authorization', 'key', 'balance', 'collection', 'fast_observation', 'pricing', 'notification'])
const severities = new Set(['critical', 'warning', 'info'])
const currentStatuses = new Set(['action_required', 'low', 'unknown', 'stale', 'error', 'rate_limited', 'protected', 'pending', 'sending'])
const timelineKinds = new Set(['event', 'pricing', 'notification'])
const timelineStatuses = new Map<string, Map<string, string>>([
  ['event', new Map([['recorded', 'recorded'], ['acknowledged', 'read']])],
  ['pricing', new Map(['prepared', 'applied', 'protected', 'rejected', 'conflict', 'failed'].map(status => [status, status]))],
  ['notification', new Map([['pending', 'pending'], ['sending', 'sending'], ['sent', 'accepted']])],
])
const sections = new Map<string, OperationsSection>([['connect', 'overview'], ['keys', 'import'], ['monitor', 'monitor']])

export function operationReasonKey(reason: string): string {
  const message = reasons.get(reason)
  if (message) return `governance.workbench.reasons.${message}`
  if (Object.prototype.hasOwnProperty.call(eventKeys, reason)) return `governance.${eventKeys[reason]}`
  return 'governance.workbench.reasons.unknown'
}
export function operationsSeverityKey(severity: string): string { return `governance.workbench.severity.${severities.has(severity) ? severity : 'unknown'}` }
export function targetSection(target: string): OperationsSection | null { return sections.get(target) ?? null }
export function timelineKindKey(kind: string): string { return `governance.timeline.kinds.${timelineKinds.has(kind) ? kind : 'unknown'}` }
export function timelineStatusKey(kind: string, status: string): string { return `governance.timeline.status.${timelineStatuses.get(kind)?.get(status) ?? 'unknown'}` }
export function workbenchKindKey(kind: string): string { return `governance.workbench.kinds.${workbenchKinds.has(kind) ? kind : 'unknown'}` }
export function workbenchStatusKey(status: string): string { return `governance.workbench.status.${currentStatuses.has(status) ? status : 'unknown'}` }
