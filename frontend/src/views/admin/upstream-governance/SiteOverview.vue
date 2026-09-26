<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { Site, Snapshot } from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'
const props = defineProps<{
  site: Site
  snapshot: Snapshot | null
  bindingCount: number
}>()
const { t } = useI18n()
const amount = (value: number | null | undefined) =>
  value == null
    ? t('governance.unknown')
    : `${Number(value.toFixed(6))} ${props.snapshot?.catalog.account?.unit || ''}`
</script>
<template>
  <dl class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
    <div
      class="rounded-xl border border-primary-200 bg-gradient-to-br from-primary-50 to-white p-4 dark:border-primary-800/50 dark:from-primary-900/20 dark:to-dark-800"
    >
      <dt class="text-xs text-primary-700 dark:text-primary-300">
        {{ t('governance.balance') }}
      </dt>
      <dd
        data-test="account-balance"
        class="mt-2 text-2xl font-semibold tabular-nums tracking-tight"
      >
        {{ amount(snapshot?.catalog.account?.balance) }}
      </dd>
      <p class="mt-2 truncate text-xs text-gray-500">
        {{
          snapshot?.catalog.account?.username ||
          snapshot?.catalog.account?.email ||
          t('governance.upstreamAccount')
        }}
      </p>
    </div>
    <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
      <dt class="text-xs text-gray-500">{{ t('governance.visibleGroups') }}</dt>
      <dd class="mt-2 text-2xl font-semibold tabular-nums">
        {{ snapshot?.catalog.groups.length ?? '—'
        }}<span class="ml-2 text-sm font-normal text-gray-400"
          >/ {{ bindingCount }} {{ t('governance.imported') }}</span
        >
      </dd>
      <p class="mt-2 text-xs text-gray-500">
        {{ t('governance.catalogSummary') }}
      </p>
    </div>
    <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
      <dt class="text-xs text-gray-500">
        {{ t('governance.frozenBalance') }} / {{ t('governance.usedBalance') }}
      </dt>
      <dd
        data-test="account-frozen"
        class="mt-2 text-base font-semibold tabular-nums"
      >
        {{ amount(snapshot?.catalog.account?.frozen_balance) }}
      </dd>
      <p
        data-test="account-used"
        class="mt-1 text-sm tabular-nums text-gray-500"
      >
        {{ amount(snapshot?.catalog.account?.used_balance) }}
      </p>
    </div>
    <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
      <dt class="text-xs text-gray-500">{{ t('governance.snapshot') }}</dt>
      <dd class="mt-3 whitespace-nowrap text-sm font-medium tabular-nums">
        {{ formatGovernanceTime(snapshot?.created_at || site.last_sync_at) }}
      </dd>
      <p class="mt-3 text-xs text-gray-500">
        {{ site.enabled ? t('governance.autoOn') : t('governance.autoOff') }} ·
        {{ site.interval_minutes }} {{ t('governance.minutes') }}
      </p>
    </div>
  </dl>
</template>
