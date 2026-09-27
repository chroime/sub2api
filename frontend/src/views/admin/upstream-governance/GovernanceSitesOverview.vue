<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { Site } from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'
import { siteStateKeys } from './feedback'
const props = defineProps<{ sites: Site[]; disabled?: boolean }>()
const emit = defineEmits<{ select: [site: Site] }>()
const { t } = useI18n()
const query = ref(''), filter = ref<'all' | 'attention' | 'paused'>('all')
const needsAttention = (site: Site) => !!site.last_error || site.status === 'reauth_required' || site.balance_monitor_status?.state === 'low' || !!site.balance_monitor_status?.last_error
const attention = computed(() => props.sites.filter(needsAttention).length)
const collecting = computed(() => props.sites.filter(site => site.enabled).length)
const visible = computed(() => props.sites.filter(site => `${site.name} ${site.base_url}`.toLowerCase().includes(query.value.trim().toLowerCase()) && (filter.value === 'all' || (filter.value === 'attention' ? needsAttention(site) : !site.enabled))).sort((a, b) => Number(needsAttention(b)) - Number(needsAttention(a)) || a.id - b.id))
</script>
<template>
  <section class="min-w-0 space-y-5" data-test="sites-overview">
    <dl class="grid grid-cols-3 gap-2 sm:gap-4"><div v-for="metric in [{ key: 'siteTotal', value: sites.length }, { key: 'sitesNeedAttention', value: attention }, { key: 'sitesCollecting', value: collecting }]" :key="metric.key" class="min-w-0 rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800 sm:p-5"><dt class="text-xs text-gray-500 sm:text-sm">{{ t('governance.' + metric.key) }}</dt><dd class="mt-2 text-2xl font-semibold tabular-nums" :class="metric.key === 'sitesNeedAttention' && metric.value ? 'text-amber-700 dark:text-amber-400' : ''">{{ metric.value }}</dd></div></dl>
    <div class="flex flex-wrap items-center gap-3"><div class="relative min-w-0 basis-full sm:max-w-sm sm:flex-1"><Icon name="search" size="sm" class="absolute left-3 top-3 text-gray-400" /><input v-model="query" data-test="site-search" class="input w-full pl-9 text-sm" :placeholder="t('governance.searchSites')" :aria-label="t('governance.searchSites')" /></div><div class="flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800"><button v-for="option in (['all', 'attention', 'paused'] as const)" :key="option" type="button" class="min-h-9 rounded-md px-3 text-sm transition-colors" :class="filter === option ? 'bg-white font-medium text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500'" :aria-pressed="filter === option" @click="filter = option">{{ t(`governance.siteFilter_${option}`) }}</button></div></div>
    <nav :aria-label="t('governance.upstreamSites')" class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
      <button v-for="site in visible" :id="'governance-site-' + site.id" :key="site.id" type="button" class="grid w-full min-w-0 gap-3 border-b border-gray-100 p-4 text-left transition-colors last:border-b-0 hover:bg-gray-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 disabled:opacity-60 dark:border-dark-700 dark:hover:bg-dark-700 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-center sm:p-5" :disabled="disabled" @click="emit('select', site)">
        <div class="flex min-w-0 items-center gap-3"><span class="rounded-lg bg-primary-50 p-2 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"><Icon name="server" size="md" /></span><div class="min-w-0"><p class="truncate font-semibold">{{ site.name }}</p><p class="mt-1 truncate text-xs text-gray-500">{{ site.base_url }}</p></div></div>
        <div class="flex min-w-0 flex-wrap items-center gap-2 text-xs"><span class="rounded-full px-2.5 py-1 font-medium" :class="needsAttention(site) ? 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300'">{{ t('governance.' + (siteStateKeys[site.status] || 'unknown')) }}</span><span v-if="site.balance_monitor_status?.state === 'low'" class="text-amber-700 dark:text-amber-400">{{ t('governance.balanceState_low') }}</span><span class="uppercase text-gray-500">{{ site.platform }}</span><p class="basis-full text-gray-500">{{ t(site.enabled ? 'governance.autoOn' : 'governance.autoOff') }}<span v-if="site.enabled"> · {{ site.interval_minutes }} {{ t('governance.minutes') }}</span></p></div>
        <div class="flex min-w-0 items-center justify-between gap-4 sm:justify-end"><time class="text-xs tabular-nums text-gray-500">{{ formatGovernanceTime(site.last_sync_at) }}</time><Icon name="arrowRight" size="sm" class="shrink-0 text-gray-400" /></div>
      </button><p v-if="!visible.length" class="p-8 text-center text-sm text-gray-500">{{ t('governance.noMatchingSites') }}</p>
    </nav>
  </section>
</template>
