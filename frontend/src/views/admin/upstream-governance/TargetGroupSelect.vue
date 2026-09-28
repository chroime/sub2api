<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onUnmounted, ref, watch, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { providerLabel } from './providers'
import type { Transport } from '@/api/admin/upstream-governance'
import type { GroupPlatform } from '@/types'

const props = defineProps<{
  modelValue: number[]
  groups: { id: number; name: string; platform: string; rate_multiplier: number }[]
  disabled?: boolean
  id?: string
  ariaLabel?: string
  placeholder?: string
  transport?: Transport | ''
}>()
const emit = defineEmits<{ 'update:modelValue': [value: number[]] }>()
const { t } = useI18n()
const trigger = ref<HTMLButtonElement>(), menu = ref<HTMLElement>(), search = ref<HTMLInputElement>()
const clear = ref<HTMLButtonElement>()
const listboxId = `governance-targets-${getCurrentInstance()!.uid}`
const open = ref(false), query = ref(''), focused = ref(-1), style = ref<CSSProperties>({})
const options = computed(() => props.groups.map(group => ({
  value: group.id,
  label: `${group.name} · ${group.platform === 'composite' ? t('governance.compositeGroup') : providerLabel(group.platform)} · ${group.rate_multiplier}×`,
  platform: group.platform,
  name: group.name,
  multiplier: group.rate_multiplier,
  disabled: props.transport !== undefined && group.platform !== props.transport && group.platform !== 'composite',
})))
const visible = computed(() => options.value.filter(group => group.label.toLowerCase().includes(query.value.trim().toLowerCase())))
const selected = computed(() => props.modelValue.map(id => options.value.find(group => group.value === id) || {
  value: id, label: t('governance.unavailableTarget', { id }), name: t('governance.unavailableTarget', { id }), platform: 'unknown',
}))
const title = computed(() => selected.value.map(group => group.label).join(', '))
const unavailable = (group: { value: number; disabled: boolean }) => !props.modelValue.includes(group.value) && (group.disabled || props.modelValue.length >= 100)
function position() {
  if (!open.value || !trigger.value) return
  const rect = trigger.value.getBoundingClientRect()
  const above = window.innerHeight - rect.bottom < 220 && rect.top > window.innerHeight - rect.bottom
  const width = Math.min(Math.max(rect.width, 280), window.innerWidth - 16)
  style.value = {
    width: `${width}px`, left: `${Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))}px`,
    maxHeight: `${Math.max(0, Math.min(360, (above ? rect.top : window.innerHeight - rect.bottom) - 14))}px`,
    ...(above ? { bottom: `${window.innerHeight - rect.top + 6}px` } : { top: `${rect.bottom + 6}px` }),
  }
}
function close(restoreFocus = false) {
  open.value = false
  if (restoreFocus) trigger.value?.focus()
}
function outside(event: Event) {
  const target = event.target as Node
  if (!trigger.value?.contains(target) && !menu.value?.contains(target)) close()
}
function removeListeners() {
  document.removeEventListener('pointerdown', outside)
  window.removeEventListener('resize', position)
  window.removeEventListener('scroll', position, true)
}
watch(open, async value => {
  removeListeners()
  if (!value) { query.value = ''; focused.value = -1; return }
  position()
  document.addEventListener('pointerdown', outside)
  window.addEventListener('resize', position)
  window.addEventListener('scroll', position, true)
  await nextTick()
  if (open.value) search.value?.focus()
})
watch(() => props.disabled, disabled => { if (disabled) close() })
watch(query, () => { focused.value = -1 })
onUnmounted(removeListeners)
function toggle(id: number) {
  const option = options.value.find(group => group.value === id)
  if (props.disabled || !option || unavailable(option)) return
  emit('update:modelValue', props.modelValue.includes(id) ? props.modelValue.filter(value => value !== id) : [...new Set([...props.modelValue, id])])
}
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(true); return }
  if (event.key === 'Tab') {
    if (!event.shiftKey && event.target === search.value && clear.value && !clear.value.disabled) {
      event.preventDefault(); clear.value.focus()
    } else if (event.shiftKey && event.target === clear.value) {
      event.preventDefault(); search.value?.focus()
    } else close(true)
    return
  }
  if (event.target !== search.value) return
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    const step = event.key === 'ArrowDown' ? 1 : -1
    let index = focused.value < 0 ? (step > 0 ? 0 : visible.value.length - 1) : focused.value + step
    while (index >= 0 && index < visible.value.length && unavailable(visible.value[index]!)) index += step
    if (index >= 0 && index < visible.value.length) {
      focused.value = index
      nextTick(() => menu.value?.querySelector<HTMLElement>(`[data-option-index="${index}"]`)?.scrollIntoView?.({ block: 'nearest' }))
    }
  } else if (event.key === 'Enter') {
    event.preventDefault()
    const option = visible.value[focused.value]
    if (option) toggle(option.value)
  }
}
</script>

