<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ImportAccountConfig } from '@/api/admin/upstream-governance'
import type { ImportTemplate } from '@/api/admin/upstream-import-templates'
import { defaultImportConfig, defaultModelSelections, type ModelSelections } from './import-config'
import { applyImportTemplateSettings, importTemplateSettings } from './import-templates'
import ImportTemplatePanel from './ImportTemplatePanel.vue'
import ModelTemplatePanel from './ModelTemplatePanel.vue'

// Global settings page for the shared import templates. The account
// parameter form here is only the editor for a template, not an import draft.
const emit = defineEmits<{ busy: [value: boolean] }>()
const { t } = useI18n()
const config = ref<ImportAccountConfig>(defaultImportConfig())
const quotaEnabled = ref(true)
const models = ref<ModelSelections>(defaultModelSelections())
const settings = computed(() => importTemplateSettings(config.value, quotaEnabled.value))
const busy = ref(false)

function load(template: ImportTemplate | null) {
  const next = template ? applyImportTemplateSettings(defaultImportConfig(), template.settings) : { config: defaultImportConfig(), quotaEnabled: true }
  config.value = next.config
  quotaEnabled.value = next.quotaEnabled
}
function setBusy(value: boolean) {
  busy.value = value
  emit('busy', value)
}
function number(event: Event) { return Number((event.target as HTMLInputElement).value) }
function checked(event: Event) { return (event.target as HTMLInputElement).checked }
</script>
<template>
  <div data-test="import-templates-view" class="min-w-0 space-y-5" :aria-label="t('governance.templatesTab.title')">
    <section class="min-w-0 space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5">
      <ImportTemplatePanel variant="library" scope-key="library" :settings="settings" :pristine="false" @selected="load" @busy="setBusy" />
      <fieldset data-test="template-parameters" :disabled="busy" class="grid min-w-0 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <label class="text-sm">{{ t('governance.concurrency') }}<input :value="config.concurrency" data-test="concurrency" type="number" min="1" max="2147483647" class="input mt-1 w-full" @input="config = { ...config, concurrency: number($event) }" /></label>
        <label class="text-sm">{{ t('governance.accountPriority') }}<input :value="config.priority ?? 1" data-test="priority" type="number" min="0" max="2147483647" step="1" class="input mt-1 w-full" @input="config = { ...config, priority: number($event) }" /></label>
        <label class="governance-checkbox-label flex items-center gap-2 text-sm sm:col-span-2"><input :checked="config.upstream_billing_rate_sync_enabled" type="checkbox" class="governance-checkbox" @change="config = { ...config, upstream_billing_rate_sync_enabled: checked($event) }" />{{ t('governance.syncUpstreamBilling') }}</label>
        <label class="governance-checkbox-label flex items-center gap-2 text-sm sm:col-span-2"><input :checked="config.openai_long_context_billing_enabled" type="checkbox" class="governance-checkbox" @change="config = { ...config, openai_long_context_billing_enabled: checked($event) }" />{{ t('governance.longContextBilling') }}</label>
        <label class="governance-checkbox-label flex items-center gap-2 text-sm font-medium sm:col-span-2 lg:col-span-4"><input :checked="quotaEnabled" data-test="quota-enabled" type="checkbox" class="governance-checkbox" @change="quotaEnabled = checked($event)" />{{ t('governance.quotaControl') }}</label>
        <template v-if="quotaEnabled">
          <label class="text-xs text-gray-500">{{ t('governance.dailyQuota') }}<input :value="config.quota_daily_limit" class="input mt-1 w-full" type="number" min="0" step="any" @input="config = { ...config, quota_daily_limit: number($event) }" /></label>
          <label class="text-xs text-gray-500">{{ t('governance.weeklyQuota') }}<input :value="config.quota_weekly_limit" class="input mt-1 w-full" type="number" min="0" step="any" @input="config = { ...config, quota_weekly_limit: number($event) }" /></label>
          <label class="text-xs text-gray-500">{{ t('governance.totalQuota') }}<input :value="config.quota_limit" class="input mt-1 w-full" type="number" min="0" step="any" @input="config = { ...config, quota_limit: number($event) }" /></label>
        </template>
      </fieldset>
    </section>
    <section class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5">
      <ModelTemplatePanel v-model="models" variant="library" :groups="[]" />
    </section>
  </div>
</template>
