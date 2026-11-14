<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import type { ModelRun, ModelTestConfig } from '@/api/admin/upstream-model-monitoring'
import StatusIndicator from './StatusIndicator.vue'
import { formatGovernanceTime } from './format'

defineProps<{
  items: ModelRun[]
  loading: boolean
  disabled: boolean
  filtered: boolean
  batchId: string
  targetLabel: (config: ModelTestConfig) => string
}>()
const hideId = defineModel<string | null>('hideId', { default: null })
const deleteId = defineModel<string | null>('deleteId', { default: null })
const emit = defineEmits<{ detail: [id: string]; compare: [run: ModelRun]; batch: [id: string]; hide: [run: ModelRun]; remove: [run: ModelRun] }>()
const { t } = useI18n()
const mt = (key: string) => t(`governance.modelMonitoring.${key}`)
const runTone = (state: string) => state === 'succeeded' ? 'success' : state === 'failed' ? 'danger' : state === 'indeterminate' ? 'warning' : ['queued', 'running'].includes(state) ? 'info' : 'neutral'
const reviewTone = (review: ModelRun['review']) => review === 'pass' ? 'success' : review === 'fail' ? 'danger' : 'neutral'
const timing = (ms: number | null | undefined) => ms == null ? '—' : `${ms.toLocaleString()} ms`
const canHide = (run: ModelRun) => run.request.template === 'pelican' && run.request.config.target_type === 'local_group'
</script>

