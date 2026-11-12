<template>
  <span
    v-if="label"
    data-group-identity
    class="inline-flex max-w-full shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-semibold leading-4 text-white"
    :class="colorClass"
    :style="customColorStyle"
    :title="label"
  >
    <Icon name="shield" size="xs" class="h-3 w-3 shrink-0" aria-hidden="true" />
    <span class="max-w-[8rem] truncate">{{ label }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { GroupIdentity } from '@/types'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ identity?: GroupIdentity | null }>()
const label = computed(() => typeof props.identity?.label === 'string' ? props.identity.label.trim() : '')

const customColorStyle = computed(() => {
  const color = props.identity?.color
  if (typeof color !== 'string' || !/^#[0-9a-f]{6}$/i.test(color)) return undefined
  const backgroundColor = color.toLowerCase()
  const channels = [1, 3, 5].map(index => {
    const channel = parseInt(backgroundColor.slice(index, index + 2), 16) / 255
    return channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4
  })
  const luminance = 0.2126 * channels[0]! + 0.7152 * channels[1]! + 0.0722 * channels[2]!
  const whiteContrast = 1.05 / (luminance + 0.05)
  const blackContrast = (luminance + 0.05) / 0.05
  return { backgroundColor, color: whiteContrast >= blackContrast ? '#ffffff' : '#000000' }
})

const colorClass = computed(() => {
  if (customColorStyle.value) return undefined
  switch (props.identity?.color) {
    case 'blue': return 'bg-blue-700'
    case 'amber': return 'bg-amber-700'
    case 'violet': return 'bg-violet-700'
    case 'rose': return 'bg-rose-700'
    case 'gray': return 'bg-gray-600'
    default: return 'bg-teal-700'
  }
})
</script>
