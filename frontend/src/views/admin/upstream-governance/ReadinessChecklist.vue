<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ReadinessCheck, ReadinessOverview } from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'

const props = withDefaults(defineProps<{
  overview?: ReadinessOverview | null
  disabled?: boolean
}>(), {
  overview: null,
  disabled: false,
})

const emit = defineEmits<{
  navigate: [target: ReadinessCheck['target_tab']]
}>()

const { t } = useI18n()
const items = computed(() => props.overview?.checks ?? [])

const stateClass: Record<ReadinessCheck['state'], string> = {
  configured: 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300',
  not_configured: 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300',
  not_enabled: 'bg-slate-100 text-slate-600 dark:bg-dark-700 dark:text-gray-300',
  pending: 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300',
  read_failed: 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300',
}

function itemLabel(item: ReadinessCheck) {
  return t('governance.readiness.items.' + item.key)
}
function itemDetail(item: ReadinessCheck) {
  const key = 'governance.readiness.details.' + item.detail
  const translated = t(key)
  return translated === key ? t('governance.readiness.details.unknown') : translated
}
function stateLabel(item: ReadinessCheck) {
  return t('governance.readiness.states.' + item.state)
}
function countLabel(item: ReadinessCheck) {
  const count = item.count ?? 0
  if (!Number.isFinite(count) || count <= 0) return ''
  if (item.key === 'bindings') return t('governance.readiness.counts.bindings', { count })
  if (item.key === 'managed_keys') return t('governance.readiness.counts.managed_keys', { count })
  return ''
}
</script>

<template>
  <section data-test="readiness-checklist" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h3 class="font-semibold">{{ t('governance.readiness.title') }}</h3>
        <p class="mt-1 max-w-3xl text-sm leading-relaxed text-gray-500 dark:text-gray-400">{{ t('governance.readiness.description') }}</p>
      </div>
      <time v-if="overview" class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400" :datetime="overview.evaluated_at">{{ t('governance.readiness.evaluatedAt', { time: formatGovernanceTime(overview.evaluated_at) }) }}</time>
    </header>
    <p v-if="!overview" role="status" class="mt-4 rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-900 dark:text-gray-400">{{ t('governance.readiness.notLoaded') }}</p>
    <div v-else class="mt-4 grid gap-3 md:grid-cols-2">
      <article v-for="item in items" :key="item.key" data-test="readiness-item" class="min-w-0 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <div class="flex items-start gap-3">
          <span class="mt-0.5 h-2.5 w-2.5 shrink-0 rounded-full" :class="item.state === 'configured' ? 'bg-emerald-500' : item.state === 'pending' ? 'bg-amber-500' : item.state === 'read_failed' ? 'bg-red-500' : 'bg-gray-400'" aria-hidden="true"></span>
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <h4 class="text-sm font-medium">{{ itemLabel(item) }}</h4>
              <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="stateClass[item.state]">{{ stateLabel(item) }}</span>
            </div>
            <p class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ itemDetail(item) }}</p>
            <p v-if="countLabel(item)" class="mt-1 text-xs font-medium text-gray-600 dark:text-gray-300">{{ countLabel(item) }}</p>
          </div>
          <button v-if="item.target_tab" data-test="readiness-navigate" :data-target="item.target_tab" type="button" class="btn btn-secondary shrink-0 text-xs" :disabled="disabled" @click="emit('navigate', item.target_tab)">{{ t('governance.readiness.open') }}</button>
        </div>
      </article>
    </div>
  </section>
</template>
