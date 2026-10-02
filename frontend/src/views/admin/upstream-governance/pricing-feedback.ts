export interface PricingFeedback {
  titleKey: string
  detailKey: string
  actionKey: string
}

export interface PricingStatusInput {
  enabled: boolean
  manual_owner?: boolean
  protected?: boolean
}

const reasonMessages = new Map<string, string>([
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
])

const observationMessages = new Map<string, string>([
  ['idle', 'idle'],
  ['running', 'running'],
  ['healthy', 'healthy'],
  ['rate_limited', 'rateLimited'],
  ['reauth_required', 'reauthRequired'],
  ['error', 'error'],
  ['disabled', 'disabled'],
])

// Reason codes belong in optional technical details, not in the primary card.
export function pricingFeedback(reason: string): PricingFeedback {
  const message = reasonMessages.get(reason) ?? 'unknown'
  const prefix = `governance.reliability.feedback.${message}`
  return { titleKey: `${prefix}.title`, detailKey: `${prefix}.detail`, actionKey: `${prefix}.action` }
}

export function pricingStatusKey(policy: PricingStatusInput): string {
  const status = !policy.enabled ? 'disabled' : policy.manual_owner ? 'manual' : policy.protected ? 'protected' : 'managed'
  return `governance.reliability.status.${status}`
}

export function observationStatusKey(status: string): string {
  return `governance.reliability.observation.${observationMessages.get(status) ?? 'unknown'}`
}
