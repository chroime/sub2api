<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { CanvasNode as CanvasNodeModel } from '../types'
import PromptNode from './nodes/PromptNode.vue'
import ConfigNode from './nodes/ConfigNode.vue'
import ImageNode from './nodes/ImageNode.vue'
import type { ImageModel } from '@/api/imageGeneration'

const props = defineProps<{ node: CanvasNodeModel; selected?: boolean; panMode?: boolean; imageUrl?: string; imageModels?: ImageModel[]; imageModelsLoading?: boolean; imageModelsError?: string }>()
const { t } = useI18n()
const emit = defineEmits<{
  (event: 'select', nodeId: string, additive: boolean): void
  (event: 'move', nodeId: string, screenDelta: { x: number; y: number }): void
  (event: 'delete', nodeId: string): void
  (event: 'connect-start', nodeId: string): void
  (event: 'update', nodeId: string, patch: Partial<CanvasNodeModel>): void
  (event: 'retry', nodeId: string): void
  (event: 'generate', nodeId: string): void
  (event: 'retry-models'): void
}>()
const dragging = ref(false)
const dragStart = ref({ x: 0, y: 0 })
const pendingDelta = ref({ x: 0, y: 0 })
const emittedDelta = ref({ x: 0, y: 0 })
let dragFrame: number | undefined
const size = computed(() => props.node.size ?? { width: 240, height: 140 })
const label = computed(() => t(`infiniteCanvas.nodeTypes.${props.node.type}`))
const metadataSummary = computed(() => {
  const keys = Object.keys(props.node.metadata).filter((key) => !['text', 'prompt', 'model', 'url', 'status'].includes(key))
  return keys.length ? t(keys.length === 1 ? 'infiniteCanvas.node.metadataField' : 'infiniteCanvas.node.metadataFields', { count: keys.length }) : t('infiniteCanvas.node.placeholder')
})

function startDrag(event: PointerEvent) {
  if (event.button !== 0 || props.panMode) return
  event.stopPropagation()
  dragging.value = true
  dragStart.value = { x: event.clientX, y: event.clientY }
  pendingDelta.value = { x: 0, y: 0 }
  emittedDelta.value = { x: 0, y: 0 }
  emit('select', props.node.id, event.shiftKey || event.metaKey || event.ctrlKey)
  ;(event.currentTarget as HTMLElement).setPointerCapture(event.pointerId)
}
function handlePointerDown(event: PointerEvent) {
  if (!props.panMode) startDrag(event)
}
function moveDrag(event: PointerEvent) {
  if (!dragging.value) return
  pendingDelta.value = { x: event.clientX - dragStart.value.x, y: event.clientY - dragStart.value.y }
  if (dragFrame === undefined) dragFrame = requestAnimationFrame(flushDrag)
}
function stopDrag(event: PointerEvent) {
  if (!dragging.value) return
  flushDrag()
  dragging.value = false
  ;(event.currentTarget as HTMLElement).releasePointerCapture?.(event.pointerId)
}
function flushDrag() {
  if (dragFrame !== undefined) { cancelAnimationFrame(dragFrame); dragFrame = undefined }
  if (dragging.value && (pendingDelta.value.x !== 0 || pendingDelta.value.y !== 0)) {
    const incremental = { x: pendingDelta.value.x - emittedDelta.value.x, y: pendingDelta.value.y - emittedDelta.value.y }
    if (incremental.x !== 0 || incremental.y !== 0) emit('move', props.node.id, incremental)
    emittedDelta.value = { ...pendingDelta.value }
  }
}
function startConnection(event: PointerEvent) {
  if (event.button !== 0 || props.panMode) return
  event.stopPropagation()
  emit('connect-start', props.node.id)
}
onBeforeUnmount(() => { if (dragFrame !== undefined) cancelAnimationFrame(dragFrame) })
</script>

<template>
  <article
    class="canvas-node"
    :class="{ 'canvas-node--selected': selected }"
    data-canvas-no-zoom
    :style="{ left: `${node.position.x}px`, top: `${node.position.y}px`, width: `${size.width}px`, minHeight: `${size.height}px` }"
    @pointerdown="handlePointerDown"
    @pointermove="moveDrag"
    @pointerup="stopDrag"
    @dblclick.stop="emit('select', node.id, false)"
  >
    <header class="canvas-node__header">
      <span>{{ label }}</span>
      <button type="button" :title="t('infiniteCanvas.node.delete')" data-canvas-no-zoom @pointerdown.stop @click.stop="emit('delete', node.id)">×</button>
    </header>
    <button class="canvas-node__handle canvas-node__handle--output" type="button" :title="t('infiniteCanvas.node.connect')" :aria-label="t('infiniteCanvas.node.connect')" @pointerdown="startConnection" />
    <div class="canvas-node__body">
      <PromptNode v-if="node.type === 'prompt'" :node="node" @update="emit('update', node.id, $event)" />
      <ConfigNode v-else-if="node.type === 'config'" :node="node" :models="imageModels" :models-loading="imageModelsLoading" :models-error="imageModelsError" @update="emit('update', node.id, $event)" @generate="emit('generate', node.id)" @retry-models="emit('retry-models')" />
      <ImageNode v-else-if="node.type === 'image'" :node="node" :image-url="imageUrl" @delete="emit('delete', node.id)" @retry="emit('retry', node.id)" />
      <span v-if="Object.keys(node.metadata).filter((key) => !['text', 'prompt', 'model', 'url', 'status'].includes(key)).length" class="canvas-node__placeholder">{{ metadataSummary }}</span>
    </div>
  </article>
</template>

<style scoped>
.canvas-node { position: absolute; box-sizing: border-box; border: 1px solid #cbd5e1; border-radius: 8px; background: #fff; color: #1e293b; box-shadow: 0 4px 14px rgb(15 23 42 / 12%); user-select: none; overflow: hidden; }
.canvas-node--selected { border-color: #2563eb; box-shadow: 0 0 0 2px rgb(37 99 235 / 28%), 0 4px 14px rgb(15 23 42 / 12%); }
.canvas-node__header { display: flex; justify-content: space-between; align-items: center; padding: 8px 10px; background: #f8fafc; font-size: 12px; font-weight: 600; }
.canvas-node__header button { border: 0; background: transparent; cursor: pointer; color: #64748b; font-size: 16px; line-height: 1; }
.canvas-node__handle { position: absolute; top: 50%; right: -5px; width: 10px; height: 10px; padding: 0; border: 1px solid #2563eb; border-radius: 50%; background: #fff; transform: translateY(-50%); cursor: crosshair; z-index: 1; }
.canvas-node__body { display: flex; flex-direction: column; gap: 6px; padding: 12px; font-size: 12px; }
.canvas-node__placeholder { color: #64748b; }
.canvas-node__text { color: #334155; overflow-wrap: anywhere; }
</style>
