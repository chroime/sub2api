<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import CanvasEdgeLayer from './CanvasEdgeLayer.vue'
import CanvasMinimap from './CanvasMinimap.vue'
import CanvasNode from './CanvasNode.vue'
import { useCanvasViewport } from '../composables/useCanvasViewport'
import { getCanvasNodeSize, type CanvasEdge, type CanvasNode as CanvasNodeModel, type CanvasPoint, type CanvasProject, type CanvasViewport } from '../types'
import type { ImageModel } from '@/api/imageGeneration'

const props = defineProps<{ project?: CanvasProject | null; activeProject?: CanvasProject | null; selectedNodeIds?: string[]; imageUrls?: Record<string, string>; imageModels?: ImageModel[]; imageModelsLoading?: boolean; imageModelsError?: string }>()
const emit = defineEmits<{
  (event: 'node-select', nodeId: string, additive: boolean): void
  (event: 'node-move', nodeId: string, position: CanvasPoint): void
  (event: 'node-delete', nodeId: string): void
  (event: 'node-update', nodeId: string, patch: Partial<CanvasNodeModel>): void
  (event: 'node-retry', nodeId: string): void
  (event: 'node-generate', nodeId: string): void
  (event: 'edge-create', edge: Pick<CanvasEdge, 'sourceNodeId' | 'targetNodeId' | 'kind'>): void
  (event: 'viewport-update', viewport: CanvasViewport): void
  (event: 'empty-canvas-double-click', point: CanvasPoint): void
  (event: 'retry-image-models'): void
}>()
const surface = ref<HTMLElement>()
const { t } = useI18n()
const currentProject = computed(() => props.project ?? props.activeProject ?? null)
const { viewport, panBy, zoomAt, screenToWorld } = useCanvasViewport(currentProject.value?.viewport)
let panPointer: number | undefined
let lastPointer = { x: 0, y: 0 }
let frame: number | undefined
let pending = { x: 0, y: 0 }
const panMode = ref(false)
const connectingNodeId = ref<string | undefined>()
const surfaceSize = ref({ width: 800, height: 600 })
const dragPositions = new Map<string, CanvasPoint>()
const connectionLabel = computed(() => {
  if (!connectingNodeId.value) return ''
  return t('infiniteCanvas.node.connectionHint')
})
watch(() => currentProject.value?.viewport, (value) => { if (value) viewport.value = { ...value } }, { deep: true })
watch(() => currentProject.value?.id, () => { connectingNodeId.value = undefined; dragPositions.clear() })
watch(viewport, (value) => emit('viewport-update', { ...value }), { deep: true })
function pointerDown(event: PointerEvent) {
  if (event.button !== 0 && event.button !== 1 && !event.ctrlKey && !event.shiftKey) return
  if ((event.target as HTMLElement).closest('[data-canvas-no-pan]')) return
  if (connectingNodeId.value) connectingNodeId.value = undefined
  panPointer = event.pointerId
  lastPointer = { x: event.clientX, y: event.clientY }
  surface.value?.setPointerCapture(event.pointerId)
}
function pointerMove(event: PointerEvent) {
  if (panPointer !== event.pointerId) return
  pending = { x: pending.x + event.clientX - lastPointer.x, y: pending.y + event.clientY - lastPointer.y }
  lastPointer = { x: event.clientX, y: event.clientY }
  if (frame === undefined) frame = requestAnimationFrame(() => { panBy(pending.x, pending.y); emit('viewport-update', { ...viewport.value }); pending = { x: 0, y: 0 }; frame = undefined })
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
  connectingNodeId.value = undefined
  const rect = surface.value?.getBoundingClientRect()
  if (rect) emit('empty-canvas-double-click', screenToWorld({ x: event.clientX - rect.left, y: event.clientY - rect.top }))
}
function keyboard(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    connectingNodeId.value = undefined
    return
  }
  if (event.key === ' ' || event.key === 'Spacebar' || event.key === 'Control' || event.ctrlKey) {
    panMode.value = true
    if (event.key === ' ') event.preventDefault()
  }
}
function keyup(event: KeyboardEvent) {
  if (event.key === ' ' || event.key === 'Spacebar' || event.key === 'Control' || !event.ctrlKey) panMode.value = event.ctrlKey
}
function resizeSurface() {
  const element = surface.value
  if (element) surfaceSize.value = { width: element.clientWidth || 800, height: element.clientHeight || 600 }
}
onMounted(() => { window.addEventListener('keydown', keyboard); window.addEventListener('keyup', keyup); resizeSurface(); window.addEventListener('resize', resizeSurface) })
onBeforeUnmount(() => { window.removeEventListener('keydown', keyboard); window.removeEventListener('keyup', keyup); window.removeEventListener('resize', resizeSurface); if (frame !== undefined) cancelAnimationFrame(frame) })
function updateNode(nodeId: string, screenDelta: CanvasPoint) {
  const node = currentProject.value?.nodes.find((item) => item.id === nodeId)
  if (node) {
    const previous = dragPositions.get(nodeId) ?? node.position
    const next = { x: previous.x + screenDelta.x / viewport.value.zoom, y: previous.y + screenDelta.y / viewport.value.zoom }
    dragPositions.set(nodeId, next)
    emit('node-move', nodeId, next)
  }
}
function handleNodeSelect(nodeId: string, additive: boolean) {
  const node = currentProject.value?.nodes.find((item) => item.id === nodeId)
  if (node) dragPositions.set(nodeId, { ...node.position })
  if (connectingNodeId.value && connectingNodeId.value !== nodeId) {
    emit('edge-create', { sourceNodeId: connectingNodeId.value, targetNodeId: nodeId, kind: 'reference' })
    connectingNodeId.value = undefined
    return
  }
  emit('node-select', nodeId, additive)
}
function inferEdgeKind(sourceNode: CanvasNodeModel, targetNode: CanvasNodeModel): CanvasEdge['kind'] {
  if (sourceNode.type === 'image' && targetNode.type === 'prompt') return 'reference'
  if (sourceNode.type === 'config' && (targetNode.type === 'image' || targetNode.type === 'prompt')) return 'config'
  if (sourceNode.type === 'prompt' && (targetNode.type === 'config' || targetNode.type === 'image')) return 'prompt'
  return 'reference'
}
function handleNodeConnectTarget(nodeId: string) {
  const sourceId = connectingNodeId.value
  if (!sourceId || sourceId === nodeId) {
    connectingNodeId.value = undefined
    return
  }
  const source = currentProject.value?.nodes.find((node) => node.id === sourceId)
  const target = currentProject.value?.nodes.find((node) => node.id === nodeId)
  if (source && target) emit('edge-create', { sourceNodeId: source.id, targetNodeId: target.id, kind: inferEdgeKind(source, target) })
  connectingNodeId.value = undefined
}
function startConnection(nodeId: string) {
  connectingNodeId.value = connectingNodeId.value === nodeId ? undefined : nodeId
}
function centerViewport(point: CanvasPoint) {
  viewport.value = { x: surfaceSize.value.width / 2 - point.x * viewport.value.zoom, y: surfaceSize.value.height / 2 - point.y * viewport.value.zoom, zoom: viewport.value.zoom }
}
function fitView() {
  const nodes = currentProject.value?.nodes ?? []
  if (!nodes.length) {
    viewport.value = { x: surfaceSize.value.width / 2, y: surfaceSize.value.height / 2, zoom: 1 }
    return
  }
  const padding = 72
  const minX = Math.min(...nodes.map((node) => node.position.x))
  const minY = Math.min(...nodes.map((node) => node.position.y))
  const maxX = Math.max(...nodes.map((node) => node.position.x + getCanvasNodeSize(node).width))
  const maxY = Math.max(...nodes.map((node) => node.position.y + getCanvasNodeSize(node).height))
  const contentWidth = Math.max(1, maxX - minX)
  const contentHeight = Math.max(1, maxY - minY)
  const availableWidth = Math.max(240, surfaceSize.value.width - padding * 2)
  const availableHeight = Math.max(240, surfaceSize.value.height - padding * 2)
  const zoom = Math.min(5, Math.max(0.05, Math.min(availableWidth / contentWidth, availableHeight / contentHeight)))
  viewport.value = {
    x: surfaceSize.value.width / 2 - (minX + contentWidth / 2) * zoom,
    y: surfaceSize.value.height / 2 - (minY + contentHeight / 2) * zoom,
    zoom,
  }
}
defineExpose({ fitView })
</script>

