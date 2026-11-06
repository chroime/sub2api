<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type {
  ImportAccountConfig,
  RemoteGroup,
  Transport,
} from '@/api/admin/upstream-governance'
import type { ModelSelections } from './import-config'
import ModelTemplatePanel from './ModelTemplatePanel.vue'
import { providerLabel, transportPlatforms } from './providers'
// The import parameters as a one-line summary above the group table; the
// template picker, account parameters and model whitelist open in a dialog.
// The dialog stays mounted so default templates still load and apply.
const props = defineProps<{
  config: ImportAccountConfig
  models: ModelSelections
  quotaEnabled: boolean
  groups: RemoteGroup[]
  preserveModels?: Transport[]
  disabled?: boolean
  /** The template whose parameters are in the draft, '' when none or edited. */
  templateName?: string
  /** Remounts the model picker for a refreshed catalog. */
  modelsKey?: string
}>()
const emit = defineEmits<{
  'update:config': [value: ImportAccountConfig]
  'update:models': [value: ModelSelections]
  'update:quotaEnabled': [value: boolean]
  ready: [value: boolean]
  'model-edited': [platform: Transport]
  manage: []
}>()
const { t } = useI18n()
const open = ref(false)
const restrictedModels = computed(() => transportPlatforms
  .filter(platform => props.models[platform]?.enabled)
  .map(platform => t('governance.importParameters.modelCount', { platform: providerLabel(platform), count: props.models[platform].models.length })))
function manage() {
  open.value = false
  emit('manage')
}
function number(event: Event) {
  return Number((event.target as HTMLInputElement).value)
}
function checked(event: Event) {
  return (event.target as HTMLInputElement).checked
}
</script>
<template>
  <div data-test="import-parameters" class="flex flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border border-gray-200 px-4 py-3 text-sm dark:border-dark-600">
    <span class="font-medium">{{ t('governance.importSettings') }}</span>
    <p data-test="import-parameters-summary" class="min-w-0 flex-1 text-xs text-gray-600 dark:text-gray-300">
      <span>{{ t('governance.importParameters.template', { name: templateName || t('governance.importParameters.custom') }) }}</span>
      <span> · {{ t('governance.importParameters.concurrency', { n: config.concurrency }) }}</span>
      <span> · {{ t('governance.importParameters.priority', { n: config.priority ?? 1 }) }}</span>
      <span v-if="quotaEnabled"> · {{ t('governance.importParameters.quota') }}</span>
      <span> · {{ restrictedModels.length ? restrictedModels.join('，') : t('governance.importParameters.allModels') }}</span>
    </p>
    <slot name="actions" />
    <button data-test="open-import-parameters" type="button" class="btn btn-secondary text-sm" :disabled="disabled" aria-haspopup="dialog" @click="open = true">{{ t('governance.importParameters.open') }}</button>
  </div>
  <BaseDialog :show="open" :keep-mounted="true" :title="t('governance.importSettings')" width="extra-wide" @close="open = false">
    <div data-test="import-parameters-dialog" class="space-y-5">
      <slot name="template" :manage="manage" />
      <fieldset :disabled="disabled" class="grid min-w-0 gap-4 sm:grid-cols-2">
        <label class="text-sm"
          >{{ t('governance.concurrency')
          }}<input
            :value="config.concurrency"
            data-test="concurrency"
            type="number"
            min="1"
            max="2147483647"
            required
            class="input mt-1 w-full"
            @input="
              emit('update:config', { ...config, concurrency: number($event) })
            "
        /></label>
        <label class="text-sm"
          >{{ t('governance.accountPriority') }}<input
            :value="config.priority ?? 1"
            data-test="priority"
            type="number"
            min="0"
            max="2147483647"
            step="1"
            required
            class="input mt-1 w-full"
            @input="emit('update:config', { ...config, priority: number($event) })"
          /></label>
        <div class="space-y-3 sm:col-span-2">
          <label class="governance-checkbox-label flex items-center gap-2 text-sm"
            ><input
              :checked="config.upstream_billing_rate_sync_enabled"
              data-test="sync-billing"
              type="checkbox"
              class="governance-checkbox"
              @change="
                emit('update:config', {
                  ...config,
                  upstream_billing_rate_sync_enabled: checked($event),
                })
              "
            />{{ t('governance.syncUpstreamBilling') }}</label
          ><label class="governance-checkbox-label flex items-center gap-2 text-sm"
            ><input
              :checked="config.openai_long_context_billing_enabled"
              data-test="long-context"
              type="checkbox"
              class="governance-checkbox"
              @change="
                emit('update:config', {
                  ...config,
                  openai_long_context_billing_enabled: checked($event),
                })
              "
            />{{ t('governance.longContextBilling') }}</label
          >
          <p class="text-xs text-gray-500">
            {{ t('governance.longContextHint') }}
          </p>
        </div>
        <div class="sm:col-span-2">
          <label class="governance-checkbox-label flex items-center gap-2 text-sm font-medium"
            ><input
              :checked="quotaEnabled"
              data-test="quota-enabled"
              type="checkbox"
              class="governance-checkbox"
              @change="emit('update:quotaEnabled', checked($event))"
            />{{ t('governance.quotaControl') }}</label
          >
          <div v-if="quotaEnabled" class="mt-3 grid gap-3 sm:grid-cols-3">
            <label class="text-xs text-gray-500"
              >{{ t('governance.dailyQuota')
              }}<input
                :value="config.quota_daily_limit"
                data-test="daily-quota"
                class="input mt-1 w-full"
                type="number"
                min="0"
                step="any"
                required
                @input="
                  emit('update:config', {
                    ...config,
                    quota_daily_limit: number($event),
                  })
                " /></label
            ><label class="text-xs text-gray-500"
              >{{ t('governance.weeklyQuota')
              }}<input
                :value="config.quota_weekly_limit"
                data-test="weekly-quota"
                class="input mt-1 w-full"
                type="number"
                min="0"
                step="any"
                required
                @input="
                  emit('update:config', {
                    ...config,
                    quota_weekly_limit: number($event),
                  })
                " /></label
            ><label class="text-xs text-gray-500"
              >{{ t('governance.totalQuota')
              }}<input
                :value="config.quota_limit"
                data-test="total-quota"
                class="input mt-1 w-full"
                type="number"
                min="0"
                step="any"
                required
                @input="
                  emit('update:config', {
                    ...config,
                    quota_limit: number($event),
                  })
                "
            /></label>
          </div>
        </div>
      </fieldset>
      <div class="border-t border-gray-100 pt-5 dark:border-dark-700">
        <ModelTemplatePanel
          :key="modelsKey"
          :model-value="models"
          :groups="groups"
          :preserve-platforms="preserveModels"
          :disabled="disabled"
          variant="picker"
          @manage="manage"
          @update:model-value="emit('update:models', $event)"
          @ready="emit('ready', $event)"
          @edited="emit('model-edited', $event)"
        />
      </div>
      <p
        class="rounded-lg bg-primary-50 p-3 text-xs text-primary-800 dark:bg-primary-900/20 dark:text-primary-300"
      >
        {{ t('governance.nameAndNoteHint') }}
      </p>
      <div class="flex justify-end"><button data-test="close-import-parameters" type="button" class="btn btn-primary" @click="open = false">{{ t('governance.importParameters.done') }}</button></div>
    </div>
  </BaseDialog>
</template>