<template>
  <div class="min-w-0 max-w-full overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
    <div role="region" :aria-label="mt('runHistory')" tabindex="0" class="max-w-full overflow-x-auto focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500">
      <table data-test="model-run-table" class="w-full min-w-[960px] border-collapse text-left text-xs" :aria-busy="loading">
        <thead class="whitespace-nowrap border-b border-gray-200 bg-gray-50 text-gray-500 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-400">
          <tr>
            <th scope="col" class="px-4 py-3 font-medium">{{ mt('modelAndTarget') }}</th>
            <th scope="col" class="px-3 py-3 font-medium">{{ mt('templates') }} / {{ mt('efforts') }}</th>
            <th scope="col" class="px-3 py-3 font-medium">{{ mt('runState') }}</th>
            <th scope="col" class="px-3 py-3 font-medium">{{ mt('manualReview') }}</th>
            <th scope="col" class="px-3 py-3 font-medium">TTFT / {{ mt('duration') }}</th>
            <th scope="col" class="px-3 py-3 font-medium">{{ mt('createdAt') }}</th>
            <th scope="col" class="px-4 py-3 text-right font-medium">{{ t('common.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-if="loading"><td colspan="7" role="status" class="p-8 text-center text-gray-500">{{ t('common.loading') }}</td></tr>
          <tr v-else-if="!items.length"><td colspan="7" class="p-8 text-center text-gray-500">{{ mt(filtered ? 'noMatchingRuns' : 'noRuns') }}</td></tr>
          <template v-for="run in loading ? [] : items" :key="run.id">
            <tr class="transition-colors hover:bg-gray-50/80 dark:hover:bg-dark-700/40" :data-test="`model-run-row-${run.id}`">
              <td class="px-4 py-3 align-middle">
                <div class="flex items-center gap-2"><PlatformIcon :platform="run.request.config.platform" size="md" /><span class="max-w-48 truncate font-semibold text-gray-900 dark:text-gray-100" :title="run.request.config.model">{{ run.request.config.model }}</span></div>
                <p class="mt-1 max-w-60 truncate text-gray-500" :title="targetLabel(run.request.config)">{{ targetLabel(run.request.config) }}</p>
              </td>
              <td class="whitespace-nowrap px-3 py-3 align-middle"><p>{{ mt(`template_${run.request.template}`) }}</p><p class="mt-1 text-gray-500">{{ run.request.effort }} · {{ mt('sample') }} {{ run.request.sample }}</p></td>
              <td class="whitespace-nowrap px-3 py-3 align-middle"><span class="inline-flex items-center gap-1"><StatusIndicator :label="mt(`state_${run.status}`)" :tone="runTone(run.status)" aria-hidden="true" :tabindex="-1" /><span>{{ mt(`state_${run.status}`) }}</span></span></td>
              <td class="whitespace-nowrap px-3 py-3 align-middle"><span class="inline-flex items-center gap-1"><StatusIndicator :label="mt(`review_${run.review || 'pending'}`)" :tone="reviewTone(run.review)" aria-hidden="true" :tabindex="-1" /><span>{{ mt(`review_${run.review || 'pending'}`) }}</span></span></td>
              <td class="whitespace-nowrap px-3 py-3 align-middle tabular-nums"><p>{{ timing(run.result?.ttft_ms) }}</p><p class="mt-1 text-gray-500">{{ timing(run.result?.duration_ms) }}</p></td>
              <td class="whitespace-nowrap px-3 py-3 align-middle tabular-nums text-gray-500"><time :datetime="run.created_at">{{ formatGovernanceTime(run.created_at) }}</time></td>
              <td class="px-3 py-3 align-middle">
                <div class="flex items-center justify-end gap-1">
                  <button type="button" class="run-action" :data-test="`model-run-${run.id}`" :aria-label="mt('details')" :title="mt('details')" @click="emit('detail', run.id)"><Icon name="eye" size="sm" aria-hidden="true" /><span class="run-action-label">{{ mt('details') }}</span></button>
                  <button type="button" class="run-action" :data-test="`model-compare-${run.id}`" :aria-label="mt('compare')" :title="mt('compare')" @click="emit('compare', run)"><Icon name="chart" size="sm" aria-hidden="true" /><span class="run-action-label">{{ mt('compare') }}</span></button>
                  <button v-if="!batchId" type="button" class="run-action" :disabled="disabled" :data-test="`model-run-batch-${run.id}`" :aria-label="mt('viewBatch')" :title="mt('viewBatch')" @click="emit('batch', run.batch_id)"><Icon name="grid" size="sm" aria-hidden="true" /><span class="run-action-label">{{ mt('viewBatch') }}</span></button>
                  <button v-if="canHide(run)" type="button" class="run-action" :disabled="disabled" :data-test="`model-run-hide-${run.id}`" :aria-label="mt('hideRun')" :title="mt('hideRun')" @click="hideId = run.id; deleteId = null"><Icon name="eyeOff" size="sm" aria-hidden="true" /></button>
                  <button type="button" class="run-action hover:!text-red-600" :disabled="disabled" :data-test="`model-run-delete-${run.id}`" :aria-label="mt('deleteRun')" :title="mt('deleteRun')" @click="deleteId = run.id; hideId = null"><Icon name="trash" size="sm" aria-hidden="true" /></button>
                </div>
              </td>
            </tr>
            <tr v-if="hideId === run.id || deleteId === run.id">
              <td colspan="7" class="px-4 py-3" :class="hideId === run.id ? 'bg-amber-50 dark:bg-amber-900/20' : 'bg-red-50 dark:bg-red-900/20'">
                <div class="flex flex-wrap items-center gap-3"><span class="mr-auto">{{ mt(hideId === run.id ? 'hideRunHint' : 'deleteRunHint') }}</span>
                  <button v-if="hideId === run.id" type="button" class="btn btn-primary text-xs" :disabled="disabled" :data-test="`model-run-hide-confirm-${run.id}`" @click="emit('hide', run)">{{ mt('hideRun') }}</button>
                  <button v-else type="button" class="btn btn-danger text-xs" :disabled="disabled" :data-test="`model-run-delete-confirm-${run.id}`" @click="emit('remove', run)">{{ t('common.delete') }}</button>
                  <button type="button" class="btn btn-secondary text-xs" :disabled="disabled" @click="hideId = null; deleteId = null">{{ t('common.cancel') }}</button>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
.run-action {
  @apply inline-flex h-9 min-w-9 shrink-0 items-center justify-center gap-1 rounded-lg px-2 text-xs text-gray-500 transition hover:bg-gray-100 hover:text-gray-900 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-600 dark:hover:text-gray-100;
}
.run-action-label { display: none; }
@container model-monitor (min-width: 1400px) {
  .run-action-label { display: inline; }
}
</style>
