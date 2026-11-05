<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import Select from '@/components/common/Select.vue'
import type { Site, SiteBalance } from '@/api/admin/upstream-governance'
import sub2apiLogo from '@/assets/governance/sub2api.svg'
import newapiLogo from '@/assets/governance/newapi.png'
import { formatGovernanceTime } from './format'
import { siteStateKeys } from './feedback'
const props = withDefaults(defineProps<{
  sites: Site[]
  disabled?: boolean
  compact?: boolean
  selectedSiteId?: number | null
}>(), {
  compact: false,
  selectedSiteId: null
})
const emit = defineEmits<{ select: [site: Site] }>()
const { t } = useI18n()
type SiteFilter = 'all' | 'attention' | 'paused'
const query = ref(''), filter = ref<SiteFilter>('all')
const needsAttention = (site: Site) => !!site.last_error || site.status === 'reauth_required' || site.balance_monitor_status?.state === 'low' || !!site.balance_monitor_status?.last_error || !!site.key_issue_count
const attention = computed(() => props.sites.filter(needsAttention).length)
const collecting = computed(() => props.sites.filter(site => site.enabled).length)
const ordered = computed(() => [...props.sites].sort((a, b) => Number(needsAttention(b)) - Number(needsAttention(a)) || a.id - b.id))
const visible = computed(() => ordered.value.filter(site => `${site.name} ${site.base_url}`.toLowerCase().includes(query.value.trim().toLowerCase()) && (filter.value === 'all' || (filter.value === 'attention' ? needsAttention(site) : !site.enabled))))
const filters = computed<{ id: SiteFilter; count: number }[]>(() => [
  { id: 'all', count: props.sites.length },
  { id: 'attention', count: attention.value },
  { id: 'paused', count: props.sites.length - collecting.value },
])

const platformName = (site: Site) => site.platform === 'sub2api' ? 'Sub2API' : 'New API'
const platformLogo = (site: Site) => site.platform === 'sub2api' ? sub2apiLogo : newapiLogo

