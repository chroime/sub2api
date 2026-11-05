<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select from '@/components/common/Select.vue'
import { getModelsByPlatform } from '@/composables/useModelWhitelist'
import api, {
  type ModelTemplate,
  type ModelTemplateCollection,
  type RemoteGroup,
  type Transport,
} from '@/api/admin/upstream-governance'
import { isOpenAITextModel, type ModelSelections } from './import-config'
import { errorKey } from './feedback'
import { providerLabel, transportPlatforms } from './providers'
const props = defineProps<{
  modelValue: ModelSelections
  groups: RemoteGroup[]
  preservePlatforms?: Transport[]
  disabled?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: ModelSelections]
  ready: [value: boolean]
  edited: [platform: Transport]
}>()
const { t } = useI18n()
const templateLimit = 50
const platforms = transportPlatforms
const platform = ref<Transport>('openai')
const collection = ref<ModelTemplateCollection>({ version: 0, templates: [] })
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const search = ref('')
const custom = ref('')
const templateName = ref('')
const saveAsDefault = ref(false)
const templateID = ref('')
const pendingDelete = ref(false)
// The template each protocol's selection last came from, applied automatically
// as the default or explicitly by the administrator. Choosing another template
// in the dropdown only previews it until it is applied.
const applied = reactive<Partial<Record<Transport, string>>>({})
let generation = 0
let templatesLoaded = false
const edited: Partial<Record<Transport, boolean>> = Object.fromEntries((props.preservePlatforms || []).map(value => [value, true]))
const current = computed(() => props.modelValue[platform.value])
const availableTemplates = computed(() =>
  collection.value.templates.filter(
    (template) => template.platform === platform.value,
  ),
)
const selectedTemplate = computed(() =>
  availableTemplates.value.find((template) => template.id === templateID.value),
)
const templateOptions = computed(() => [
  { value: '', label: t('governance.selectTemplate') },
  ...availableTemplates.value.map(template => ({
    value: template.id,
    label: `${template.name}${template.is_default ? ' · ' + t('governance.defaultTemplate') : ''}`,
  })),
])
function sameModels(a: string[], b: string[]) {
  const set = new Set(a)
  return set.size === new Set(b).size && b.every(model => set.has(model))
}
const matchesSelected = computed(() =>
  !!selectedTemplate.value && current.value.enabled && sameModels(current.value.models, selectedTemplate.value.models),
)
const pendingApply = computed(() =>
  !!selectedTemplate.value && applied[platform.value] !== selectedTemplate.value.id && !matchesSelected.value,
)
const modified = computed(() =>
  !!selectedTemplate.value && applied[platform.value] === selectedTemplate.value.id && !matchesSelected.value,
)
const templateDiff = computed(() => {
  const template = selectedTemplate.value
  if (!template) return null
  const selected = new Set(current.value.models)
  const target = new Set(template.models)
  return {
    name: template.name,
    total: target.size,
    added: template.models.filter(model => !selected.has(model)).length,
    removed: current.value.models.filter(model => !target.has(model)).length,
  }
})
function upstreamModels(p: Transport) {
  return [
    ...new Set(
      props.groups
        .filter(
          (group) =>
            group.platform === p,
        )
        .flatMap((group) => group.models || []),
    ),
  ]
}
const upstreamSet = computed(() => new Set(upstreamModels(platform.value)))
// Upstream-visible models first, then the built-in list, then hand-added ones.
// The order does not depend on the selection, so rows stay put when toggled.
const availableModels = computed(() => [
  ...new Set([
    ...upstreamModels(platform.value),
    ...getModelsByPlatform(platform.value),
    ...current.value.models,
  ]),
].filter(model => platform.value !== 'openai' || isOpenAITextModel(model)))
const filteredModels = computed(() =>
  availableModels.value.filter((model) =>
    model.toLowerCase().includes(search.value.trim().toLowerCase()),
  ),
)
const unselectedMatches = computed(() =>
  search.value.trim() ? filteredModels.value.filter(model => !current.value.models.includes(model)) : [],
)
function platformBadge(p: Transport) {
  const selection = props.modelValue[p]
  return selection.enabled ? String(selection.models.length) : t('governance.modelPicker.unrestricted')
}
function update(models: string[], enabled = current.value.enabled) {
  edited[platform.value] = true
  emit('edited', platform.value)
  emit('update:modelValue', {
    ...props.modelValue,
    [platform.value]: { enabled, models: [...new Set(models)] },
  })
}
function toggle(model: string) {
  update(
    current.value.models.includes(model)
      ? current.value.models.filter((value) => value !== model)
      : [...current.value.models, model],
  )
}
function showTemplate(id: string) {
  templateID.value = id
  pendingDelete.value = false
  templateName.value = selectedTemplate.value?.name || ''
  saveAsDefault.value = selectedTemplate.value?.is_default || false
}
function chooseTemplate(value: string | number | boolean | null) {
  showTemplate(typeof value === 'string' ? value : '')
  // A template that already matches the selection needs no confirmation.
  if (selectedTemplate.value && matchesSelected.value) applied[platform.value] = selectedTemplate.value.id
}
function applyTemplate() {
  const template = selectedTemplate.value
  if (!template) return
  update(template.models, true)
  applied[platform.value] = template.id
}
function showAppliedTemplate() {
  const id = applied[platform.value]
  showTemplate(id && availableTemplates.value.some(template => template.id === id) ? id : '')
}
function addCustom() {
  // Model IDs never contain whitespace, so pasted lists split cleanly.
  const entries = custom.value.split(/[\s,，;；]+/).filter(Boolean)
  const invalid = entries.filter(model => model.length > 200 || /[\s*\p{Cc}]/u.test(model))
  const valid = entries.filter(model => !invalid.includes(model))
  if (valid.length) update([...current.value.models, ...valid], true)
  custom.value = invalid.join(' ')
  error.value = !entries.length || invalid.length ? t('governance.invalidModelName') : ''
}
function pasteModels(event: ClipboardEvent) {
  const text = event.clipboardData?.getData('text') || ''
  // Text inputs drop line breaks from pasted text, which would glue IDs together.
  if (!/[\r\n]/.test(text)) return
  event.preventDefault()
  custom.value = [custom.value.trim(), text.replace(/\s+/g, ' ').trim()].filter(Boolean).join(' ')
}
async function load() {
  const request = ++generation
  loading.value = true
  emit('ready', false)
  error.value = ''
  try {
    const value = await api.modelTemplates()
    if (request !== generation) return
    collection.value = value
    templatesLoaded = true
    applyDefaults()
    showAppliedTemplate()
    emit('ready', true)
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) loading.value = false
  }
}
function applyDefaults() {
  if (!templatesLoaded) return
  const selections = { ...props.modelValue }
  for (const p of platforms) {
    if (edited[p]) continue
    const template = collection.value.templates.find(
      (item) => item.platform === p && item.is_default,
    )
    const models = template?.models || upstreamModels(p)
    selections[p] = { enabled: models.length > 0, models: [...models] }
    if (template) applied[p] = template.id
    else delete applied[p]
  }
  emit('update:modelValue', selections)
}
async function persist(templates: ModelTemplate[]): Promise<boolean> {
  const request = generation
  saving.value = true
  error.value = ''
  try {
    const saved = await api.saveModelTemplates({
      version: collection.value.version,
      templates,
    })
    if (request !== generation) return false
    collection.value = saved
    return true
  } catch (e) {
    if (request !== generation) return false
    if ((e as { status?: number }).status === 409) {
      error.value = t('governance.templateConflict')
      try {
        const latest = await api.modelTemplates()
        if (request === generation) collection.value = latest
      } catch {
        if (request === generation)
          error.value = t('governance.templateRefreshFailed')
      }
    } else error.value = t(errorKey(e))
    return false
  } finally {
    if (request === generation) saving.value = false
  }
}
async function saveTemplate(asNew: boolean) {
  const existing = asNew ? undefined : selectedTemplate.value
  if (!asNew && !existing) return
  if (
    !templateName.value.trim() ||
    !current.value.models.length ||
    current.value.models.length > 500
  ) {
    error.value = t('governance.templateInvalid')
    return
  }
  if (asNew && collection.value.templates.length >= templateLimit) {
    error.value = t('governance.modelPicker.limitReached')
    return
  }
  const request = generation
  const id =
    existing?.id ||
    `tpl_${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}_${Math.random().toString(36).slice(2)}`}`
  const template: ModelTemplate = {
    id,
    name: templateName.value.trim(),
    platform: platform.value,
    models: [...current.value.models],
    is_default: saveAsDefault.value,
  }
  // An update keeps the template's place in the list; a new one is appended.
  const others = collection.value.templates.map((item) =>
    item.id === id
      ? template
      : template.is_default && item.platform === template.platform
        ? { ...item, is_default: false }
        : item,
  )
  if (await persist(existing ? others : [...others, template]) && request === generation) {
    showTemplate(id)
    applied[platform.value] = id
  }
}
async function removeTemplate() {
  const template = selectedTemplate.value
  if (!template || !pendingDelete.value) return
  const request = generation
  if (await persist(collection.value.templates.filter((item) => item.id !== template.id)) && request === generation) {
    if (applied[template.platform as Transport] === template.id) delete applied[template.platform as Transport]
    showTemplate('')
  }
}
watch(platform, () => {
  search.value = ''
  custom.value = ''
  showAppliedTemplate()
})
watch(() => props.groups, applyDefaults)
onMounted(load)
onUnmounted(() => {
  generation++
})
</script>
<template>
  <section class="space-y-4">
    <div>
      <h4 class="font-semibold">{{ t('governance.modelRestrictions') }}</h4>
      <p class="mt-1 text-xs text-gray-500">
        {{ t('governance.modelTemplateHint') }}
      </p>
    </div>
    <div class="flex flex-wrap gap-2" :aria-label="t('governance.transport')">
      <button
        v-for="p in platforms"
        :key="p"
        type="button"
        :data-test="'model-platform-' + p"
        class="inline-flex items-center gap-2 rounded-lg border px-3 py-2 text-sm transition-colors"
        :class="
          platform === p
            ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300'
            : 'border-gray-200 dark:border-dark-600'
        "
        :aria-pressed="platform === p"
        :disabled="disabled || saving"
        @click="platform = p"
      >
        <PlatformIcon :platform="p" />{{
          providerLabel(p)
        }}<span
          :data-test="'model-platform-count-' + p"
          class="rounded bg-gray-100 px-1.5 text-xs tabular-nums text-gray-500 dark:bg-dark-700 dark:text-gray-300"
          >{{ platformBadge(p) }}</span
        >
      </button>
    </div>
    <p v-if="loading" role="status" class="text-sm text-gray-500">
      {{ t('common.loading') }}
    </p>
    <div
      v-if="error"
      role="alert"
      class="flex items-center gap-3 text-sm text-red-600"
    >
      <span>{{ error }}</span
      ><button
        type="button"
        class="btn btn-secondary"
        :disabled="loading || saving"
        @click="load"
      >
        {{ t('common.refresh') }}
      </button>
    </div>
    <fieldset :disabled="disabled || loading || saving" class="min-w-0 space-y-3">
      <div class="flex flex-wrap items-center gap-3">
        <label class="governance-checkbox-label flex items-center gap-2 text-sm font-medium"
          ><input
            data-test="model-restriction-enabled"
            type="checkbox"
            class="governance-checkbox"
            :checked="current.enabled"
            @change="
              update(
                current.models,
                ($event.target as HTMLInputElement).checked,
              )
            "
          />{{ t('governance.enableModelRestrictions') }}</label
        >
        <span class="text-xs text-gray-500">{{
          t('governance.modelSelectedCount', { count: current.models.length })
        }}</span>
        <div class="flex w-full min-w-0 items-center gap-2 sm:ml-auto sm:w-auto">
          <span
            v-if="modified"
            data-test="template-modified"
            class="shrink-0 rounded bg-amber-50 px-1.5 py-0.5 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
            >{{ t('governance.modelPicker.modified') }}</span
          >
          <Select
            data-test="model-template"
            class="w-full min-w-0 sm:w-auto sm:min-w-48"
            :model-value="templateID"
            :options="templateOptions"
            :disabled="disabled || loading || saving"
            :aria-label="t('governance.modelTemplate')"
            @update:model-value="chooseTemplate"
          />
        </div>
      </div>
      <div
        v-if="pendingApply && templateDiff"
        data-test="template-apply-prompt"
        role="status"
        class="flex flex-wrap items-center gap-3 rounded-lg bg-amber-50 p-3 text-xs text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
      >
        <p class="min-w-0 flex-1">
          {{ t('governance.modelPicker.applyPrompt', templateDiff) }}
          <template v-if="!current.enabled">{{ t('governance.modelPicker.applyEnables') }}</template>
        </p>
        <button data-test="apply-model-template" type="button" class="btn btn-primary text-xs" @click="applyTemplate">
          {{ t('governance.modelPicker.apply') }}
        </button>
        <button data-test="cancel-model-template" type="button" class="btn btn-secondary text-xs" @click="showAppliedTemplate">
          {{ t('governance.modelPicker.cancel') }}
        </button>
      </div>
      <p
        v-if="!current.enabled"
        class="rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-900"
      >
        {{ t('governance.modelsUnrestricted') }}
      </p>
      <template v-else>
        <div
          v-if="current.models.length"
          data-test="selected-models"
          class="flex max-h-28 flex-wrap gap-1.5 overflow-y-auto"
          :aria-label="t('governance.modelPicker.selectedModels')"
        >
          <span
            v-for="model in current.models"
            :key="model"
            class="inline-flex max-w-full items-center gap-1 rounded-md bg-primary-50 py-0.5 pl-2 pr-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
            ><span class="truncate" :title="model">{{ model }}</span
            ><button
              type="button"
              class="rounded px-1 leading-none hover:bg-primary-100 dark:hover:bg-primary-900/40"
              :aria-label="t('governance.modelPicker.removeModel', { model })"
              @click="toggle(model)"
            >×</button></span
          >
        </div>
        <div class="flex flex-wrap gap-2">
          <input
            v-model="search"
            data-test="model-search"
            class="input min-w-40 flex-1 text-sm"
            :placeholder="t('governance.searchModels')"
            :aria-label="t('governance.searchModels')"
          /><button
            v-if="unselectedMatches.length"
            data-test="select-filtered"
            type="button"
            class="btn btn-secondary text-xs"
            @click="update([...current.models, ...unselectedMatches])"
          >
            {{ t('governance.modelPicker.selectFiltered', { count: unselectedMatches.length }) }}</button
          ><button
            type="button"
            class="btn btn-secondary text-xs"
            @click="update(upstreamModels(platform))"
          >
            {{ t('governance.useVisibleModels') }}</button
          ><button
            data-test="clear-models"
            type="button"
            class="btn btn-secondary text-xs"
            @click="update([])"
          >
            {{ t('governance.clearModels') }}
          </button>
        </div>
        <div
          class="grid max-h-72 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-gray-200 p-2 sm:grid-cols-2 lg:grid-cols-3 dark:border-dark-600"
        >
          <label
            v-for="model in filteredModels"
            :key="model"
            class="governance-checkbox-label flex min-w-0 items-center gap-2 rounded px-2 py-1.5 text-xs hover:bg-gray-50 dark:hover:bg-dark-700"
            ><input
              type="checkbox"
              class="governance-checkbox"
              :checked="current.models.includes(model)"
              :value="model"
              @change="toggle(model)"
            /><span class="truncate" :title="model">{{ model }}</span
            ><span
              v-if="upstreamSet.has(model)"
              data-test="upstream-model"
              class="ml-auto shrink-0 rounded bg-gray-100 px-1 text-[10px] text-gray-500 dark:bg-dark-700 dark:text-gray-400"
              >{{ t('governance.modelPicker.upstreamBadge') }}</span
            ></label
          >
        </div>
        <p
          v-if="!current.models.length"
          role="alert"
          class="text-xs text-amber-700 dark:text-amber-400"
        >
          {{ t('governance.emptyWhitelist') }}
        </p>
        <div class="flex gap-2">
          <input
            v-model="custom"
            data-test="custom-model"
            class="input min-w-0 flex-1 text-sm"
            :placeholder="t('governance.modelPicker.customModelBulk')"
            :aria-label="t('governance.customModel')"
            @keydown.enter.prevent="addCustom"
            @paste="pasteModels"
          /><button
            data-test="add-model"
            type="button"
            class="btn btn-secondary"
            @click="addCustom"
          >
            {{ t('common.add') }}
          </button>
        </div>
      </template>
      <details class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900">
        <summary class="cursor-pointer font-medium">
          {{ t('governance.manageTemplates') }}
        </summary>
        <div class="mt-3 flex flex-wrap items-center gap-3">
          <input
            v-model="templateName"
            data-test="template-name"
            maxlength="100"
            class="input min-w-40 flex-1 text-sm"
            :aria-label="t('governance.templateName')"
            :placeholder="t('governance.templateName')"
          /><label class="governance-checkbox-label flex items-center gap-2 text-xs"
            ><input
              v-model="saveAsDefault"
              data-test="template-default"
              type="checkbox"
              class="governance-checkbox"
            />{{ t('governance.useAsDefault') }}</label
          >
        </div>
        <div class="mt-3 flex flex-wrap gap-2">
          <button
            v-if="selectedTemplate"
            data-test="update-template"
            type="button"
            class="btn btn-secondary text-xs"
            :disabled="!current.models.length || !templateName.trim()"
            @click="saveTemplate(false)"
          >
            {{ t('governance.modelPicker.updateTemplate') }}</button
          ><button
            data-test="save-template"
            type="button"
            class="btn btn-secondary text-xs"
            :disabled="!current.models.length || !templateName.trim() || collection.templates.length >= templateLimit"
            @click="saveTemplate(true)"
          >
            {{ t('governance.modelPicker.saveAsNew') }}</button
          ><button
            v-if="selectedTemplate"
            data-test="delete-template"
            type="button"
            class="btn btn-secondary text-xs text-red-600 dark:text-red-400"
            @click="pendingDelete = true"
          >
            {{ t('governance.modelPicker.deleteTemplate') }}
          </button>
        </div>
        <div
          v-if="pendingDelete && selectedTemplate"
          role="alert"
          class="mt-3 flex flex-wrap items-center gap-3 rounded-lg bg-red-50 p-3 text-xs text-red-700 dark:bg-red-900/20 dark:text-red-300"
        >
          <p class="min-w-0 flex-1">{{ t('governance.modelPicker.deleteConfirm', { name: selectedTemplate.name }) }}</p>
          <button data-test="confirm-delete-template" type="button" class="btn btn-danger text-xs" @click="removeTemplate">
            {{ t('governance.modelPicker.confirmDelete') }}
          </button>
          <button type="button" class="btn btn-secondary text-xs" @click="pendingDelete = false">
            {{ t('governance.modelPicker.cancel') }}
          </button>
        </div>
        <p v-if="collection.templates.length >= templateLimit" class="mt-2 text-xs text-amber-700 dark:text-amber-400">
          {{ t('governance.modelPicker.limitReached') }}
        </p>
        <p class="mt-2 text-xs text-gray-500">
          {{ t('governance.templatePersistedHint') }}
        </p>
      </details>
    </fieldset>
  </section>
</template>
