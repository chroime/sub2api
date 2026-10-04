<script setup lang="ts">
import { computed } from 'vue'
import type { CanvasNode } from '../../types'

const props = defineProps<{ node: CanvasNode }>()
const emit = defineEmits<{ (event: 'update', patch: Partial<CanvasNode>): void }>()
const prompt = computed(() => {
  const metadata = props.node.metadata as Record<string, unknown>
  return typeof metadata.prompt === 'string' ? metadata.prompt : typeof metadata.text === 'string' ? metadata.text : ''
})
function update(value: string) { emit('update', { metadata: { prompt: value, text: value } }) }
</script>

<template>
  <label class="prompt-node" data-canvas-no-zoom>
    <span class="prompt-node__label">Prompt</span>
    <textarea :value="prompt" rows="4" data-canvas-no-zoom @pointerdown.stop @input="update(($event.target as HTMLTextAreaElement).value)" />
  </label>
</template>

<style scoped>
.prompt-node { display: flex; flex-direction: column; gap: 5px; }
.prompt-node__label { color: #64748b; font-size: 11px; font-weight: 600; }
.prompt-node textarea { min-height: 72px; resize: vertical; width: 100%; box-sizing: border-box; border: 1px solid #cbd5e1; border-radius: 5px; padding: 6px; color: #1e293b; background: #fff; font: inherit; font-size: 12px; }
</style>
