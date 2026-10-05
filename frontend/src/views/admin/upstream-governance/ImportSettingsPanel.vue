<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type {
  ImportAccountConfig,
  RemoteGroup,
  Transport,
} from '@/api/admin/upstream-governance'
import type { ModelSelections } from './import-config'
import ModelTemplatePanel from './ModelTemplatePanel.vue'
defineProps<{
  config: ImportAccountConfig
  models: ModelSelections
  quotaEnabled: boolean
  groups: RemoteGroup[]
  preserveModels?: Transport[]
  disabled?: boolean
}>()
const emit = defineEmits<{
  'update:config': [value: ImportAccountConfig]
  'update:models': [value: ModelSelections]
  'update:quotaEnabled': [value: boolean]
  ready: [value: boolean]
  'model-edited': [platform: Transport]
}>()
const { t } = useI18n()
function number(event: Event) {
  return Number((event.target as HTMLInputElement).value)
}
function checked(event: Event) {
  return (event.target as HTMLInputElement).checked
}
</script>
<template>
  <details class="rounded-xl border border-gray-200 dark:border-dark-600">
    <summary
      class="flex cursor-pointer flex-wrap items-center justify-between gap-2 px-4 py-4"
    >
      <span class="font-semibold">{{ t('governance.importSettings') }}</span
      ><span class="text-xs font-normal text-gray-500">{{
        t('governance.importSettingsSummary', {
          concurrency: config.concurrency,
          priority: config.priority ?? 1,
        })
      }}</span>
    </summary>
    <div class="space-y-5 border-t border-gray-100 p-4 dark:border-dark-700">
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
          :model-value="models"
          :groups="groups"
          :preserve-platforms="preserveModels"
          :disabled="disabled"
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
    </div>
  </details>
</template>
