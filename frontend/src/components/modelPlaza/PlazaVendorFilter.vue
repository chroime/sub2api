<template>
  <div class="plaza-vendor-filter flex min-w-0 flex-col items-start gap-2 border-t border-gray-200 px-4 py-3 sm:flex-row sm:items-center sm:gap-3 dark:border-dark-700" data-vendor-filter>
    <span class="shrink-0 text-xs font-medium text-gray-500">{{ t('modelPlaza.vendor.treeLabel') }}</span>
    <div class="vendor-chips flex w-full min-w-0 flex-1 flex-wrap gap-1.5" data-vendor-chips>
      <button v-for="node in nodes" :key="node.key" type="button" :data-vendor="node.key" :aria-pressed="vendor === node.key"
        class="flex shrink-0 items-center gap-2 whitespace-nowrap rounded-lg border px-3 py-2 text-xs transition-colors focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-500"
        :class="vendor === node.key ? 'border-primary-200 bg-primary-50 font-semibold text-primary-700 dark:border-primary-700 dark:bg-primary-500/10 dark:text-primary-300' : 'border-transparent text-gray-600 hover:bg-gray-100 dark:text-dark-200 dark:hover:bg-dark-700'"
        @click="emit('update:vendor', node.key)">
        <ModelIcon v-if="node.sampleModel" :model="node.sampleModel" size="14px" />
        <Icon v-else name="grid" size="sm" />
        {{ node.label }}<span class="tabular-nums opacity-70">{{ t('modelPlaza.vendor.modelCount', { count: node.models.length }) }}</span>
      </button>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import ModelIcon from '@/components/common/ModelIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { modelVendorLabel } from '@/utils/modelVendor'
import type { PlazaVendorNode } from './plazaVendorTree'
const props = defineProps<{ vendors: PlazaVendorNode[]; vendor: string }>()
const emit = defineEmits<{ 'update:vendor': [value: string] }>()
const { t } = useI18n()
const modelVendorByPlatform: Record<string, string> = {
  anthropic: 'claude',
  openai: 'openai',
  gemini: 'gemini',
  grok: 'xai',
  kimi: 'moonshot',
  zhipu: 'zhipu',
  deepseek: 'deepseek',
  minimax: 'minimax'
}
const orderedVendors = computed(() => {
  const order = new Map(CONCRETE_PLATFORM_OPTIONS.flatMap(({ value }) => {
    const vendor = modelVendorByPlatform[value]
    return vendor ? [vendor] : []
  }).map((vendor, index) => [vendor, index]))
  return [...props.vendors].sort((a, b) => {
    const ai = order.get(a.key), bi = order.get(b.key)
    if (ai !== undefined || bi !== undefined) {
      if (ai === undefined) return 1
      if (bi === undefined) return -1
      return ai - bi
    }
    return a.label.localeCompare(b.label)
  })
})
const all = computed<PlazaVendorNode>(() => ({ key: 'all', label: t('modelPlaza.filters.allVendors'), sampleModel: '', models: orderedVendors.value.flatMap(v => v.models) }))
const nodes = computed(() => {
  const selected = props.vendor === 'all' ? null
    : orderedVendors.value.find(v => v.key === props.vendor) ?? { key: props.vendor, label: modelVendorLabel(props.vendor), sampleModel: '', models: [] }
  const choices = selected && !orderedVendors.value.some(v => v.key === selected.key)
    ? [...orderedVendors.value, selected]
    : orderedVendors.value
  return [all.value, ...choices]
})
</script>
