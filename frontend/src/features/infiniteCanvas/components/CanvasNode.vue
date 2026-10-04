<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import type { CanvasNode as CanvasNodeModel } from '../types'

const props = defineProps<{ node: CanvasNodeModel; selected?: boolean; panMode?: boolean }>()
const emit = defineEmits<{
  (event: 'select', nodeId: string, additive: boolean): void
  (event: 'move', nodeId: string, screenDelta: { x: number; y: number }): void
  (event: 'delete', nodeId: string): void
  (event: 'connect-start', nodeId: string): void
}>()
const dragging = ref(false)
const dragStart = ref({ x: 0, y: 0 })
const pendingDelta = ref({ x: 0, y: 0 })
let dragFrame: number | undefined
const size = computed(() => props.node.size ?? { width: 240, height: 140 })
const label = computed(() => props.node.type[0].toUpperCase() + props.node.type.slice(1))
const metadataSummary = computed(() => {
  const keys = Object.keys(props.node.metadata).filter((key) => !['text', 'model', 'url', 'status'].includes(key))
  return keys.length ? `${keys.length} metadata field${keys.length === 1 ? '' : 's'}` : 'Placeholder node'
})

function startDrag(event: PointerEvent) {
  if (event.button !== 0 || props.panMode) return
  event.stopPropagation()
  dragging.value = true
  dragStart.value = { x: event.clientX, y: event.clientY }
  pendingDelta.value = { x: 0, y: 0 }
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
    emit('move', props.node.id, { ...pendingDelta.value })
    pendingDelta.value = { x: 0, y: 0 }
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
      <button type="button" title="Delete node" @pointerdown.stop @click.stop="emit('delete', node.id)">×</button>
    </header>
    <button class="canvas-node__handle canvas-node__handle--output" type="button" title="Connect node" aria-label="Connect node" @pointerdown="startConnection" />
    <div class="canvas-node__body">
      <span class="canvas-node__placeholder">{{ metadataSummary }}</span>
      <span v-if="node.type === 'prompt' && typeof node.metadata.text === 'string'" class="canvas-node__text">{{ node.metadata.text }}</span>
      <span v-else-if="node.type === 'config' && typeof node.metadata.model === 'string'" class="canvas-node__text">{{ node.metadata.model }}</span>
      <span v-else-if="node.type === 'image' && typeof node.metadata.status === 'string'" class="canvas-node__text">{{ node.metadata.status }}</span>
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