<template>
  <div class="min-w-0">
    <button :id="id" ref="trigger" type="button" class="input flex w-full items-center gap-2 text-left text-sm disabled:cursor-not-allowed disabled:opacity-50" :disabled="disabled" :aria-label="ariaLabel || t('governance.localGroup')" aria-haspopup="listbox" :aria-expanded="open" :aria-controls="open ? listboxId : undefined" :title="title" @click="open = !open" @keydown.down.prevent="!disabled && (open = true)">
      <PlatformIcon v-if="selected[0]" :platform="selected[0].platform as GroupPlatform" size="sm" class="shrink-0" />
      <span class="min-w-0 flex-1 truncate" :class="!selected.length && 'text-gray-400'">{{ selected[0]?.name || placeholder || t('governance.chooseMultipleGroups') }}</span>
      <span v-if="selected.length > 1" class="shrink-0 rounded bg-primary-50 px-1.5 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">+{{ selected.length - 1 }}</span>
      <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400" :class="open && 'rotate-180'" />
    </button>
    <Teleport to="body">
      <div v-if="open" ref="menu" :style="style" class="fixed z-[9999] flex flex-col overflow-hidden rounded-xl border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-800" @keydown="keydown">
        <div class="flex shrink-0 items-center gap-2 border-b border-gray-100 px-3 py-2 dark:border-dark-700">
          <Icon name="search" size="sm" class="text-gray-400" /><input ref="search" v-model="query" role="combobox" aria-expanded="true" aria-autocomplete="list" :aria-controls="listboxId" :aria-activedescendant="focused >= 0 && visible[focused] ? `${listboxId}-${visible[focused]!.value}` : undefined" class="min-w-0 flex-1 bg-transparent text-sm outline-none" :aria-label="t('governance.searchLocalGroups')" :placeholder="t('governance.searchLocalGroups')" />
        </div>
        <div :id="listboxId" role="listbox" aria-multiselectable="true" :aria-label="ariaLabel || t('governance.localGroup')" class="min-h-0 overflow-y-auto p-1.5">
          <div v-for="(group, index) in visible" :id="`${listboxId}-${group.value}`" :key="group.value" role="option" :aria-selected="modelValue.includes(group.value)" :aria-disabled="unavailable(group)" :data-option-index="index" class="flex items-center gap-2 rounded-lg px-2.5 py-2.5 text-sm" :class="[unavailable(group) ? 'cursor-not-allowed opacity-50' : 'cursor-pointer hover:bg-gray-50 dark:hover:bg-dark-700', focused === index && 'bg-gray-100 dark:bg-dark-700', modelValue.includes(group.value) && 'text-primary-700 dark:text-primary-300']" @mousedown.prevent @click="toggle(group.value); search?.focus()" @mouseenter="!unavailable(group) && (focused = index)">
            <span aria-hidden="true" class="flex h-4 w-4 shrink-0 items-center justify-center rounded border" :class="modelValue.includes(group.value) ? 'border-primary-500 bg-primary-500 text-white' : 'border-gray-300 dark:border-dark-500'"><Icon v-if="modelValue.includes(group.value)" name="check" size="xs" /></span>
            <PlatformIcon :platform="group.platform as GroupPlatform" size="sm" class="shrink-0" />
            <span class="min-w-0 flex-1"><span class="block truncate">{{ group.name }}</span><span class="block text-xs text-gray-500">{{ group.platform === 'composite' ? t('governance.compositeGroup') : providerLabel(group.platform) }}<span v-if="group.disabled"> · {{ t('governance.targetProtocolMismatch') }}</span></span></span>
            <span class="shrink-0 text-xs tabular-nums text-gray-500">{{ group.multiplier }}×</span>
          </div>
          <p v-if="!visible.length" class="p-3 text-center text-xs text-gray-500">{{ t('governance.noMatchingGroups') }}</p>
        </div>
        <div class="flex shrink-0 items-center justify-between gap-3 border-t border-gray-100 px-3 py-2 text-xs dark:border-dark-700">
          <span class="text-gray-500">{{ t(modelValue.length >= 100 ? 'governance.targetLimit' : 'governance.selectedTargets', { count: modelValue.length }) }}</span>
          <button ref="clear" data-test="clear-targets" type="button" class="text-primary-600 disabled:text-gray-400" :disabled="disabled || !modelValue.length" @click="emit('update:modelValue', []); search?.focus()">{{ t('governance.clearTargetGroups') }}</button>
        </div>
      </div>
    </Teleport>
  </div>
</template>
