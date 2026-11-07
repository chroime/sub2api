<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  label: string
  tone?: 'success' | 'warning' | 'danger' | 'neutral' | 'info'
  icon?: 'checkCircle' | 'xCircle' | 'exclamationTriangle' | 'clock' | 'infoCircle' | 'eyeOff' | 'check'
}>(), { tone: 'neutral' })

const appearance = computed(() => ({
  success: { icon: 'checkCircle', color: 'text-emerald-600 dark:text-emerald-400' },
  warning: { icon: 'exclamationTriangle', color: 'text-amber-600 dark:text-amber-400' },
  danger: { icon: 'xCircle', color: 'text-red-600 dark:text-red-400' },
  neutral: { icon: 'clock', color: 'text-gray-500 dark:text-gray-400' },
  info: { icon: 'infoCircle', color: 'text-sky-600 dark:text-sky-400' },
} as const)[props.tone])
</script>

<template>
  <span
    class="group/status relative inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md align-middle outline-none focus-visible:ring-2 focus-visible:ring-primary-500"
    :class="appearance.color"
    role="img"
    :aria-label="label"
    :title="label"
    tabindex="0"
  >
    <Icon :name="icon || appearance.icon" size="sm" aria-hidden="true" />
    <span class="sr-only">{{ label }}</span>
    <span aria-hidden="true" :data-label="label" class="pointer-events-none absolute bottom-full left-1/2 z-30 mb-1 hidden w-max max-w-56 -translate-x-1/2 whitespace-normal rounded-md bg-gray-900 px-2 py-1 text-center text-xs font-normal leading-relaxed text-white shadow-sm after:content-[attr(data-label)] group-hover/status:block group-focus-visible/status:block dark:bg-gray-700" />
  </span>
</template>
