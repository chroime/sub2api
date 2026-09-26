<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { providerLabel } from './providers'
import type { Transport } from '@/api/admin/upstream-governance'
import type { GroupPlatform } from '@/types'

const props = defineProps<{
  modelValue: number
  groups: { id: number; name: string; platform: string; rate_multiplier: number }[]
  disabled?: boolean
  id?: string
  ariaLabel?: string
  placeholder?: string
  transport?: Transport | ''
}>()
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()
const { t } = useI18n()
const options = computed(() => props.groups.map(group => ({
  value: group.id,
  label: `${group.name} · ${group.platform === 'composite' ? t('governance.compositeGroup') : providerLabel(group.platform)} · ${group.rate_multiplier}×`,
  platform: group.platform,
  name: group.name,
  multiplier: group.rate_multiplier,
  disabled: props.transport !== undefined && group.platform !== props.transport && group.platform !== 'composite',
})))
</script>

<template>
  <Select
    :id="id"
    :model-value="modelValue || null"
    :options="options"
    :disabled="disabled"
    :placeholder="placeholder || t('governance.choose')"
    :aria-label="ariaLabel || t('governance.localGroup')"
    clearable
    @update:model-value="emit('update:modelValue', $event == null ? 0 : Number($event))"
  >
    <template #selected="{ option }">
      <span class="flex min-w-0 items-center gap-2">
        <PlatformIcon v-if="option" :platform="option.platform as GroupPlatform" size="sm" class="shrink-0" />
        <span class="truncate">{{ option?.name || placeholder || t('governance.choose') }}</span>
        <span v-if="option" class="ml-auto shrink-0 text-xs text-gray-500">{{ option.multiplier }}×</span>
      </span>
    </template>
    <template #option="{ option }">
      <span class="flex min-w-0 items-center gap-2">
        <PlatformIcon :platform="option.platform as GroupPlatform" size="sm" class="shrink-0" />
        <span class="min-w-0"><span class="block truncate">{{ option.name }}</span><span class="block text-xs text-gray-500">{{ option.platform === 'composite' ? t('governance.compositeGroup') : providerLabel(String(option.platform)) }}<span v-if="option.disabled"> · {{ t('governance.targetProtocolMismatch') }}</span></span></span>
        <span class="ml-auto shrink-0 text-xs tabular-nums text-gray-500">{{ option.multiplier }}×</span>
      </span>
    </template>
  </Select>
</template>
