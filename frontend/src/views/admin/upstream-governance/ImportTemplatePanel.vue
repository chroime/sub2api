<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import api, {
  type ImportTemplate,
  type ImportTemplateCollection,
  type ImportTemplateSettings,
} from '@/api/admin/upstream-import-templates'
import { validImportTemplateSettings } from './import-templates'

const props = defineProps<{
  settings: ImportTemplateSettings
  scopeKey: string
  pristine: boolean
  disabled?: boolean
}>()
const emit = defineEmits<{
  apply: [value: { settings: ImportTemplateSettings; automatic: boolean }]
  changed: []
  ready: [value: boolean]
  busy: [value: boolean]
}>()
const { t } = useI18n()
const collection = ref<ImportTemplateCollection>({ version: 0, templates: [] })
const loaded = ref(false)
const loading = ref(false)
const saving = ref(false)
const conflict = ref(false)
const error = ref('')
const continued = ref(false)
const saved = ref(false)
const selectedID = ref('')
const name = ref('')
const isDefault = ref(false)
const pendingApply = ref(false)
const pendingDelete = ref(false)
let generation = 0

const selected = computed(() => collection.value.templates.find(item => item.id === selectedID.value))
const locked = computed(() => !!props.disabled || loading.value || saving.value)
const readBlocked = computed(() => locked.value || !loaded.value)
const writeBlocked = computed(() => readBlocked.value || conflict.value)
const validSettings = computed(() => validImportTemplateSettings(props.settings))
const validName = computed(() => isValidName(name.value))
const saveBlocked = computed(() => writeBlocked.value || !validSettings.value || !validName.value)
const options = computed(() => [
  { value: '', label: t('governance.importTemplates.select') },
  ...collection.value.templates.map(item => ({
    value: item.id,
    label: `${item.name}${item.is_default ? ' · ' + t('governance.importTemplates.default') : ''}`,
  })),
])
const parameterFields = [
  { key: 'concurrency', label: 'concurrency' },
  { key: 'priority', label: 'accountPriority' },
  { key: 'quota_enabled', label: 'quotaControl' },
  { key: 'quota_daily_limit', label: 'dailyQuota' },
  { key: 'quota_weekly_limit', label: 'weeklyQuota' },
  { key: 'quota_limit', label: 'totalQuota' },
  { key: 'upstream_billing_rate_sync_enabled', label: 'syncUpstreamBilling' },
  { key: 'openai_long_context_billing_enabled', label: 'longContextBilling' },
] as const
const selectedParameters = computed(() => selected.value ? parameterFields.map(field => {
  const value = selected.value!.settings[field.key]
  return {
    ...field,
    value: typeof value === 'boolean'
      ? t(`governance.importTemplates.${value ? 'enabled' : 'disabled'}`)
      : String(value),
  }
}) : [])

function isValidName(value: string): boolean {
  const trimmed = value.trim()
  return trimmed.length > 0 && Array.from(trimmed).length <= 100 && !/\p{Cc}/u.test(trimmed)
}

function validCollection(value: ImportTemplateCollection): boolean {
  if (!value || !Number.isSafeInteger(value.version) || value.version < 0
    || !Array.isArray(value.templates) || value.templates.length > 50) return false
  const ids = new Set<string>()
  let defaults = 0
  return value.templates.every(item => {
    if (!item || typeof item.id !== 'string' || !/^[a-zA-Z0-9_-]{1,64}$/.test(item.id)
      || ids.has(item.id) || typeof item.name !== 'string' || !isValidName(item.name)
      || typeof item.is_default !== 'boolean' || !validImportTemplateSettings(item.settings)) return false
    ids.add(item.id)
    if (item.is_default) defaults++
    return defaults <= 1
  })
}

async function load(initial: boolean) {
  const request = ++generation
  loading.value = true
  loaded.value = false
  continued.value = false
  error.value = ''
  saved.value = false
  pendingApply.value = false
  pendingDelete.value = false
  emit('ready', false)
  try {
    const value = await api.list()
    if (request !== generation) return
    if (!validCollection(value)) throw new Error('Invalid import template collection')
    collection.value = value
    loaded.value = true
    conflict.value = false
    // Only the initial response may fill a still-pristine draft. Retries and
    // explicit reloads must never become delayed automatic draft overwrites.
    if (initial && props.pristine) {
      const template = value.templates.find(item => item.is_default)
      if (template) {
        selectedID.value = template.id
        name.value = template.name
        isDefault.value = template.is_default
        emit('apply', { settings: { ...template.settings }, automatic: true })
      }
    }
    emit('ready', true)
  } catch {
    if (request === generation) error.value = 'loadFailed'
  } finally {
    if (request === generation) loading.value = false
  }
}

function reload() {
  if (locked.value) return
  void load(false)
}

function continueCurrent() {
  if (locked.value || loaded.value || error.value !== 'loadFailed') return
  continued.value = true
  emit('ready', true)
}

