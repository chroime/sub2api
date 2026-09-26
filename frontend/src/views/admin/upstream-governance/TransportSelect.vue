<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { Transport } from '@/api/admin/upstream-governance'
defineProps<{ modelValue: Transport | ''; disabled?: boolean; id?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: Transport | ''] }>()
const { t } = useI18n()
const options = [
  { value: 'openai', label: 'OpenAI', platform: 'openai' as const },
  { value: 'anthropic', label: 'Anthropic', platform: 'anthropic' as const },
  { value: 'gemini', label: 'Gemini', platform: 'gemini' as const },
]
</script>
<template>
  <Select
    :id="id"
    :model-value="modelValue"
    :options="options"
    :disabled="disabled"
    :placeholder="t('governance.choose')"
    :aria-label="t('governance.transport')"
    @update:model-value="emit('update:modelValue', $event as Transport | '')"
  >
    <template #selected="{ option }"
      ><span class="flex items-center gap-2"
        ><PlatformIcon
          v-if="option"
          :platform="option.platform as Transport"
          size="sm"
        />{{ option?.label || t('governance.choose') }}</span
      ></template
    >
    <template #option="{ option }"
      ><span class="flex items-center gap-2"
        ><PlatformIcon :platform="option.platform" size="sm" />{{
          option.label
        }}</span
      ></template
    >
  </Select>
</template>
