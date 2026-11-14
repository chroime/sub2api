<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { ModelEffort, ModelReview, ModelRunFilters, ModelTestTemplate } from '@/api/admin/upstream-model-monitoring'

defineProps<{ disabled?: boolean; loading?: boolean }>()
const emit = defineEmits<{ apply: [filters: ModelRunFilters] }>()
const { t } = useI18n()
const mt = (key: string) => t(`governance.modelMonitoring.${key}`)
const emptyDraft = () => ({ q: '', status: '', template: '' as ModelTestTemplate | '', effort: '' as ModelEffort | 'default' | '', review: '' as ModelReview | '' })
const draft = ref(emptyDraft())
const from = ref(''), to = ref(''), error = ref('')
const options = (values: string[], prefix: string) => [{ value: '', label: mt('filterAll') }, ...values.map(value => ({ value, label: prefix ? mt(`${prefix}_${value}`) : value }))]
const statusOptions = computed(() => options(['queued', 'running', 'succeeded', 'failed', 'cancelled', 'indeterminate', 'skipped'], 'state'))
const templateOptions = computed(() => options(['candy', 'pelican', 'token_audit', 'context', 'probe'], 'template'))
const effortOptions = computed(() => options(['low', 'medium', 'high', 'default'], ''))
const reviewOptions = computed(() => options(['pending', 'pass', 'fail'], 'review'))

function search() {
  const start = from.value ? new Date(from.value) : null
  const end = to.value ? new Date(to.value) : null
  if ((start && !Number.isFinite(start.getTime())) || (end && !Number.isFinite(end.getTime())) || (start && end && start >= end)) {
    error.value = mt('invalidDateRange')
    return
  }
  error.value = ''
  const filters: ModelRunFilters = {}
  for (const [key, value] of Object.entries(draft.value)) {
    if (typeof value === 'string' && value.trim()) Object.assign(filters, { [key]: value.trim() })
  }
  if (start) filters.created_from = start.toISOString()
  if (end) filters.created_to = end.toISOString()
  emit('apply', filters)
}
function reset() {
  draft.value = emptyDraft(); from.value = ''; to.value = ''; error.value = ''
  emit('apply', {})
}
</script>

<template>
  <form data-test="model-run-filters" class="min-w-0 space-y-3 rounded-xl border border-gray-200 bg-gray-50/70 p-3 dark:border-dark-600 dark:bg-dark-900/40" :aria-label="mt('runFilters')" @submit.prevent="search">
    <fieldset :disabled="disabled" class="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
      <label class="min-w-0 text-xs text-gray-500 dark:text-gray-400 sm:col-span-2">
        {{ mt('keyword') }}
        <input v-model="draft.q" data-test="model-run-query" type="search" maxlength="200" class="input mt-1 w-full" :placeholder="mt('keywordPlaceholder')" />
      </label>
      <label class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
        {{ mt('runState') }}
        <Select v-model="draft.status" data-test="model-run-status" class="mt-1" :options="statusOptions" :disabled="disabled" :aria-label="mt('runState')" />
      </label>
      <label class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
        {{ mt('templates') }}
        <Select v-model="draft.template" data-test="model-run-template" class="mt-1" :options="templateOptions" :disabled="disabled" :aria-label="mt('templates')" />
      </label>
      <label class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
        {{ mt('efforts') }}
        <Select v-model="draft.effort" data-test="model-run-effort" class="mt-1" :options="effortOptions" :disabled="disabled" :aria-label="mt('efforts')" />
      </label>
      <label class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
        {{ mt('manualReview') }}
        <Select v-model="draft.review" data-test="model-run-review" class="mt-1" :options="reviewOptions" :disabled="disabled" :aria-label="mt('manualReview')" />
      </label>
      <label class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
        {{ mt('createdFrom') }}
        <input v-model="from" data-test="model-run-from" type="datetime-local" class="input mt-1 min-w-0 max-w-full w-full" />
      </label>
      <label class="min-w-0 text-xs text-gray-500 dark:text-gray-400">
        {{ mt('createdTo') }}
        <input v-model="to" data-test="model-run-to" type="datetime-local" class="input mt-1 min-w-0 max-w-full w-full" />
      </label>
    </fieldset>
    <div class="flex flex-wrap items-center justify-end gap-2">
      <p v-if="error" role="alert" class="mr-auto text-xs text-red-600 dark:text-red-400">{{ error }}</p>
      <button type="button" data-test="model-run-reset" class="btn btn-secondary text-xs" :disabled="disabled || loading" @click="reset"><Icon name="refresh" size="sm" aria-hidden="true" />{{ t('common.reset') }}</button>
      <button type="submit" data-test="model-run-search" class="btn btn-primary text-xs" :disabled="disabled || loading"><Icon name="search" size="sm" aria-hidden="true" />{{ t('common.search') }}</button>
    </div>
  </form>
</template>