function choose(value: string | number | boolean | null) {
  if (readBlocked.value) return
  selectedID.value = typeof value === 'string' ? value : ''
  name.value = selected.value?.name || ''
  isDefault.value = selected.value?.is_default || false
  pendingApply.value = false
  pendingDelete.value = false
  saved.value = false
}

function applySelected(confirmed = false) {
  if (readBlocked.value || !selected.value) return
  const different = Object.entries(selected.value.settings).some(
    ([key, value]) => props.settings[key as keyof ImportTemplateSettings] !== value,
  )
  if (different && !confirmed) {
    pendingApply.value = true
    pendingDelete.value = false
    return
  }
  if (confirmed && !pendingApply.value) return
  pendingApply.value = false
  emit('apply', { settings: { ...selected.value.settings }, automatic: false })
}

async function persist(templates: ImportTemplate[]): Promise<boolean> {
  if (writeBlocked.value) return false
  const request = generation
  saving.value = true
  saved.value = false
  error.value = ''
  pendingApply.value = false
  pendingDelete.value = false
  emit('busy', true)
  try {
    const value = await api.save({ version: collection.value.version, templates })
    if (request !== generation) return false
    if (!validCollection(value)) throw new Error('Invalid import template collection')
    collection.value = value
    saved.value = true
    emit('changed')
    return true
  } catch (cause) {
    if (request === generation) {
      conflict.value = (cause as { status?: number })?.status === 409
      error.value = conflict.value ? 'conflict' : 'saveFailed'
    }
    return false
  } finally {
    if (request === generation) {
      saving.value = false
      emit('busy', false)
    }
  }
}

