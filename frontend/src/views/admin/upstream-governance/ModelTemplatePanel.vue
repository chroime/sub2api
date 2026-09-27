<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { getModelsByPlatform } from '@/composables/useModelWhitelist'
import api, {
  type ModelTemplate,
  type ModelTemplateCollection,
  type RemoteGroup,
  type Transport,
} from '@/api/admin/upstream-governance'
import type { ModelSelections } from './import-config'
import { errorKey } from './feedback'
import { providerLabel, transportPlatforms } from './providers'
const props = defineProps<{
  modelValue: ModelSelections
  groups: RemoteGroup[]
  disabled?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: ModelSelections]
  ready: [value: boolean]
}>()
const { t } = useI18n()
const platforms = transportPlatforms
const platform = ref<Transport>('openai')
const collection = ref<ModelTemplateCollection>({ version: 0, templates: [] })
const loading = ref(true)
const saving = ref(false)
const error = ref('')
const search = ref('')
const custom = ref('')
const templateName = ref('')
const saveAsDefault = ref(true)
const templateID = ref('')
let generation = 0
let templatesLoaded = false
const edited: Partial<Record<Transport, boolean>> = {}
const current = computed(() => props.modelValue[platform.value])
const availableTemplates = computed(() =>
  collection.value.templates.filter(
    (template) => template.platform === platform.value,
  ),
)
const selectedTemplate = computed(() =>
  availableTemplates.value.find((template) => template.id === templateID.value),
)
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
const availableModels = computed(() => [
  ...new Set([
    ...current.value.models,
    ...upstreamModels(platform.value),
    ...getModelsByPlatform(platform.value),
  ]),
])
const filteredModels = computed(() =>
  availableModels.value.filter((model) =>
    model.toLowerCase().includes(search.value.toLowerCase()),
  ),
)
function update(models: string[], enabled = current.value.enabled) {
  edited[platform.value] = true
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
function loadTemplate() {
  if (!selectedTemplate.value) return
  update(selectedTemplate.value.models, true)
  templateName.value = selectedTemplate.value.name
  saveAsDefault.value = selectedTemplate.value.is_default
}
function addCustom() {
  const model = custom.value.trim()
  if (!model || model.length > 200 || /[\s*\u0000-\u001f\u007f]/.test(model)) {
    error.value = t('governance.invalidModelName')
    return
  }
  update([...current.value.models, model], true)
  custom.value = ''
  error.value = ''
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
  }
  emit('update:modelValue', selections)
}
async function persist(templates: ModelTemplate[]) {
  const request = generation
  saving.value = true
  error.value = ''
  try {
    const saved = await api.saveModelTemplates({
      version: collection.value.version,
      templates,
    })
    if (request !== generation) return
    collection.value = saved
  } catch (e) {
    if (request !== generation) return
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
  } finally {
    if (request === generation) saving.value = false
  }
}
async function saveTemplate() {
  if (
    !templateName.value.trim() ||
    !current.value.models.length ||
    current.value.models.length > 500
  ) {
    error.value = t('governance.templateInvalid')
    return
  }
  const request = generation
  const id =
    selectedTemplate.value?.id ||
    `tpl_${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}_${Math.random().toString(36).slice(2)}`}`
  const template: ModelTemplate = {
    id,
    name: templateName.value.trim(),
    platform: platform.value,
    models: [...current.value.models],
    is_default: saveAsDefault.value,
  }
  const templates = collection.value.templates
    .filter((item) => item.id !== id)
    .map((item) =>
      template.is_default && item.platform === template.platform
        ? { ...item, is_default: false }
        : item,
    )
  await persist([...templates, template])
  if (request === generation && !error.value) templateID.value = id
}
async function removeTemplate() {
  if (!selectedTemplate.value) return
  const request = generation
  await persist(
    collection.value.templates.filter((item) => item.id !== templateID.value),
  )
  if (request === generation && !error.value) {
    templateID.value = ''
    templateName.value = ''
  }
}
watch(platform, () => {
  templateID.value = ''
  templateName.value = ''
  search.value = ''
  custom.value = ''
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
        }}
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
        <select
          v-model="templateID"
          data-test="model-template"
          class="input ml-auto min-w-48 text-sm"
          :aria-label="t('governance.modelTemplate')"
          @change="loadTemplate"
        >
          <option value="">{{ t('governance.selectTemplate') }}</option>
          <option
            v-for="template in availableTemplates"
            :key="template.id"
            :value="template.id"
          >
            {{ template.name
            }}{{
              template.is_default ? ' · ' + t('governance.defaultTemplate') : ''
            }}
          </option>
        </select>
      </div>
      <p
        v-if="!current.enabled"
        class="rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-900"
      >
        {{ t('governance.modelsUnrestricted') }}
      </p>
      <template v-else>
        <div class="flex flex-wrap gap-2">
          <input
            v-model="search"
            class="input min-w-40 flex-1 text-sm"
            :placeholder="t('governance.searchModels')"
            :aria-label="t('governance.searchModels')"
          /><button
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
          class="grid max-h-44 grid-cols-1 gap-1 overflow-y-auto rounded-lg border border-gray-200 p-2 sm:grid-cols-2 dark:border-dark-600"
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
            /><span class="truncate" :title="model">{{ model }}</span></label
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
            :placeholder="t('governance.customModel')"
            :aria-label="t('governance.customModel')"
            @keydown.enter.prevent="addCustom"
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
          ><button
            data-test="save-template"
            type="button"
            class="btn btn-secondary"
            :disabled="!current.models.length || !templateName.trim()"
            @click="saveTemplate"
          >
            {{ t('governance.saveTemplate') }}</button
          ><button
            v-if="selectedTemplate"
            data-test="delete-template"
            type="button"
            class="btn btn-secondary text-red-600"
            @click="removeTemplate"
          >
            {{ t('common.delete') }}
          </button>
        </div>
        <p class="mt-2 text-xs text-gray-500">
          {{ t('governance.templatePersistedHint') }}
        </p>
      </details>
    </fieldset>
  </section>
</template>
