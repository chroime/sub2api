<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CanvasNode } from '../types'

const props = defineProps<{ node?: CanvasNode | null; mobileOpen?: boolean }>()
const emit = defineEmits<{ (event: 'update', patch: Partial<CanvasNode>): void; (event: 'delete'): void; (event: 'close'): void }>()
const { t } = useI18n()
const prompt = computed(() => typeof props.node?.metadata.prompt === 'string' ? props.node.metadata.prompt : typeof props.node?.metadata.text === 'string' ? props.node.metadata.text : '')
const model = computed(() => typeof props.node?.metadata.model === 'string' ? props.node.metadata.model : '')
const size = computed(() => typeof props.node?.metadata.size === 'string' ? props.node.metadata.size : '')
const quality = computed(() => typeof props.node?.metadata.quality === 'string' ? props.node.metadata.quality : '')
const count = computed(() => typeof props.node?.metadata.count === 'number' ? String(props.node.metadata.count) : '')
const background = computed(() => typeof props.node?.metadata.background === 'string' ? props.node.metadata.background : '')
const status = computed(() => typeof props.node?.metadata.status === 'string' ? props.node.metadata.status : '')
function update(field: string, value: string) { emit('update', { metadata: { [field]: field === 'count' ? Math.max(1, Number.parseInt(value, 10) || 1) : value } }) }
</script>

<template>
  <aside :class="['canvas-inspector w-full shrink-0 border-t border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900 lg:static lg:block lg:w-72 lg:border-l lg:border-t-0', mobileOpen ? 'fixed inset-y-0 right-0 z-40 block w-72 shadow-xl' : 'hidden lg:block']">
    <div class="mb-3 flex items-center justify-between"><h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('infiniteCanvas.inspector.title') }}</h2><button type="button" :aria-label="t('infiniteCanvas.inspector.close')" class="text-xs text-gray-500 lg:hidden" @click="emit('close')">{{ t('infiniteCanvas.inspector.close') }}</button></div>
    <div v-if="node" class="space-y-3">
      <p class="text-xs text-gray-500 dark:text-dark-400">{{ t(`infiniteCanvas.nodeTypes.${node.type}`) }} {{ t('infiniteCanvas.inspector.node', { type: '' }).trim() }}</p>
      <label v-if="node.type === 'prompt'" class="block text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.inspector.prompt') }}<textarea data-canvas-no-zoom class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="prompt" rows="4" @pointerdown.stop @input="update('prompt', ($event.target as HTMLTextAreaElement).value)" /></label>
      <template v-if="node.type === 'config'"><label class="block text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.inspector.model') }}<input data-canvas-no-zoom class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="model" @pointerdown.stop @input="update('model', ($event.target as HTMLInputElement).value)" /></label><label class="block text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.inspector.size') }}<input data-canvas-no-zoom class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="size" @pointerdown.stop @input="update('size', ($event.target as HTMLInputElement).value)" /></label><label class="block text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.inspector.quality') }}<input data-canvas-no-zoom class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="quality" @pointerdown.stop @input="update('quality', ($event.target as HTMLInputElement).value)" /></label><label class="block text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.inspector.count') }}<input data-canvas-no-zoom type="number" min="1" max="10" class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="count" @pointerdown.stop @input="update('count', ($event.target as HTMLInputElement).value)" /></label><label class="block text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.inspector.background') }}<input data-canvas-no-zoom class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="background" @pointerdown.stop @input="update('background', ($event.target as HTMLInputElement).value)" /></label></template>
      <label v-if="node.type === 'image'" class="block text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.inspector.status') }}<select data-canvas-no-zoom class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="status" @pointerdown.stop @change="update('status', ($event.target as HTMLSelectElement).value)"><option value="pending">{{ t('infiniteCanvas.status.pending') }}</option><option value="completed">{{ t('infiniteCanvas.status.completed') }}</option><option value="failed">{{ t('infiniteCanvas.status.failed') }}</option><option value="ready">{{ t('infiniteCanvas.status.ready') }}</option><option value="error">{{ t('infiniteCanvas.status.error') }}</option></select></label>
      <button type="button" class="text-xs text-red-600 hover:underline" @click="emit('delete')">{{ t('infiniteCanvas.inspector.deleteNode') }}</button>
    </div>
    <p v-else data-canvas-empty="node" class="text-sm text-gray-500 dark:text-dark-400">{{ t('infiniteCanvas.inspector.empty') }}</p>
  </aside>
</template>
