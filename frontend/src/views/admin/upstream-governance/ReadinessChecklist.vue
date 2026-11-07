<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ReadinessCheck, ReadinessOverview } from '@/api/admin/upstream-governance'
import { formatGovernanceTime } from './format'
import Icon from '@/components/icons/Icon.vue'
import StatusIndicator from './StatusIndicator.vue'

const props = withDefaults(defineProps<{
  overview?: ReadinessOverview | null
  disabled?: boolean
  loading?: boolean
  readFailed?: boolean
}>(), {
  overview: null,
  disabled: false,
  loading: false,
  readFailed: false,
})

const emit = defineEmits<{
  navigate: [target: ReadinessCheck['target_tab']]
  retry: []
}>()

const { t } = useI18n()
const items = computed(() => props.overview?.checks ?? [])

const stateTone = {
  configured: 'success',
  not_configured: 'neutral',
  not_enabled: 'neutral',
  pending: 'warning',
  read_failed: 'danger',
} as const

function itemLabel(item: ReadinessCheck) {
  return t('governance.readiness.items.' + item.key)
}
function itemDetail(item: ReadinessCheck) {
  if (!item.detail) return ''
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
      </div>
      <time v-if="overview" class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400" :datetime="overview.evaluated_at">{{ t('governance.readiness.evaluatedAt', { time: formatGovernanceTime(overview.evaluated_at) }) }}</time>
    </header>
    <p v-if="loading" data-test="readiness-loading" role="status" class="mt-4 rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-900 dark:text-gray-400">{{ t('governance.readiness.loading') }}</p>
    <div v-else-if="readFailed" data-test="readiness-read-failed" role="alert" class="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-900/10 dark:text-red-300">
      <span>{{ t('governance.readiness.readFailed') }}</span>
      <button data-test="readiness-retry" type="button" class="btn btn-secondary shrink-0 text-xs" :disabled="disabled" @click="emit('retry')">{{ t('governance.readiness.retry') }}</button>
    </div>
    <p v-else-if="!overview" role="status" class="mt-4 rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-900 dark:text-gray-400">{{ t('governance.readiness.notLoaded') }}</p>
    <div v-else class="mt-4 grid gap-3 md:grid-cols-2">
      <article v-for="item in items" :key="item.key" data-test="readiness-item" class="min-w-0 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <div class="flex items-start gap-3">
          <StatusIndicator :label="stateLabel(item)" :tone="stateTone[item.state]" />
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <h4 class="text-sm font-medium">{{ itemLabel(item) }}</h4>
            </div>
            <p v-if="itemDetail(item) && (item.state === 'pending' || item.state === 'read_failed')" class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ itemDetail(item) }}</p>
            <p v-if="countLabel(item)" class="mt-1 text-xs font-medium text-gray-600 dark:text-gray-300">{{ countLabel(item) }}</p>
          </div>
          <button v-if="item.target_tab" data-test="readiness-navigate" :data-target="item.target_tab" type="button" class="btn btn-secondary flex h-9 w-9 shrink-0 items-center justify-center !p-0" :title="t('governance.readiness.open')" :aria-label="`${t('governance.readiness.open')} · ${itemLabel(item)}`" :disabled="disabled" @click="emit('navigate', item.target_tab)"><Icon name="arrowRight" size="sm" aria-hidden="true" /></button>
        </div>
      </article>
    </div>
  </section>
</template>