<template>
  <section ref="surface" class="canvas-surface" :class="`canvas-surface--${currentProject?.backgroundMode ?? 'grid'}`" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerUp" @wheel="wheel" @dblclick="emptyDoubleClick">
    <div v-if="connectionLabel" class="canvas-surface__connection-hint" role="status">{{ connectionLabel }}</div>
    <div v-if="currentProject" class="canvas-surface__world" :style="{ transform: `translate(${viewport.x}px, ${viewport.y}px) scale(${viewport.zoom})` }">
      <CanvasEdgeLayer :nodes="currentProject.nodes" :edges="currentProject.edges" />
      <CanvasNode v-for="node in currentProject.nodes" :key="node.id" :node="node" :image-url="imageUrls?.[node.id]" :selected="selectedNodeIds?.includes(node.id)" :pan-mode="panMode" :image-models="imageModels" :image-models-loading="imageModelsLoading" :image-models-error="imageModelsError" @select="handleNodeSelect" @connect-start="startConnection" @connect-target="handleNodeConnectTarget" @move="updateNode" @delete="(id) => emit('node-delete', id)" @update="(id, patch) => emit('node-update', id, patch)" @retry="(id) => emit('node-retry', id)" @generate="(id) => emit('node-generate', id)" @retry-models="emit('retry-image-models')" />
    </div>
    <CanvasMinimap v-if="currentProject" class="canvas-surface__minimap" :nodes="currentProject.nodes" :viewport="viewport" :host-width="surfaceSize.width" :host-height="surfaceSize.height" @navigate="centerViewport" />
  </section>
</template>

<style scoped>
.canvas-surface { position: relative; width: 100%; height: 100%; min-height: 420px; overflow: hidden; background-color: #f8fafc; touch-action: none; }
.canvas-surface--grid { background-image: linear-gradient(#e2e8f0 1px, transparent 1px), linear-gradient(90deg, #e2e8f0 1px, transparent 1px); background-size: 24px 24px; }
.canvas-surface--dots { background-image: radial-gradient(#cbd5e1 1px, transparent 1px); background-size: 20px 20px; }
.canvas-surface__world { position: absolute; inset: 0; transform-origin: 0 0; }
.canvas-surface__minimap { position: absolute; right: 16px; bottom: 16px; z-index: 2; }
.canvas-surface__connection-hint { position: absolute; top: 14px; left: 50%; z-index: 4; transform: translateX(-50%); border: 1px solid #93c5fd; border-radius: 999px; padding: 6px 12px; color: #1e40af; background: rgb(239 246 255 / 95%); box-shadow: 0 4px 12px rgb(15 23 42 / 12%); font-size: 12px; pointer-events: none; }
</style>
