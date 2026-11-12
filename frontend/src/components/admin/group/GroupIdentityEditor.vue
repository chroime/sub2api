<template>
  <section class="space-y-3" :aria-label="t('admin.groups.identity.title')">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <label :for="`${id}-label`" class="input-label mb-0">{{ t('admin.groups.identity.title') }}</label>
      <span class="text-xs text-gray-400 dark:text-dark-400">{{ t('admin.groups.identity.optional') }}</span>
    </div>
    <div class="flex flex-wrap gap-2" role="group" :aria-label="t('admin.groups.identity.presets')">
      <button
        v-for="preset in presets"
        :key="preset.key"
        type="button"
        :data-identity-preset="preset.key"
        :aria-pressed="matches(preset)"
        class="inline-flex min-h-8 items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition-colors"
        :class="matches(preset) ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-900/30 dark:text-primary-300' : 'border-gray-200 bg-white text-gray-600 hover:border-primary-300 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300'"
        @click="choose(preset)"
      >
        <Icon v-if="matches(preset)" name="check" size="xs" />
        {{ preset.title }}
      </button>
    </div>
    <div class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]">
      <div class="min-w-0">
        <input
          :id="`${id}-label`"
          :value="modelValue?.label || ''"
          :maxlength="24"
          :placeholder="t('admin.groups.identity.placeholder')"
          :aria-describedby="`${id}-count`"
          :aria-invalid="invalid || undefined"
          class="input"
          type="text"
          autocomplete="off"
          @input="setLabel(($event.target as HTMLInputElement).value)"
        />
        <div :id="`${id}-count`" class="mt-1 flex justify-between gap-2 text-xs" :class="invalid ? 'text-red-600 dark:text-red-400' : 'text-gray-400 dark:text-dark-400'">
          <span>{{ invalid ? t('admin.groups.identity.invalid') : t('admin.groups.identity.labelHint') }}</span>
          <span class="shrink-0 tabular-nums">{{ labelLength }}/12</span>
        </div>
      </div>
      <div class="flex h-10 items-center gap-1.5" role="group" :aria-label="t('admin.groups.identity.color')">
        <button
          v-for="color in colors"
          :key="color.name"
          type="button"
          :data-identity-color="color.name"
          :aria-label="t(`admin.groups.identity.colors.${color.name}`)"
          :aria-pressed="selectedColor === color.name"
          :title="t(`admin.groups.identity.colors.${color.name}`)"
          class="flex h-7 w-7 items-center justify-center rounded-full border-2 text-white transition-shadow"
          :class="[color.background, selectedColor === color.name ? 'border-white ring-2 ring-gray-400 dark:border-dark-800 dark:ring-gray-200' : 'border-transparent']"
          @click="setColor(color.name)"
        >
          <Icon v-if="selectedColor === color.name" name="check" size="xs" :stroke-width="2.5" />
        </button>
        <input
          :id="`${id}-color`"
          type="color"
          class="ml-1 h-8 w-9 cursor-pointer rounded border border-gray-200 bg-white p-0.5 dark:border-dark-600 dark:bg-dark-800"
          :value="customColor"
          :aria-label="t('admin.groups.identity.customColor')"
          :title="t('admin.groups.identity.customColor')"
          @input="setColor(($event.target as HTMLInputElement).value)"
        />
      </div>
    </div>
    <div class="flex min-h-10 flex-wrap items-center gap-3 rounded-lg bg-gray-50 px-3 py-2 dark:bg-dark-800">
      <span class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.groups.identity.preview') }}</span>
      <GroupBadge :name="name?.trim() || t('admin.groups.identity.previewName')" :platform="platform" :identity="modelValue" :show-rate="false" :truncate="false" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GroupIdentity, GroupPlatform } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'

const props = defineProps<{ id: string; modelValue?: GroupIdentity; name?: string; platform?: GroupPlatform }>()
const emit = defineEmits<{ 'update:modelValue': [identity: GroupIdentity] }>()
const { t } = useI18n()
const colors = [
  { name: 'teal', hex: '#0f766e', background: 'bg-teal-700' },
  { name: 'blue', hex: '#1d4ed8', background: 'bg-blue-700' },
  { name: 'amber', hex: '#b45309', background: 'bg-amber-700' },
  { name: 'violet', hex: '#6d28d9', background: 'bg-violet-700' },
  { name: 'rose', hex: '#be123c', background: 'bg-rose-700' },
  { name: 'gray', hex: '#4b5563', background: 'bg-gray-600' },
] as const
type Color = typeof colors[number]['name'] | `#${string}`
interface Preset { key: string; title: string; label: string; color: Color }
const presets = computed<Preset[]>(() => [
  { key: 'none', title: t('admin.groups.identity.none'), label: '', color: 'teal' },
  { key: 'self', title: t('admin.groups.identity.self'), label: t('admin.groups.identity.self'), color: 'teal' },
  { key: 'official', title: t('admin.groups.identity.official'), label: t('admin.groups.identity.official'), color: 'blue' },
  { key: 'thirdParty', title: t('admin.groups.identity.thirdParty'), label: t('admin.groups.identity.thirdParty'), color: 'amber' },
])
const selectedColor = computed(() => props.modelValue?.color || 'teal')
const customColor = computed(() => /^#[0-9a-f]{6}$/i.test(selectedColor.value)
  ? selectedColor.value : colors.find(color => color.name === selectedColor.value)?.hex || '#0f766e')
const labelLength = computed(() => Array.from(props.modelValue?.label?.trim() || '').length)
const invalid = computed(() => labelLength.value > 12 || /[\p{Cc}\p{Zl}\p{Zp}]/u.test(props.modelValue?.label || ''))
function matches(preset: Preset) {
  if (!preset.label) return !props.modelValue?.label?.trim()
  return props.modelValue?.label === preset.label && selectedColor.value === preset.color
}
function choose(preset: Preset) { emit('update:modelValue', preset.label ? { label: preset.label, color: preset.color } : {}) }
function setLabel(label: string) { emit('update:modelValue', label.trim() ? { label, color: selectedColor.value as Color } : {}) }
function setColor(value: string) {
  if (!colors.some(color => color.name === value) && !/^#[0-9a-f]{6}$/i.test(value)) return
  emit('update:modelValue', { ...props.modelValue, color: value as Color })
}
function validate() { return !invalid.value }
defineExpose({ validate })
</script>
