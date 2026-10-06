<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import type { SiteInput, Transport } from '@/api/admin/upstream-governance'
import { governanceProviders, transportUnavailable } from './providers'
const props = defineProps<{ modelValue: Transport | ''; disabled?: boolean; id?: string; sitePlatform?: SiteInput['platform']; remotePlatform?: string; allowAll?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: Transport | ''] }>()
const { t } = useI18n()
const options = computed(() => {
  const providers = governanceProviders.map(provider => {
    const reason = transportUnavailable(provider.value, props.sitePlatform, props.remotePlatform)
    return { ...provider, platform: provider.value as string, disabled: !!reason, reason: reason ? t('governance.' + reason) : '' }
  })
  return props.allowAll ? [{ value: '' as const, label: t('governance.allProtocols'), platform: '', disabled: false, reason: '' }, ...providers] : providers
})
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
          v-if="option?.platform"
          :platform="option.platform as Transport"
          size="sm"
        />{{ option?.label || t('governance.choose') }}</span
      ></template
    >
    <template #option="{ option }"
      ><span class="flex items-center gap-2"
        ><PlatformIcon v-if="option.platform" :platform="option.platform as Transport" size="sm" class="shrink-0" /><span><span class="block">{{ option.label }}</span><span v-if="option.reason" class="block text-xs text-gray-400">{{ option.reason }}</span></span></span
      ></template
    >
  </Select>
</template>
