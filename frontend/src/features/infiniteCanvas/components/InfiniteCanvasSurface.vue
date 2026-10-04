<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import CanvasEdgeLayer from './CanvasEdgeLayer.vue'
import CanvasMinimap from './CanvasMinimap.vue'
import CanvasNode from './CanvasNode.vue'
import { useCanvasViewport } from '../composables/useCanvasViewport'
import type { CanvasEdge, CanvasPoint, CanvasProject, CanvasViewport } from '../types'

const props = defineProps<{ project?: CanvasProject | null; activeProject?: CanvasProject | null; selectedNodeIds?: string[] }>()
const emit = defineEmits<{
  (event: 'node-select', nodeId: string, additive: boolean): void
  (event: 'node-move', nodeId: string, position: CanvasPoint): void
  (event: 'node-delete', nodeId: string): void
  (event: 'edge-create', edge: Pick<CanvasEdge, 'sourceNodeId' | 'targetNodeId' | 'kind'>): void
  (event: 'viewport-update', viewport: CanvasViewport): void
  (event: 'empty-canvas-double-click', point: CanvasPoint): void
}>()
const surface = ref<HTMLElement>()
const currentProject = computed(() => props.project ?? props.activeProject ?? null)
const { viewport, panBy, zoomAt, screenToWorld } = useCanvasViewport(currentProject.value?.viewport)
let panPointer: number | undefined
let lastPointer = { x: 0, y: 0 }
let frame: number | undefined
let pending = { x: 0, y: 0 }
watch(() => currentProject.value?.viewport, (value) => { if (value) viewport.value = { ...value } }, { deep: true })
watch(viewport, (value) => emit('viewport-update', { ...value }), { deep: true })
function pointerDown(event: PointerEvent) {
  if (event.button !== 0 && event.button !== 1 && !event.ctrlKey && !event.shiftKey) return
  if ((event.target as HTMLElement).closest('[data-canvas-no-pan]')) return
  panPointer = event.pointerId
  lastPointer = { x: event.clientX, y: event.clientY }
  surface.value?.setPointerCapture(event.pointerId)
}
function pointerMove(event: PointerEvent) {
  if (panPointer !== event.pointerId) return
  pending = { x: event.clientX - lastPointer.x, y: event.clientY - lastPointer.y }
  lastPointer = { x: event.clientX, y: event.clientY }
  if (frame === undefined) frame = requestAnimationFrame(() => { panBy(pending.x, pending.y); frame = undefined })
}
function pointerUp(event: PointerEvent) {
  if (panPointer !== event.pointerId) return
  panPointer = undefined
  surface.value?.releasePointerCapture(event.pointerId)
}
function wheel(event: WheelEvent) {
  if ((event.target as HTMLElement).closest('[data-canvas-no-zoom]')) return
  event.preventDefault()
  const rect = surface.value?.getBoundingClientRect()
  if (!rect) return
  const point = { x: event.clientX - rect.left, y: event.clientY - rect.top }
  zoomAt(viewport.value.zoom * (event.deltaY > 0 ? 0.9 : 1.1), point)
}
function emptyDoubleClick(event: MouseEvent) {
  if ((event.target as HTMLElement).closest('[data-canvas-no-zoom]')) return
  const rect = surface.value?.getBoundingClientRect()
  if (rect) emit('empty-canvas-double-click', screenToWorld({ x: event.clientX - rect.left, y: event.clientY - rect.top }))
}
function keyboard(event: KeyboardEvent) { if (event.key === ' ') surface.value?.classList.toggle('canvas-surface--pan-mode', true) }
function keyup(event: KeyboardEvent) { if (event.key === ' ') surface.value?.classList.toggle('canvas-surface--pan-mode', false) }
onMounted(() => { window.addEventListener('keydown', keyboard); window.addEventListener('keyup', keyup) })
onBeforeUnmount(() => { window.removeEventListener('keydown', keyboard); window.removeEventListener('keyup', keyup); if (frame !== undefined) cancelAnimationFrame(frame) })
function updateNode(nodeId: string, position: CanvasPoint) { emit('node-move', nodeId, { x: position.x / viewport.value.zoom, y: position.y / viewport.value.zoom }) }
</script>

<template>
  <section ref="surface" class="canvas-surface" :class="`canvas-surface--${currentProject?.backgroundMode ?? 'grid'}`" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerUp" @wheel="wheel" @dblclick="emptyDoubleClick">
    <div v-if="currentProject" class="canvas-surface__world" :style="{ transform: `translate(${viewport.x}px, ${viewport.y}px) scale(${viewport.zoom})` }">
      <CanvasEdgeLayer :nodes="currentProject.nodes" :edges="currentProject.edges" />
      <CanvasNode v-for="node in currentProject.nodes" :key="node.id" :node="node" :selected="selectedNodeIds?.includes(node.id)" @select="(id, additive) => emit('node-select', id, additive)" @move="updateNode" @delete="(id) => emit('node-delete', id)" />
    </div>
    <CanvasMinimap v-if="currentProject" class="canvas-surface__minimap" :nodes="currentProject.nodes" :viewport="viewport" @navigate="(point) => panBy(-point.x * viewport.zoom, -point.y * viewport.zoom)" />
  </section>
</template>

<style scoped>
.canvas-surface { position: relative; width: 100%; height: 100%; min-height: 420px; overflow: hidden; background-color: #f8fafc; touch-action: none; }
.canvas-surface--grid { background-image: linear-gradient(#e2e8f0 1px, transparent 1px), linear-gradient(90deg, #e2e8f0 1px, transparent 1px); background-size: 24px 24px; }
.canvas-surface--dots { background-image: radial-gradient(#cbd5e1 1px, transparent 1px); background-size: 20px 20px; }
.canvas-surface__world { position: absolute; inset: 0; transform-origin: 0 0; }
.canvas-surface__minimap { position: absolute; right: 16px; bottom: 16px; z-index: 2; }
</style>
