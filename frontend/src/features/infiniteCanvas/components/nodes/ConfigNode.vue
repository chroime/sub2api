<script setup lang="ts">
import { computed } from 'vue'
import type { CanvasNode } from '../../types'

const props = defineProps<{ node: CanvasNode }>()
const emit = defineEmits<{ (event: 'update', patch: Partial<CanvasNode>): void; (event: 'generate'): void }>()
const metadata = computed(() => props.node.metadata as Record<string, unknown>)
const value = (field: string, fallback = '') => typeof metadata.value[field] === 'string' || typeof metadata.value[field] === 'number' ? String(metadata.value[field]) : fallback
function update(field: string, raw: string) {
  const parsed = ['count'].includes(field) ? Math.max(1, Number.parseInt(raw, 10) || 1) : raw
  emit('update', { metadata: { [field]: parsed } })
}
</script>

<template>
  <div class="config-node" data-canvas-no-zoom>
    <label>Model<input :value="value('model')" data-canvas-no-zoom @pointerdown.stop @input="update('model', ($event.target as HTMLInputElement).value)" /></label>
    <label>Size<select :value="value('size', '1024x1024')" data-canvas-no-zoom @pointerdown.stop @change="update('size', ($event.target as HTMLSelectElement).value)"><option value="1024x1024">1024x1024</option><option value="1536x1024">1536x1024</option><option value="1024x1536">1024x1536</option><option value="1K">1K</option><option value="2K">2K</option><option value="4K">4K</option></select></label>
    <div class="config-node__row"><label>Quality<input :value="value('quality')" data-canvas-no-zoom @pointerdown.stop @input="update('quality', ($event.target as HTMLInputElement).value)" /></label><label>Count<input type="number" min="1" max="10" :value="value('count', '1')" data-canvas-no-zoom @pointerdown.stop @input="update('count', ($event.target as HTMLInputElement).value)" /></label></div>
    <label>Background<input :value="value('background')" data-canvas-no-zoom @pointerdown.stop @input="update('background', ($event.target as HTMLInputElement).value)" /></label>
    <button type="button" data-canvas-no-zoom @pointerdown.stop @click.stop="emit('generate')">生成图片</button>
  </div>
</template>

<style scoped>
.config-node { display: flex; flex-direction: column; gap: 6px; font-size: 11px; }
.config-node label { display: flex; flex-direction: column; gap: 3px; color: #64748b; font-weight: 600; }
.config-node input, .config-node select { box-sizing: border-box; width: 100%; border: 1px solid #cbd5e1; border-radius: 4px; padding: 4px 5px; color: #1e293b; background: #fff; font: inherit; font-weight: 400; }
.config-node button { border: 0; border-radius: 4px; padding: 5px 7px; color: #fff; background: #2563eb; cursor: pointer; font-size: 11px; }
.config-node__row { display: grid; grid-template-columns: 1fr 60px; gap: 6px; }
</style>
