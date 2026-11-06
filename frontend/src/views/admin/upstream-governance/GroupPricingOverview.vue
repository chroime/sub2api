<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { RemoteGroup } from '@/api/admin/upstream-governance'
import PriceDetails from './PriceDetails.vue'
import { formatGovernanceRate, formatGovernanceTime } from './format'
import { providerLabel } from './providers'

const props = defineProps<{ groups: RemoteGroup[]; collectedAt?: string | null }>()
const emit = defineEmits<{ history: [] }>()
const { t } = useI18n()
const query = ref('')
const expanded = ref('')

// A model name match keeps the group, so "deepseek" finds every group selling it.
const visible = computed(() => {
  const needle = query.value.trim().toLowerCase()
  if (!needle) return props.groups
  return props.groups.filter(group =>
    `${group.name} ${group.id}`.toLowerCase().includes(needle)
    || (group.models || []).some(model => model.toLowerCase().includes(needle)),
  )
})
const platformName = (platform: string) => platform && platform !== 'unknown' ? providerLabel(platform) : ''
</script>
<template>
  <section data-test="group-pricing-overview" class="min-w-0 space-y-3">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h3 class="font-semibold">{{ t('governance.pricingTab.title') }} <span class="ml-1 text-sm font-normal text-gray-400">{{ groups.length }}</span></h3>
        <p class="mt-1 text-xs text-gray-500">{{ t('governance.pricingTab.hint', { time: formatGovernanceTime(collectedAt) }) }}</p>
      </div>
      <button data-test="pricing-history" type="button" class="btn btn-secondary text-xs" @click="emit('history')">{{ t('governance.pricingTab.rateHistory') }}</button>
    </div>
    <div class="relative min-w-0">
      <Icon name="search" size="sm" class="absolute left-3 top-2.5 text-gray-400" />
      <input v-model="query" data-test="pricing-group-search" class="input w-full py-2 pl-9 text-sm" :placeholder="t('governance.pricingTab.search')" :aria-label="t('governance.pricingTab.search')" />
    </div>
    <p v-if="!groups.length" class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-600">{{ t('governance.pricingTab.empty') }}</p>
    <div v-else class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600">
      <div v-for="group in visible" :key="group.id" data-test="pricing-group" class="border-b border-gray-100 last:border-b-0 dark:border-dark-700">
        <button type="button" class="flex w-full min-w-0 items-center gap-3 px-4 py-2.5 text-left hover:bg-gray-50 dark:hover:bg-dark-700" :aria-expanded="expanded === group.id" @click="expanded = expanded === group.id ? '' : group.id">
          <span class="min-w-0 flex-1">
            <span class="block truncate text-sm font-medium">{{ group.name }}</span>
            <span class="block truncate text-xs text-gray-500">#{{ group.id }}<template v-if="platformName(group.platform)"> · {{ platformName(group.platform) }}</template> · {{ t('governance.modelSelectedCount', { count: group.models?.length || 0 }) }} · {{ t('governance.pricingTab.priceCount', { count: group.prices?.length || 0 }) }}</span>
          </span>
          <span class="shrink-0 text-right">
            <span data-test="pricing-group-rate" class="block text-sm font-semibold tabular-nums text-primary-700 dark:text-primary-300">{{ formatGovernanceRate(group.resolved_rate_multiplier) }}×</span>
            <span v-if="group.peak_rate_enabled" class="block text-[11px] text-amber-600">{{ t('governance.peak') }} {{ formatGovernanceRate(group.peak_rate_multiplier) }}×</span>
          </span>
          <Icon :name="expanded === group.id ? 'chevronUp' : 'chevronDown'" size="sm" class="shrink-0 text-gray-400" />
        </button>
        <div v-if="expanded === group.id" class="bg-gray-50/70 px-4 pb-4 pt-1 dark:bg-dark-900/60">
          <p class="mb-2 text-xs text-gray-500">{{ t('governance.baseRate') }} {{ formatGovernanceRate(group.rate_multiplier) }} · {{ t('governance.overrideRate') }} {{ formatGovernanceRate(group.user_rate_multiplier) }}<template v-if="group.peak_rate_enabled"> · {{ group.peak_start }}–{{ group.peak_end }}</template></p>
          <PriceDetails :prices="group.prices" open />
        </div>
      </div>
      <p v-if="!visible.length" class="p-5 text-center text-sm text-gray-500">{{ t('governance.noMatchingGroups') }}</p>
    </div>
  </section>
</template>
