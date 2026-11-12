<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { BalanceHealth, Site } from '@/api/admin/upstream-governance'
import BalanceHealthPanel from './BalanceHealthPanel.vue'
import BalanceMonitorPanel from './BalanceMonitorPanel.vue'
import { formatBalance, rawBalance } from './balance-format'

// One card for the site's balance: the current amount, collection and mail
// status, and the low-balance mail settings.
defineProps<{
  site: Site
  health: BalanceHealth | null
  balance?: number | null
  unit?: string
  quotaPerUsd?: number
  disabled?: boolean
}>()
const emit = defineEmits<{ reload: []; saved: [site: Site]; busy: [value: boolean] }>()
const { t } = useI18n()
</script>
<template>
  <section data-test="balance-card" class="min-w-0 rounded-xl border border-gray-200 dark:border-dark-600">
    <header class="flex flex-wrap items-baseline justify-between gap-3 border-b border-gray-100 px-4 py-3 dark:border-dark-700 sm:px-5">
      <h3 class="font-semibold">{{ t('governance.automationPage.balanceTitle') }}</h3>
      <p data-test="balance-card-amount" :title="rawBalance(balance, unit)" class="text-lg font-semibold tabular-nums">{{ formatBalance(balance, unit, quotaPerUsd, t('governance.unknown')) }}</p>
    </header>
    <div class="space-y-5 p-4 sm:p-5">
      <BalanceHealthPanel embedded :health="health" :disabled="disabled" @reload="emit('reload')" />
      <div class="border-t border-gray-100 pt-5 dark:border-dark-700">
        <BalanceMonitorPanel embedded :site="site" :unit="unit" :disabled="disabled" @saved="emit('saved', $event)" @busy="emit('busy', $event)" />
      </div>
    </div>
  </section>
</template>
