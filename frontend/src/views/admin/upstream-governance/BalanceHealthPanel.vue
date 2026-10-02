<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { BalanceHealth } from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'
const props = defineProps<{ health: BalanceHealth | null; disabled?: boolean }>()
const emit = defineEmits<{ reload: []; configure: [] }>()
const { t } = useI18n()
const reasonKeys: Record<string, string> = { healthy: 'balanceHealthHealthy', low: 'balanceHealthLow', disabled: 'balanceHealthDisabled', collection_disabled: 'balanceCollectionDisabled', session_unavailable: 'balanceSessionUnavailable', reauth_required: 'reauth', collection_failed: 'balanceCollectionFailed', balance_unavailable: 'balanceUnavailable', balance_unit_changed: 'balanceUnitChanged', balance_stale: 'balanceStale', snapshot_outdated: 'balanceSnapshotOutdated' }
const deliveryKeys: Record<string, string> = { ready: 'mailConfigurationReady', smtp_not_configured: 'mailConfigurationMissing', smtp_invalid: 'mailConfigurationInvalid', recipients_unavailable: 'recipientsUnavailable', email_unavailable: 'emailUnavailable' }
const explanation = computed(() => t('governance.' + (reasonKeys[props.health?.reason || ''] || 'balanceHealthUnavailable')))
</script>
<template>
  <section class="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5" data-test="balance-health">
    <div class="flex flex-wrap items-start gap-3"><div class="min-w-0 flex-1"><h3 class="font-semibold">{{ t('governance.balanceHealthTitle') }}</h3><p class="mt-1 text-sm leading-relaxed text-gray-500">{{ explanation }}</p></div><button class="btn btn-secondary text-sm" type="button" :disabled="disabled" @click="emit('reload')">{{ t('common.refresh') }}</button></div>
    <template v-if="health"><dl class="mt-4 grid gap-4 text-xs sm:grid-cols-3"><div><dt class="text-gray-500">{{ t('governance.balanceObservedAt') }}</dt><dd class="mt-1 tabular-nums">{{ formatGovernanceTime(health.observed_at) }}</dd><p v-if="health.stale" data-test="balance-stale" class="mt-1 text-amber-700 dark:text-amber-400">{{ t('governance.balanceStaleLabel') }}</p></div><div><dt class="text-gray-500">{{ t('governance.nextCollection') }}</dt><dd class="mt-1 tabular-nums">{{ formatGovernanceTime(health.next_run_at) }}</dd></div><div><dt class="text-gray-500">{{ t('governance.mailReadiness') }}</dt><dd class="mt-1" :class="health.delivery_ready ? 'text-primary-700 dark:text-primary-300' : 'text-amber-700 dark:text-amber-400'">{{ t('governance.' + (deliveryKeys[health.delivery_reason] || (health.delivery_ready ? 'mailConfigurationReady' : 'mailConfigurationMissing'))) }}</dd><p class="mt-1 text-gray-500">{{ t('governance.recipientCount', { count: health.recipient_count }) }}</p></div></dl><p v-if="health.last_delivery_error" role="alert" class="mt-3 text-sm text-amber-700 dark:text-amber-400">{{ t('governance.emailDeliveryFailed') }}</p></template>
    <div class="mt-4 flex flex-wrap items-center gap-3 text-xs"><button type="button" class="font-medium text-primary-700 dark:text-primary-300" :disabled="disabled" @click="emit('configure')">{{ t('governance.configureBalance') }}</button><a href="/admin/settings" class="text-gray-500 underline underline-offset-2">{{ t('governance.systemMailSettings') }}</a><span class="text-gray-500">{{ t('governance.mailReadinessHint') }}</span></div>
  </section>
</template>
