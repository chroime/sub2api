<template>
  <section aria-labelledby="public-channel-status-title" class="border-b border-white/10 bg-[#0d141b] text-slate-100">
    <div class="mx-auto max-w-7xl px-5 py-16 sm:px-8 sm:py-20 lg:px-10">
      <div class="flex flex-col gap-5 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h2 id="public-channel-status-title" class="mt-2 text-3xl font-semibold text-white">{{ t('home.public.channelStatus.title') }}</h2>
          <p class="mt-3 max-w-2xl text-sm leading-6 text-slate-400">{{ t('home.public.channelStatus.description') }}</p>
        </div>
        <p v-if="status === 'ready' && rows.length" class="flex items-center gap-2 text-sm text-slate-400" role="status">
          <span class="h-2 w-2 rounded-full bg-emerald-300" aria-hidden="true" />
          {{ t('home.public.channelStatus.platformsAvailable', { count: rows.length }) }}
        </p>
      </div>
      <div class="mt-8 overflow-hidden rounded-lg border border-white/10 bg-[#111a22]">
        <div v-if="status === 'loading'" class="space-y-3 p-5" aria-busy="true" :aria-label="t('home.public.channelStatus.loadingAria')">
          <div v-for="index in 4" :key="index" class="h-12 animate-pulse rounded bg-white/5 motion-reduce:animate-none" />
        </div>
        <template v-else-if="status === 'ready' && (rows.length || monitors.length)">
          <div v-if="monitors.length" class="dark monitor-card-grid p-5 sm:p-6">
            <MonitorCard
              v-for="monitor in monitors"
              :key="monitor.id"
              :item="monitor"
              window="7d"
              :availability-value="monitor.availability_7d"
              :countdown-seconds="30"
            />
          </div>
          <template v-else>
          <div class="hidden grid-cols-[minmax(0,1.5fr)_minmax(0,.8fr)_repeat(3,minmax(0,.7fr))] gap-4 border-b border-white/10 px-5 py-3 text-xs font-semibold text-slate-400 sm:grid">
            <span v-for="column in columns" :key="column">{{ t(`home.public.channelStatus.table.${column}`) }}</span>
          </div>
          <ul class="divide-y divide-white/10">
            <li v-for="row in rows" :key="row.platform" class="grid gap-3 px-5 py-4 sm:grid-cols-[minmax(0,1.5fr)_minmax(0,.8fr)_repeat(3,minmax(0,.7fr))] sm:items-center">
              <div class="min-w-0">
                <p class="break-words text-sm font-semibold capitalize text-white">{{ row.platform }}</p>
                <p class="mt-1 break-words text-xs text-slate-400">{{ channelSummary(row) }}</p>
              </div>
              <div class="flex items-center text-sm text-slate-300">
                <span class="mr-2 text-xs text-slate-400 sm:hidden">{{ t('home.public.channelStatus.table.status') }}</span>
                <span class="channel-status-badge">
                  <span class="channel-status-dot" aria-hidden="true" />
                  {{ t('home.public.channelStatus.active') }}
                </span>
              </div>
              <div v-for="column in countColumns" :key="column.key" class="text-sm text-slate-300">
                <span class="mr-2 text-xs text-slate-400 sm:hidden">{{ t(`home.public.channelStatus.table.${column.label}`) }}</span>{{ row[column.key] }}
              </div>
            </li>
          </ul>
          <div class="flex flex-wrap justify-between gap-3 border-t border-white/10 px-5 py-4 text-xs text-slate-400">
            <span>{{ t('home.public.channelStatus.namesNote') }}</span>
            <a v-if="detailsHref" :href="detailsHref" class="inline-flex items-center gap-2 text-cyan-200 hover:text-cyan-100">{{ t('home.public.channelStatus.viewFullStatus') }}<Icon name="arrowRight" size="xs" /></a>
          </div>
          </template>
        </template>
        <div v-else class="px-5 py-12 text-center" :role="status === 'error' ? 'alert' : 'status'">
          <Icon name="server" size="lg" class="mx-auto text-cyan-200" aria-hidden="true" />
          <h3 class="mt-4 text-base font-semibold text-white">{{ t(`home.public.channelStatus.${stateKey}Title`) }}</h3>
          <p class="mx-auto mt-2 max-w-md text-sm leading-6 text-slate-400">{{ status === 'error' ? (errorMessage || t('home.public.channelStatus.errorFallback')) : t(`home.public.channelStatus.${stateKey}Description`) }}</p>
          <button v-if="status === 'error' && retryable !== false" type="button" class="mt-5 inline-flex items-center gap-2 rounded-lg border border-white/15 px-4 py-2 text-sm hover:bg-white/5" @click="emit('retry')">
            <Icon name="refresh" size="sm" />{{ t('home.public.channelStatus.retry') }}
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import MonitorCard from '@/components/user/monitor/MonitorCard.vue'
import type { PublicPlatformHomeChannelRow, PublicPlatformHomeStatus } from '@/composables/usePublicPlatformHome'
import type { UserMonitorView } from '@/api/channelMonitor'

const props = defineProps<{
  channels?: MaybeRefOrGetter<PublicPlatformHomeChannelRow[]>
  rows?: MaybeRefOrGetter<PublicPlatformHomeChannelRow[]>
  monitors?: MaybeRefOrGetter<UserMonitorView[]>
  status?: MaybeRefOrGetter<PublicPlatformHomeStatus>
  errorMessage?: string
  retryable?: boolean
  detailsHref?: string
}>()
const { t } = useI18n()
const emit = defineEmits<{ retry: [] }>()
const rows = computed(() => toValue(props.channels ?? props.rows) || [])
const monitors = computed(() => toValue(props.monitors) || [])
const status = computed(() => toValue(props.status) || 'idle')
const stateKey = computed(() => status.value === 'ready' ? 'empty' : status.value)
const columns = ['platform', 'channels', 'groups', 'models']
const countColumns = [
  { key: 'channelCount', label: 'channels' },
  { key: 'groupCount', label: 'groups' },
  { key: 'modelCount', label: 'models' },
] as const

function channelSummary(row: PublicPlatformHomeChannelRow): string {
  if (!row.channelNames.length) return t('home.public.channelStatus.noNamedChannels')
  const names = row.channelNames.slice(0, 2).join(' / ')
  return row.channelNames.length <= 2 ? names : `${names} ${t('home.public.channelStatus.moreChannels', { count: row.channelNames.length - 2 })}`
}
</script>

<style scoped>
.channel-status-badge {
  display: inline-flex;
  align-items: center;
  gap: .375rem;
  border: 1px solid rgb(52 211 153 / .25);
  border-radius: .375rem;
  background: rgb(52 211 153 / .1);
  padding: .25rem .5rem;
  color: #a7f3d0;
  font-size: .6875rem;
  font-weight: 600;
  white-space: nowrap;
}
.channel-status-dot {
  height: .375rem;
  width: .375rem;
  border-radius: 999px;
  background: #34d399;
  box-shadow: 0 0 8px rgb(52 211 153 / .7);
}
.monitor-card-grid {
  display: grid;
  grid-template-columns: repeat(1, minmax(0, 1fr));
  gap: 1rem;
}
@media (min-width: 768px) {
  .monitor-card-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (min-width: 1280px) {
  .monitor-card-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); }
}
</style>
