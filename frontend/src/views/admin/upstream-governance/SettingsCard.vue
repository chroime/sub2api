<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import StatusIndicator from './StatusIndicator.vue'

// A settings block shown as a compact summary. Its form opens in a dialog so
// the page stays readable; the owning panel keeps drafts and save logic.
withDefaults(defineProps<{
  title: string
  hint?: string
  status?: string
  tone?: 'on' | 'off' | 'warn'
  testId: string
  disabled?: boolean
  busy?: boolean
  dialogTitle?: string
  width?: 'normal' | 'wide' | 'extra-wide'
  /** Shown at the top of the dialog, where the page-level message is covered. */
  error?: string
  notice?: string
}>(), { tone: 'off', width: 'wide' })
const open = defineModel<boolean>('open', { default: false })
const { t } = useI18n()
</script>
<template>
  <section :data-test="testId" class="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
    <header class="flex flex-wrap items-center gap-2">
      <div class="min-w-0 flex-1">
        <h3 class="font-semibold" :title="hint">{{ title }}</h3>
      </div>
      <StatusIndicator v-if="status" :data-test="`${testId}-status`" :label="status" :tone="tone === 'on' ? 'success' : tone === 'warn' ? 'warning' : 'neutral'" />
      <slot name="actions" />
      <button v-if="$slots.form" type="button" :data-test="`${testId}-open`" class="btn btn-secondary text-sm" :disabled="disabled" :aria-haspopup="'dialog'" @click="open = true">{{ t('governance.settingsCard.configure') }}</button>
    </header>
    <div v-if="$slots.default" class="mt-3 min-w-0 text-sm"><slot /></div>
    <BaseDialog v-if="$slots.form" :show="open" :title="dialogTitle || title" :width="width" :show-close-button="!busy" :close-on-escape="!busy" @close="open = false">
      <template v-if="$slots['dialog-actions']" #header-actions><slot name="dialog-actions" /></template>
      <div :data-test="`${testId}-dialog`" class="space-y-4">
        <p v-if="error" role="alert" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-900/10 dark:text-red-200">{{ error }}</p>
        <p v-if="notice" role="status" class="rounded-lg border border-primary-100 bg-primary-50 p-3 text-sm text-primary-800 dark:border-primary-900/40 dark:bg-primary-900/10 dark:text-primary-300">{{ notice }}</p>
        <slot name="form" />
      </div>
    </BaseDialog>
  </section>
</template>
