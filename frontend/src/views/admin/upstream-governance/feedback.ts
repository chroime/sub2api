const reasonKeys: Record<string, string> = {
  stale_preview: 'stale',
  site_busy: 'siteBusy',
  site_in_use: 'siteInUse',
  reauth_required: 'reauth',
  upstream_key_missing: 'upstreamKeyMissing',
  upstream_key_unverifiable: 'upstreamKeyUnverifiable',
  candidate_not_visible: 'keyCandidateNotVisible',
  manually_abandoned: 'keyRepairAbandonedReason',
  repair_account_missing: 'keyRepairAccountMissing',
  repair_account_present: 'keyRepairAccountPresent',
  repair_context_changed: 'keyRepairContextChanged',
  repair_catalog_stale: 'keyRepairCatalogStale',
  unsupported_contract: 'unsupported',
  persistent_encryption_required: 'encryption',
  invalid_input: 'invalid',
  not_found: 'notFound',
  operation_failed: 'error',
  timeout: 'timeout',
}
export function errorKey(error: unknown): string {
  const e = error as { reason?: string; status?: number }
  return `governance.${reasonKeys[e?.reason || ''] || (e?.status === 409 ? 'stale' : 'error')}`
}
export function keyRepairErrorKey(error: unknown): string {
  const key = errorKey(error)
  return key === 'governance.stale' ? 'governance.keyRepairConflict' : key
}
export const eventKeys: Record<string, string> = {
  balance_low: 'balanceLow',
  balance_recovered: 'balanceRecovered',
  balance_notification_sent: 'balanceNotificationSent',
  balance_notification_failed: 'balanceNotificationFailed',
  group_added: 'groupAdded',
  group_removed: 'groupRemoved',
  group_changed: 'groupChanged',
  rate_changed: 'rateChanged',
  models_changed: 'modelsChanged',
  price_changed: 'priceChanged',
  channels_changed: 'channelsChanged',
  sync_failed: 'syncFailed',
  import_applied: 'importApplied',
  reconciliation_applied: 'reconciliationApplied',
  probe_failed: 'probeFailed',
  probe_recovered: 'probeRecovered',
  auto_reauthorization_required: 'autoReauthorizationRequired',
  key_missing_suspected: 'keyMissingSuspected',
  key_missing_confirmed: 'keyMissingConfirmed',
  key_recovered: 'keyRecovered',
  key_account_paused: 'keyAccountPaused',
  key_account_restored: 'keyAccountRestored',
}

export const siteStateKeys: Record<string, string> = {
  disconnected: 'stateDisconnected',
  connected: 'stateConnected',
  healthy: 'stateHealthy',
  error: 'stateError',
  reauth_required: 'stateReauth',
}
