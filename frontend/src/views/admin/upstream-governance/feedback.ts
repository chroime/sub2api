const reasonKeys: Record<string, string> = {
  stale_preview: 'stale',
  site_busy: 'siteBusy',
  site_in_use: 'siteInUse',
  reauth_required: 'reauth',
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
}

export const siteStateKeys: Record<string, string> = {
  disconnected: 'stateDisconnected',
  connected: 'stateConnected',
  healthy: 'stateHealthy',
  error: 'stateError',
  reauth_required: 'stateReauth',
}
