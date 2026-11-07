<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { BalanceHealth, ReadinessOverview, Site, Snapshot } from '@/api/admin/upstream-governance'
import ReadinessChecklist from './ReadinessChecklist.vue'
import { formatGovernanceTime } from './format'
import { siteStateKeys } from './feedback'
import { cadenceText } from './cadence'
import StatusIndicator from './StatusIndicator.vue'
const props = defineProps<{
  site: Site
  snapshot: Snapshot | null
  bindingCount: number
  balanceHealth?: BalanceHealth | null
  readiness?: ReadinessOverview | null
  readinessLoading?: boolean
  readinessReadFailed?: boolean
}>()
const emit = defineEmits<{
  navigate: [target: ReadinessOverview['checks'][number]['target_tab']]
  retryReadiness: []
}>()
const { t } = useI18n()
const amount = (value: number | null | undefined) =>
  value == null
    ? t('governance.unknown')
    : `${Number(value.toFixed(6))} ${props.snapshot?.catalog.account?.unit || ''}`
// Once every readiness item is configured the checklist folds into one line.
const readinessComplete = computed(() =>
  !props.readinessLoading && !props.readinessReadFailed && !!props.readiness?.checks.length
  && props.readiness.checks.every(check => check.state === 'configured'),
)
</script>
<template>
  <dl data-test="site-status-strip" class="grid min-w-0 gap-px overflow-hidden rounded-xl border border-gray-200 bg-gray-200 dark:border-dark-600 dark:bg-dark-600 sm:grid-cols-2 xl:grid-cols-4">
    <div class="min-w-0 bg-white p-3 dark:bg-dark-800">
      <dt class="text-xs text-gray-500">{{ t(balanceHealth?.stale ? 'governance.lastKnownBalance' : 'governance.balance') }}</dt>
      <dd data-test="account-balance" class="mt-1 break-all text-lg font-semibold tabular-nums">{{ amount(snapshot?.catalog.account?.balance) }}</dd>
      <p class="mt-0.5 truncate text-xs text-gray-500">{{ snapshot?.catalog.account?.username || snapshot?.catalog.account?.email || t('governance.upstreamAccount') }}</p>
      <details class="mt-1 text-xs text-gray-500"><summary class="cursor-pointer">{{ t('governance.frozenBalance') }} / {{ t('governance.usedBalance') }}</summary><p data-test="account-frozen" class="mt-1 break-all tabular-nums">{{ amount(snapshot?.catalog.account?.frozen_balance) }}</p><p data-test="account-used" class="mt-0.5 break-all tabular-nums">{{ amount(snapshot?.catalog.account?.used_balance) }}</p></details>
    </div>
    <div class="min-w-0 bg-white p-3 dark:bg-dark-800">
      <dt class="text-xs text-gray-500">{{ t('governance.visibleGroups') }}</dt>
      <dd class="mt-1 text-lg font-semibold tabular-nums">{{ snapshot?.catalog.groups.length ?? '—' }}<span class="ml-1.5 text-sm font-normal text-gray-400">/ {{ bindingCount }} {{ t('governance.imported') }}</span></dd>
    </div>
    <div class="min-w-0 bg-white p-3 dark:bg-dark-800">
      <dt class="text-xs text-gray-500">{{ t('governance.snapshot') }}</dt>
      <dd class="mt-1 whitespace-nowrap text-sm font-medium tabular-nums">{{ formatGovernanceTime(snapshot?.created_at || site.last_sync_at) }}</dd>
      <p data-test="site-cadence" class="mt-0.5 truncate text-xs text-gray-500">{{ site.enabled ? cadenceText(site, t) : t('governance.autoOff') }}</p>
    </div>
    <div class="min-w-0 bg-white p-3 dark:bg-dark-800">
      <dt class="text-xs text-gray-500">{{ t('governance.overviewPage.status') }}</dt>
      <dd class="mt-1 flex items-center gap-2"><StatusIndicator :label="t('governance.' + (siteStateKeys[site.status] || 'unknown'))" :tone="site.status === 'healthy' ? 'success' : site.status === 'error' ? 'danger' : 'warning'" /><StatusIndicator :label="t(site.has_credential ? 'governance.overviewPage.authorized' : 'governance.overviewPage.notAuthorized')" :tone="site.has_credential ? 'success' : 'neutral'" /></dd>
    </div>
  </dl>
  <details v-if="readinessComplete" data-test="readiness-complete" class="rounded-xl border border-gray-200 px-4 py-3 text-sm dark:border-dark-600">
    <summary class="cursor-pointer text-gray-600 dark:text-gray-300"><span class="mr-2 inline-block h-2 w-2 rounded-full bg-emerald-500" aria-hidden="true" />{{ t('governance.overviewPage.readinessComplete', { count: readiness?.checks.length ?? 0 }) }}</summary>
    <ReadinessChecklist class="mt-3" :overview="readiness" :loading="readinessLoading" :read-failed="readinessReadFailed" @navigate="emit('navigate', $event)" @retry="emit('retryReadiness')" />
  </details>
  <ReadinessChecklist v-else :overview="readiness" :loading="readinessLoading" :read-failed="readinessReadFailed" @navigate="emit('navigate', $event)" @retry="emit('retryReadiness')" />
</template>
