<template>
  <BaseDialog
    :show="show"
    :title="t('admin.channelMonitor.availabilityReset.title')"
    width="narrow"
    @close="$emit('close')"
  >
    <form class="space-y-4" @submit.prevent="submit">
      <p class="text-sm text-gray-600 dark:text-gray-400">
        {{ t('admin.channelMonitor.availabilityReset.description', { model: monitor?.primary_model ?? '' }) }}
      </p>

      <div>
        <label class="input-label" for="availability-reset-pct">
          {{ t('admin.channelMonitor.availabilityReset.percentage') }}
        </label>
        <div class="flex items-center gap-2">
          <input
            id="availability-reset-pct"
            v-model="availabilityText"
            type="text"
            inputmode="decimal"
            autocomplete="off"
            class="input min-w-0 flex-1"
            :placeholder="t('admin.channelMonitor.availabilityReset.percentagePlaceholder')"
            aria-describedby="availability-reset-error"
          />
          <span class="text-sm text-gray-500 dark:text-gray-400">%</span>
        </div>
      </div>

      <fieldset>
        <legend class="input-label">
          {{ t('admin.channelMonitor.availabilityReset.yellowBars') }}
        </legend>
        <div class="grid grid-cols-5 gap-2 sm:grid-cols-9">
          <button
            v-for="count in yellowBarOptions"
            :key="count"
            type="button"
            class="rounded-md border px-2 py-1.5 text-sm transition-colors"
            :class="yellowBars === count
              ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300'
              : 'border-gray-200 text-gray-600 hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700'"
            :aria-pressed="yellowBars === count"
            @click="yellowBars = count"
          >
            {{ count }}
          </button>
        </div>
      </fieldset>

      <div>
        <label class="input-label" for="availability-reset-layout">
          {{ t('admin.channelMonitor.availabilityReset.yellowBarLayout') }}
        </label>
        <select
          id="availability-reset-layout"
          v-model="yellowBarLayout"
          class="input w-full"
        >
          <option v-for="option in yellowBarLayoutOptions" :key="option.value" :value="option.value">
            {{ t(option.label) }}
          </option>
        </select>
      </div>

      <p v-if="errorMessage" id="availability-reset-error" class="text-sm text-red-600 dark:text-red-400">
        {{ errorMessage }}
      </p>

      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="$emit('close')">
          {{ t('common.cancel') }}
        </button>
        <button type="submit" class="btn btn-primary" :disabled="submitting">
          {{ submitting ? t('common.saving') : t('admin.channelMonitor.availabilityReset.confirm') }}
        </button>
      </div>
    </form>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ChannelMonitor, AvailabilityResetParams } from '@/api/admin/channelMonitor'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{
  show: boolean
  monitor: ChannelMonitor | null
  submitting: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', params: AvailabilityResetParams): void
}>()

const { t } = useI18n()
const availabilityText = ref('100')
const yellowBars = ref(0)
const yellowBarLayout = ref<AvailabilityResetParams['degraded_bar_layout']>('even')
const errorMessage = ref('')
const yellowBarOptions = Array.from({ length: 9 }, (_, index) => index)
const yellowBarLayoutOptions = [
  { value: 'even', label: 'admin.channelMonitor.availabilityReset.layoutEven' },
  { value: 'block_oldest', label: 'admin.channelMonitor.availabilityReset.layoutBlockOldest' },
  { value: 'block_newest', label: 'admin.channelMonitor.availabilityReset.layoutBlockNewest' },
  { value: 'random', label: 'admin.channelMonitor.availabilityReset.layoutRandom' },
] as const

watch(
  () => [props.show, props.monitor] as const,
  ([show, monitor]) => {
    if (!show || !monitor) return
    const current = Number(monitor.availability_7d)
    availabilityText.value = Number.isFinite(current) ? current.toFixed(2).replace(/\.00$/, '') : '100'
    yellowBars.value = 0
    yellowBarLayout.value = 'even'
    errorMessage.value = ''
  },
  { immediate: true }
)

function submit() {
  const raw = availabilityText.value.trim()
  const value = Number(raw)
  if (!/^\d+(?:\.\d{1,2})?$/.test(raw) || !Number.isFinite(value) || value < 0 || value > 100) {
    errorMessage.value = t('admin.channelMonitor.availabilityReset.percentageError')
    return
  }
  if (!Number.isInteger(yellowBars.value) || yellowBars.value < 0 || yellowBars.value > 8) {
    errorMessage.value = t('admin.channelMonitor.availabilityReset.yellowBarsError')
    return
  }
  errorMessage.value = ''
  emit('submit', { availability_pct: value, degraded_bars: yellowBars.value, degraded_bar_layout: yellowBarLayout.value })
}
</script>
