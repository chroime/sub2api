<template>
  <div class="space-y-6">
    <div
      v-if="loading && items.length === 0"
      class="monitor-card-grid"
    >
      <div
        v-for="i in 6"
        :key="i"
        class="min-h-[300px] animate-pulse rounded-3xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800"
      >
        <div class="flex items-start gap-3">
          <div class="w-9 h-9 rounded-xl bg-gray-200 dark:bg-dark-700"></div>
          <div class="flex-1 space-y-2">
            <div class="h-4 w-2/3 rounded bg-gray-200 dark:bg-dark-700"></div>
            <div class="h-3 w-1/2 rounded bg-gray-200 dark:bg-dark-700"></div>
          </div>
          <div class="h-6 w-16 rounded-full bg-gray-200 dark:bg-dark-700"></div>
        </div>
        <div class="mt-5 grid grid-cols-3 gap-2">
          <div v-for="metric in 3" :key="metric" class="h-16 rounded-xl bg-gray-100 dark:bg-dark-900/40"></div>
        </div>
        <div class="mt-6 h-5 w-full rounded bg-gray-100 dark:bg-dark-900/40"></div>
      </div>
    </div>

    <EmptyState
      v-else-if="items.length === 0"
      :title="t('channelStatus.empty.title')"
      :description="t('channelStatus.empty.description')"
    />

    <template v-else>
      <section v-for="group in providerGroups" :key="group.provider" :data-provider="group.provider" class="min-w-0 space-y-3">
        <header class="flex items-center gap-2.5">
          <span class="grid h-7 w-7 shrink-0 place-items-center rounded-lg" :class="providerBadgeClass(group.provider)">
            <ProviderIcon :provider="group.provider" :size="18" />
          </span>
          <h2 class="text-base font-semibold text-gray-900 dark:text-gray-100">{{ providerLabel(group.provider) }}</h2>
          <span data-testid="provider-count" class="inline-flex min-w-6 items-center justify-center rounded-full bg-gray-200/70 px-2 py-0.5 text-xs font-semibold tabular-nums text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ group.items.length }}</span>
        </header>
        <div class="monitor-card-grid">
          <MonitorCard
            v-for="item in group.items"
            :key="item.id"
            :item="item"
            :window="window"
            :availability-value="resolveAvailability(item)"
            :countdown-seconds="countdownSeconds"
            @click="emit('cardClick', item)"
          />
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView, UserMonitorDetail } from '@/api/channelMonitor'
import EmptyState from '@/components/common/EmptyState.vue'
import MonitorCard from './MonitorCard.vue'
import ProviderIcon from './ProviderIcon.vue'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'

const props = defineProps<{
  items: UserMonitorView[]
  window: '7d' | '15d' | '30d'
  countdownSeconds: number
  loading: boolean
  detailCache: Record<number, UserMonitorDetail>
}>()

const emit = defineEmits<{
  (e: 'cardClick', item: UserMonitorView): void
}>()

const { t } = useI18n()
const { providerLabel, providerBadgeClass } = useChannelMonitorFormat()

// Keep server ordering within each platform and use its first appearance for
// the section order; do not sort by name, health, or response time.
const providerGroups = computed(() => {
  const groups = new Map<string, { provider: UserMonitorView['provider']; items: UserMonitorView[] }>()
  for (const item of props.items) {
    const group = groups.get(item.provider)
    if (group) group.items.push(item)
    else groups.set(item.provider, { provider: item.provider, items: [item] })
  }
  return [...groups.values()]
})

function resolveAvailability(item: UserMonitorView): number | null {
  if (props.window === '7d') {
    return item.availability_7d ?? null
  }
  const detail = props.detailCache[item.id]
  if (!detail) return null
  const primary = detail.models.find(m => m.model === item.primary_model)
  if (!primary) return null
  return props.window === '15d' ? primary.availability_15d ?? null : primary.availability_30d ?? null
}
</script>

<style scoped>
.monitor-card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, max(17.5rem, calc((100% - 4rem) / 5))), 1fr));
  gap: 1rem;
}
</style>