async function saveTemplate(update: boolean) {
  if (saveBlocked.value || (update ? !selected.value : collection.value.templates.length >= 50)) return
  const request = generation
  const id = update ? selected.value!.id
    : `tpl_${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}_${Math.random().toString(36).slice(2)}`}`
  const template: ImportTemplate = {
    id,
    name: name.value.trim(),
    is_default: isDefault.value,
    settings: { ...props.settings },
  }
  const templates = collection.value.templates
    .filter(item => item.id !== id)
    .map(item => template.is_default ? { ...item, is_default: false } : item)
  if (await persist([...templates, template]) && request === generation) {
    selectedID.value = id
    name.value = template.name
  }
}

function requestDelete() {
  if (writeBlocked.value || !selected.value) return
  pendingDelete.value = true
  pendingApply.value = false
}

async function deleteSelected() {
  if (writeBlocked.value || !selected.value || !pendingDelete.value) return
  const request = generation
  if (await persist(collection.value.templates.filter(item => item.id !== selectedID.value)) && request === generation) {
    selectedID.value = ''
    name.value = ''
    isDefault.value = false
  }
}

function resetScope() {
  if (saving.value) emit('busy', false)
  saving.value = false
  conflict.value = false
  collection.value = { version: 0, templates: [] }
  selectedID.value = ''
  name.value = ''
  isDefault.value = false
  void load(true)
}

watch(() => props.scopeKey, resetScope)
watch(() => props.settings, () => { pendingApply.value = false }, { deep: true })
onMounted(resetScope)
onUnmounted(() => {
  generation++
  if (saving.value) emit('busy', false)
})
</script>

<template>
  <section data-test="import-template-panel" class="min-w-0 space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0 flex-1">
        <h4 class="font-semibold">{{ t('governance.importTemplates.title') }}</h4>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('governance.importTemplates.hint') }}</p>
      </div>
      <button data-test="reload-import-templates" type="button" class="btn btn-secondary text-xs" :disabled="locked" @click="reload">
        {{ t('governance.importTemplates.reload') }}
      </button>
    </div>
    <p v-if="loading || saving" role="status" class="text-sm text-gray-500 dark:text-gray-400">
      {{ t(`governance.importTemplates.${loading ? 'loading' : 'saving'}`) }}
    </p>
    <div v-if="error" role="alert" class="space-y-2 text-sm text-red-600 dark:text-red-400">
      <p>{{ t(`governance.importTemplates.${error}`) }}</p>
      <button v-if="error === 'loadFailed' && !continued" data-test="continue-import-parameters" type="button" class="btn btn-secondary text-xs" :disabled="locked" @click="continueCurrent">
        {{ t('governance.importTemplates.continueCurrent') }}
      </button>
    </div>
    <p v-if="continued" role="status" class="text-xs text-gray-500 dark:text-gray-400">{{ t('governance.importTemplates.continued') }}</p>
    <p v-if="saved" role="status" class="text-xs text-green-700 dark:text-green-400">{{ t('governance.importTemplates.saved') }}</p>
    <div class="flex flex-wrap items-center gap-3">
      <Select data-test="import-template-select" class="min-w-0 flex-1 basis-48" :model-value="selectedID" :options="options" :disabled="readBlocked" :aria-label="t('governance.importTemplates.select')" @update:model-value="choose" />
      <button data-test="apply-import-template" type="button" class="btn btn-secondary text-xs" :disabled="readBlocked || !selected" @click="applySelected()">
        {{ t('governance.importTemplates.fillDraft') }}
      </button>
    </div>
    <details v-if="selected" data-test="import-template-summary" class="rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-900">
      <summary class="cursor-pointer break-words text-xs font-medium">{{ t('governance.importTemplates.parameterSummary', { name: selected.name }) }}</summary>
      <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('governance.importTemplates.summaryHint') }}</p>
      <dl class="mt-2 grid grid-cols-1 gap-x-6 gap-y-2 text-xs sm:grid-cols-2">
        <div v-for="parameter in selectedParameters" :key="parameter.key" class="flex min-w-0 items-start justify-between gap-3">
          <dt class="text-gray-500 dark:text-gray-400">{{ t(`governance.${parameter.label}`) }}</dt>
          <dd :data-test="'import-template-value-' + parameter.key" class="shrink-0 font-medium tabular-nums">{{ parameter.value }}</dd>
        </div>
      </dl>
    </details>
    <div v-if="pendingApply" class="space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-300" role="alert">
      <p>{{ t('governance.importTemplates.overwriteConfirm') }}</p>
      <div class="flex flex-wrap gap-2">
        <button data-test="confirm-import-template-apply" type="button" class="btn btn-secondary text-xs" :disabled="readBlocked" @click="applySelected(true)">{{ t('governance.importTemplates.confirmFill') }}</button>
        <button data-test="cancel-import-template-apply" type="button" class="btn btn-secondary text-xs" :disabled="locked" @click="pendingApply = false">{{ t('governance.importTemplates.cancel') }}</button>
      </div>
    </div>
    <details class="rounded-lg bg-gray-50 p-3 dark:bg-dark-900">
      <summary class="cursor-pointer text-sm font-medium">{{ t('governance.importTemplates.manage') }}</summary>
      <div class="mt-3 space-y-3">
        <div class="flex flex-wrap items-center gap-3">
          <input v-model="name" data-test="import-template-name" class="input min-w-0 flex-1 basis-48 text-sm" :disabled="locked || !loaded" :aria-label="t('governance.importTemplates.name')" :placeholder="t('governance.importTemplates.name')" />
          <label class="governance-checkbox-label flex items-center gap-2 text-xs">
            <input v-model="isDefault" data-test="import-template-default" type="checkbox" class="governance-checkbox" :disabled="locked || !loaded" />
            {{ t('governance.importTemplates.useAsDefault') }}
          </label>
        </div>
        <p v-if="name && !validName" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t('governance.importTemplates.invalidName') }}</p>
        <p v-if="!validSettings" role="alert" class="text-xs text-amber-700 dark:text-amber-400">{{ t('governance.importTemplates.invalidSettings') }}</p>
        <p v-if="collection.templates.length >= 50" class="text-xs text-gray-500 dark:text-gray-400">{{ t('governance.importTemplates.limitReached') }}</p>
        <p v-if="selectedID && !selected && loaded" class="text-xs text-amber-700 dark:text-amber-400">{{ t('governance.importTemplates.selectionMissing') }}</p>
        <div class="flex flex-wrap gap-2">
          <button data-test="save-new-import-template" type="button" class="btn btn-secondary text-xs" :disabled="saveBlocked || collection.templates.length >= 50" @click="saveTemplate(false)">{{ t('governance.importTemplates.saveNew') }}</button>
          <button data-test="update-import-template" type="button" class="btn btn-secondary text-xs" :disabled="saveBlocked || !selected" @click="saveTemplate(true)">{{ t('governance.importTemplates.updateSelected') }}</button>
          <button data-test="delete-import-template" type="button" class="btn btn-secondary text-xs text-red-600 dark:text-red-400" :disabled="writeBlocked || !selected" @click="requestDelete">{{ t('governance.importTemplates.deleteSelected') }}</button>
        </div>
        <div v-if="pendingDelete" role="alert" class="space-y-2 text-sm text-red-600 dark:text-red-400">
          <p>{{ t('governance.importTemplates.deleteConfirm', { name: selected?.name }) }}</p>
          <div class="flex flex-wrap gap-2">
            <button data-test="confirm-import-template-delete" type="button" class="btn btn-secondary text-xs" :disabled="writeBlocked" @click="deleteSelected">{{ t('governance.importTemplates.confirmDelete') }}</button>
            <button data-test="cancel-import-template-delete" type="button" class="btn btn-secondary text-xs" :disabled="locked" @click="pendingDelete = false">{{ t('governance.importTemplates.cancel') }}</button>
          </div>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('governance.importTemplates.persistedHint') }}</p>
      </div>
    </details>
  </section>
</template>
