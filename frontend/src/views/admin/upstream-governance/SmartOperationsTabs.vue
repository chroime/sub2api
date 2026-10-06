<template>
  <div ref="tabRail" class="w-full min-w-0 max-w-full overflow-x-auto rounded-2xl bg-gray-100 p-1.5 dark:bg-dark-800">
    <div
      role="tablist"
      :aria-label="t('governance.smartOperations.title')"
      aria-orientation="horizontal"
      class="flex w-max min-w-full items-center gap-1"
    >
      <button
        v-for="entry in SMART_OPERATIONS_SECTIONS"
        :id="`governance-${entry.id}-tab`"
        :key="entry.id"
        ref="tabButtons"
        type="button"
        role="tab"
        :aria-controls="modelValue === entry.id ? `governance-${entry.id}-panel` : undefined"
        :aria-selected="modelValue === entry.id"
        :tabindex="!disabled && focusedSection === entry.id ? 0 : -1"
        :disabled="disabled"
        class="flex min-h-[44px] flex-1 shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-xl px-4 py-2.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-gray-500 focus-visible:ring-offset-2 focus-visible:ring-offset-gray-100 disabled:cursor-not-allowed disabled:opacity-50 dark:focus-visible:ring-gray-400 dark:focus-visible:ring-offset-dark-800 sm:px-5"
        :class="modelValue === entry.id
          ? 'bg-gray-800 text-white shadow-sm dark:bg-dark-600 dark:text-gray-50'
          : 'text-gray-500 hover:bg-gray-200/70 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-100'"
        @focus="focusedSection = entry.id"
        @click="selectSection(entry.id)"
        @keydown="onKeydown($event, entry.id)"
      >
        <Icon :name="entry.icon" size="sm" class="shrink-0" aria-hidden="true" />
        <span>{{ t(entry.labelKey) }}</span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { SMART_OPERATIONS_SECTIONS, type SmartOperationsSection } from '@/config/smartOperations'

const props = withDefaults(defineProps<{
  modelValue: SmartOperationsSection
  disabled?: boolean
}>(), {
  disabled: false
})

const emit = defineEmits<{
  (event: 'update:modelValue', section: SmartOperationsSection): void
}>()

const { t } = useI18n()
const tabRail = ref<HTMLElement | null>(null)
const tabButtons = ref<HTMLButtonElement[]>([])
const focusedSection = ref<SmartOperationsSection>(props.modelValue)

watch(() => props.modelValue, section => {
  focusedSection.value = section
  revealSelectedTab()
}, { flush: 'post' })
onMounted(revealSelectedTab)

function revealSelectedTab() {
  const rail = tabRail.value
  const button = tabButtons.value.find(item => item.id === `governance-${props.modelValue}-tab`)
  if (!rail || !button || rail.scrollWidth <= rail.clientWidth) return

  const railBounds = rail.getBoundingClientRect()
  const buttonBounds = button.getBoundingClientRect()
  const left = railBounds.left + rail.clientLeft
  const right = left + rail.clientWidth
  const offset = buttonBounds.left < left
    ? buttonBounds.left - left
    : buttonBounds.right > right ? buttonBounds.right - right : 0

  // Keep route-driven selection visible without scrolling the page or moving focus.
  if (offset) rail.scrollLeft = Math.max(0, Math.min(rail.scrollWidth - rail.clientWidth, rail.scrollLeft + offset))
}

function selectSection(section: SmartOperationsSection) {
  if (props.disabled) return
  focusedSection.value = section
  emit('update:modelValue', section)
}

function onKeydown(event: KeyboardEvent, section: SmartOperationsSection) {
  if (props.disabled) return

  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    selectSection(section)
    return
  }

  const index = SMART_OPERATIONS_SECTIONS.findIndex(entry => entry.id === section)
  const lastIndex = SMART_OPERATIONS_SECTIONS.length - 1
  let nextIndex: number
  switch (event.key) {
    case 'ArrowRight':
      nextIndex = index === lastIndex ? 0 : index + 1
      break
    case 'ArrowLeft':
      nextIndex = index === 0 ? lastIndex : index - 1
      break
    case 'Home':
      nextIndex = 0
      break
    case 'End':
      nextIndex = lastIndex
      break
    default:
      return
  }

  event.preventDefault()
  const nextSection = SMART_OPERATIONS_SECTIONS[nextIndex]
  if (!nextSection) return
  focusedSection.value = nextSection.id
  tabButtons.value.find(button => button.id === `governance-${nextSection.id}-tab`)?.focus()
}
</script>
