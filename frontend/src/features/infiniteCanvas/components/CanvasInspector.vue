<script setup lang="ts">
import { computed } from 'vue'
import type { CanvasNode } from '../types'

const props = defineProps<{ node?: CanvasNode | null }>()
const emit = defineEmits<{ (event: 'update', patch: Partial<CanvasNode>): void; (event: 'delete'): void }>()
const text = computed(() => typeof props.node?.metadata.text === 'string' ? props.node.metadata.text : '')
const model = computed(() => typeof props.node?.metadata.model === 'string' ? props.node.metadata.model : '')
const status = computed(() => typeof props.node?.metadata.status === 'string' ? props.node.metadata.status : '')
function update(field: 'text' | 'model' | 'status', value: string) { emit('update', { metadata: { [field]: value } }) }
</script>

<template>
  <aside class="canvas-inspector w-full shrink-0 border-t border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900 lg:w-72 lg:border-l lg:border-t-0">
    <h2 class="mb-3 text-sm font-semibold text-gray-900 dark:text-gray-100">Inspector</h2>
    <div v-if="node" class="space-y-3">
      <p class="text-xs text-gray-500 dark:text-dark-400">{{ node.type }} node</p>
      <label v-if="node.type === 'prompt'" class="block text-xs text-gray-600 dark:text-gray-300">Prompt<textarea class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="text" rows="4" @input="update('text', ($event.target as HTMLTextAreaElement).value)" /></label>
      <label v-if="node.type === 'config'" class="block text-xs text-gray-600 dark:text-gray-300">Model<input class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="model" @input="update('model', ($event.target as HTMLInputElement).value)" /></label>
      <label v-if="node.type === 'image'" class="block text-xs text-gray-600 dark:text-gray-300">Status<select class="mt-1 w-full rounded-md border px-2 py-1 text-sm dark:border-dark-600 dark:bg-dark-800" :value="status" @change="update('status', ($event.target as HTMLSelectElement).value)"><option value="pending">Pending</option><option value="ready">Ready</option><option value="error">Error</option></select></label>
      <button type="button" class="text-xs text-red-600 hover:underline" @click="emit('delete')">Delete node</button>
    </div>
    <p v-else data-canvas-empty="node" class="text-sm text-gray-500 dark:text-dark-400">Select a node to inspect it.</p>
  </aside>
</template>