const usd = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', minimumFractionDigits: 2, maximumFractionDigits: 2 })
const compact = new Intl.NumberFormat('en-US', { notation: 'compact', maximumFractionDigits: 1 })
// New API quota is not a currency, so it is never converted; it is only
// abbreviated and labelled with its unit.
function balanceText(balance: SiteBalance): string {
  if (balance.unit === 'usd') return usd.format(balance.amount)
  const unit = balance.unit === 'quota' ? ` ${t('governance.siteRail.quotaUnit')}` : balance.unit ? ` ${balance.unit}` : ''
  return compact.format(balance.amount) + unit
}
function balanceClass(site: Site): string {
  if ((site.balance?.amount ?? 0) < 0) return 'text-red-600 dark:text-red-400'
  if (site.balance_monitor_status?.state === 'low') return 'text-amber-700 dark:text-amber-400'
  return 'text-gray-700 dark:text-gray-200'
}
const collection = (site: Site) => site.enabled ? t('governance.siteRail.every', { n: site.interval_minutes }) : t('governance.siteRail.collectionPaused')
interface SiteIssue { key: string; text: string; severe: boolean; test?: string }
// Only states that need an administrator are spelled out; healthy sites show
// their collection cadence instead.
function issues(site: Site): SiteIssue[] {
  const out: SiteIssue[] = []
  if (site.status === 'reauth_required') out.push({ key: 'reauth', text: t('governance.stateReauth'), severe: true })
  else if (site.last_error || site.status === 'error') out.push({ key: 'error', text: t('governance.stateError'), severe: true })
  else if (site.status === 'disconnected') out.push({ key: 'disconnected', text: t('governance.stateDisconnected'), severe: false })
  if (site.key_issue_count) out.push({ key: 'keys', text: t('governance.keyIssues', { count: site.key_issue_count }), severe: true, test: 'site-key-issues' })
  if (site.balance_monitor_status?.state === 'low') out.push({ key: 'low', text: t('governance.balanceState_low'), severe: false })
  if (site.balance_monitor_status?.last_error) out.push({ key: 'balance', text: t('governance.siteRail.balanceCheckFailed'), severe: false })
  return out
}
function statusDot(site: Site): string {
  const found = issues(site)
  if (found.some(issue => issue.severe)) return 'bg-red-500'
  if (found.length) return 'bg-amber-500'
  if (!site.enabled) return 'bg-gray-300 dark:bg-dark-500'
  return 'bg-emerald-500'
}
// Clamped at "just now": a sync stamped by the server can sit a few seconds
// ahead of this browser's clock.
function lastSync(value: string | null): string {
  if (!value) return t('common.time.never')
  const elapsed = Date.now() - new Date(value).getTime()
  if (!Number.isFinite(elapsed)) return '—'
  const minutes = Math.floor(Math.max(elapsed, 0) / 60_000)
  if (minutes < 1) return t('common.time.justNow')
  if (minutes < 60) return t('common.time.minutesAgo', { n: minutes })
  const hours = Math.floor(minutes / 60)
  return hours < 24 ? t('common.time.hoursAgo', { n: hours }) : t('common.time.daysAgo', { n: Math.floor(hours / 24) })
}
const siteTooltip = (site: Site) => [
  site.name,
  site.base_url,
  `${platformName(site)} · ${t(site.enabled ? 'governance.autoOn' : 'governance.autoOff')}${site.enabled ? ` · ${site.interval_minutes} ${t('governance.minutes')}` : ''}`,
  t('governance.siteRail.lastSync', { time: formatGovernanceTime(site.last_sync_at) }),
  site.balance ? t('governance.siteRail.balanceAt', { amount: `${site.balance.amount} ${site.balance.unit}`.trim(), time: formatGovernanceTime(site.balance.observed_at) }) : '',
].filter(Boolean).join('\n')
const pickerOptions = computed(() => ordered.value.map(site => ({
  value: site.id,
  label: [site.name, site.balance ? balanceText(site.balance) : '', needsAttention(site) ? t('governance.siteFilter_attention') : '', site.enabled ? '' : t('governance.siteRail.collectionPaused')].filter(Boolean).join(' · '),
})))
function pick(value: string | number | boolean | null) {
  const site = props.sites.find(item => item.id === value)
  if (site && !props.disabled) emit('select', site)
}
</script>
<template>
  <section v-if="compact" class="min-w-0 rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800" data-test="sites-overview-compact">
    <header class="flex items-center justify-between gap-3">
      <h2 class="truncate text-sm font-semibold">{{ t('governance.upstreamSites') }}</h2>
      <span data-test="site-count" class="shrink-0 rounded-md bg-gray-100 px-2 py-0.5 text-xs tabular-nums text-gray-500 dark:bg-dark-700">{{ sites.length }}</span>
    </header>
    <!-- Narrow layouts stack the rail above the workspace, so a picker replaces the list there. -->
    <Select class="mt-2 xl:hidden" data-test="site-picker" :model-value="selectedSiteId" :options="pickerOptions" :disabled="disabled || !sites.length" :placeholder="t('governance.siteRail.choose')" :aria-label="t('governance.siteRail.choose')" @update:model-value="pick" />
    <div class="hidden xl:block">
      <div class="mt-2 space-y-2">
        <div class="relative min-w-0">
          <Icon name="search" size="sm" class="absolute left-2.5 top-2 text-gray-400" />
          <input v-model="query" data-test="site-search" class="input w-full py-1.5 pl-8 text-sm" :placeholder="t('governance.searchSites')" :aria-label="t('governance.searchSites')" />
        </div>
        <div class="flex gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-700">
          <button v-for="option in filters" :key="option.id" type="button" :data-test="'site-filter-' + option.id" class="flex min-h-8 min-w-0 flex-1 items-center justify-center gap-1 rounded-md px-1.5 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" :class="filter === option.id ? 'bg-white font-medium text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500'" :aria-pressed="filter === option.id" @click="filter = option.id"><span class="truncate">{{ option.id === 'paused' ? t('governance.siteRail.filterPaused') : t(`governance.siteFilter_${option.id}`) }}</span><span class="shrink-0 tabular-nums" :class="option.id === 'attention' && option.count ? 'text-amber-700 dark:text-amber-400' : 'text-gray-400'">{{ option.count }}</span></button>
        </div>
      </div>
      <nav :aria-label="t('governance.upstreamSites')" data-test="site-list" class="mt-2 max-h-64 overflow-y-auto overscroll-contain rounded-lg border border-gray-200 dark:border-dark-600 xl:max-h-[calc(100dvh-22rem)]">
        <button v-for="site in visible" :id="'governance-site-' + site.id" :key="site.id" type="button" class="site-row block w-full min-w-0 border-b border-l-2 border-b-gray-100 px-3 py-2 text-left transition-colors last:border-b-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-60 dark:border-b-dark-700" :class="selectedSiteId === site.id ? 'site-row--selected border-l-primary-500 bg-primary-50 dark:bg-primary-900/20' : 'border-l-transparent hover:bg-gray-50 dark:hover:bg-dark-700'" :aria-current="selectedSiteId === site.id ? 'true' : undefined" :title="siteTooltip(site)" :disabled="disabled" @click="emit('select', site)">
          <span class="flex min-w-0 items-center gap-1.5">
            <span data-test="site-status-dot" class="h-2 w-2 shrink-0 rounded-full" :class="statusDot(site)" aria-hidden="true" />
            <img data-test="site-platform-logo" :src="platformLogo(site)" :alt="platformName(site)" class="h-4 w-4 shrink-0 rounded" width="16" height="16" />
            <span class="min-w-0 flex-1 truncate text-sm font-medium">{{ site.name }}</span>
            <span v-if="site.balance" data-test="site-balance" class="shrink-0 text-xs font-semibold tabular-nums" :class="balanceClass(site)">{{ balanceText(site.balance) }}</span>
          </span>
          <span class="mt-0.5 flex min-w-0 items-center gap-1.5 pl-[1.625rem] text-xs">
            <span v-for="issue in issues(site)" :key="issue.key" :data-test="issue.test" class="shrink-0 font-medium" :class="issue.severe ? 'text-red-600 dark:text-red-400' : 'text-amber-700 dark:text-amber-400'">{{ issue.text }}</span>
            <span class="min-w-0 flex-1 truncate text-gray-500 dark:text-dark-300">{{ collection(site) }}</span>
            <time class="shrink-0 text-[11px] tabular-nums text-gray-400" :datetime="site.last_sync_at || undefined">{{ lastSync(site.last_sync_at) }}</time>
          </span>
        </button>
        <p v-if="!visible.length" class="p-5 text-center text-sm text-gray-500">{{ t(sites.length ? 'governance.noMatchingSites' : 'governance.empty') }}</p>
      </nav>
    </div>
  </section>
  <section v-else class="min-w-0 space-y-5" data-test="sites-overview">
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
