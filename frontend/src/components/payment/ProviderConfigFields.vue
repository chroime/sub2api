<template>
  <div class="space-y-3">
    <div v-for="field in fields" :key="field.key">
      <label class="input-label" :for="`${fieldPrefix}-${field.key}`">
        {{ field.label || t(`admin.settings.payment.field_${field.key}`) }}
        <span v-if="field.optional" class="text-xs text-gray-400">({{ t('common.optional') }})</span>
        <span v-else class="text-red-500"> *</span>
      </label>
      <textarea v-if="field.sensitive && field.key.toLowerCase().includes('key') && field.key !== 'pkey'"
        :id="`${fieldPrefix}-${field.key}`" :value="modelValue[field.key]" rows="3" class="input font-mono text-xs"
        autocomplete="new-password" data-1p-ignore data-lpignore="true" data-bwignore="true" spellcheck="false"
        :placeholder="editing ? t('admin.accounts.leaveEmptyToKeep') : ''"
        @input="update(field.key, ($event.target as HTMLTextAreaElement).value)" />
      <div v-else-if="field.sensitive" class="relative">
        <input :id="`${fieldPrefix}-${field.key}`" :type="visible[field.key] ? 'text' : 'password'" :value="modelValue[field.key]" class="input pr-10"
          autocomplete="new-password" data-1p-ignore data-lpignore="true" data-bwignore="true" spellcheck="false"
          :placeholder="editing ? t('admin.accounts.leaveEmptyToKeep') : (field.defaultValue || '')"
          @input="update(field.key, ($event.target as HTMLInputElement).value)" />
        <button type="button" class="absolute inset-y-0 right-0 px-3 text-gray-400" :aria-label="t('common.view')" @click="visible[field.key] = !visible[field.key]">
          <Icon :name="visible[field.key] ? 'eyeOff' : 'eye'" size="sm" />
        </button>
      </div>
      <Select v-else-if="field.options?.length" :id="`${fieldPrefix}-${field.key}`" :model-value="modelValue[field.key]" :options="field.options" :searchable="field.options.length > 5"
        @update:model-value="update(field.key, String($event ?? ''))" />
      <input v-else :id="`${fieldPrefix}-${field.key}`" type="text" :value="modelValue[field.key]" class="input" :placeholder="field.defaultValue || ''" @input="update(field.key, ($event.target as HTMLInputElement).value)" />
      <p v-if="field.hintKey" class="mt-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t(field.hintKey) }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { ConfigFieldDef } from './providerConfig'

const props = withDefaults(defineProps<{ fields: ConfigFieldDef[]; modelValue: Record<string, string>; editing?: boolean; fieldPrefix?: string }>(), { editing: false, fieldPrefix: 'provider-config' })
const emit = defineEmits<{ 'update:modelValue': [value: Record<string, string>] }>()
const { t } = useI18n()
const visible = reactive<Record<string, boolean>>({})
function update(key: string, value: string) { emit('update:modelValue', { ...props.modelValue, [key]: value }) }
</script>
