<template>
  <fieldset class="rounded-xl border border-gray-200 p-4 dark:border-dark-600" data-testid="group-streaming-ack">
    <legend class="px-1 text-sm font-medium text-gray-900 dark:text-gray-100">
      {{ t('admin.groups.streamingACK.title') }}
    </legend>
    <div class="flex flex-wrap items-center justify-between gap-3">
      <p class="min-w-0 flex-1 text-sm leading-6 text-gray-500 dark:text-gray-400">
        {{ t('admin.groups.streamingACK.description') }}
      </p>
      <div class="inline-flex shrink-0 gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800">
        <button
          v-for="option in options"
          :key="String(option.value)"
          type="button"
          class="rounded-md px-4 py-1.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
          :class="modelValue === option.value
            ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-600 dark:text-primary-300'
            : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100'"
          :aria-pressed="modelValue === option.value"
          :data-testid="option.value ? 'group-ack-enabled' : 'group-ack-disabled'"
          @click="$emit('update:modelValue', option.value)"
        >
          {{ t(option.label) }}
        </button>
      </div>
    </div>
    <p v-if="modelValue === null" class="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-xs leading-5 text-amber-800 dark:bg-amber-900/15 dark:text-amber-300">
      {{ t('admin.groups.streamingACK.legacyHint') }}
    </p>
  </fieldset>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ modelValue: boolean | null }>()
defineEmits<{ 'update:modelValue': [value: boolean] }>()
const { t } = useI18n()
const options = [
  { value: true, label: 'admin.groups.streamingACK.enabled' },
  { value: false, label: 'admin.groups.streamingACK.disabled' }
]
</script>
