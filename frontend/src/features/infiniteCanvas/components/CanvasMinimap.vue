<script setup lang="ts">
import { computed } from 'vue'
import { getCanvasNodeSize, type CanvasNode, type CanvasViewport } from '../types'

const props = withDefaults(defineProps<{ nodes: CanvasNode[]; viewport: CanvasViewport; width?: number; height?: number; hostWidth?: number; hostHeight?: number }>(), { nodes: () => [], width: 200, height: 120, hostWidth: 800, hostHeight: 600 })
const emit = defineEmits<{ (event: 'navigate', point: { x: number; y: number }): void }>()
const bounds = computed(() => {
  const xs = props.nodes.map((node) => node.position.x)
  const ys = props.nodes.map((node) => node.position.y)
  const xe = props.nodes.map((node) => node.position.x + getCanvasNodeSize(node).width)
  const ye = props.nodes.map((node) => node.position.y + getCanvasNodeSize(node).height)
  // Keep the current viewport inside the minimap even after the user pans
  // well beyond the node cluster, so the blue viewport indicator never
  // disappears off the minimap bounds.
  const visibleLeft = -props.viewport.x / props.viewport.zoom
  const visibleTop = -props.viewport.y / props.viewport.zoom
  const visibleRight = (props.hostWidth - props.viewport.x) / props.viewport.zoom
  const visibleBottom = (props.hostHeight - props.viewport.y) / props.viewport.zoom
  const minX = Math.min(-200, ...xs, visibleLeft) - 80
  const minY = Math.min(-120, ...ys, visibleTop) - 80
  const maxX = Math.max(200, ...xe, visibleRight) + 80
  const maxY = Math.max(120, ...ye, visibleBottom) + 80
  return { x: minX, y: minY, width: Math.max(400, maxX - minX), height: Math.max(240, maxY - minY) }
})
const scale = computed(() => Math.min(props.width / bounds.value.width, props.height / bounds.value.height))
const nodeStyle = (node: CanvasNode) => ({ left: `${(node.position.x - bounds.value.x) * scale.value}px`, top: `${(node.position.y - bounds.value.y) * scale.value}px`, width: `${Math.max(3, getCanvasNodeSize(node).width * scale.value)}px`, height: `${Math.max(3, getCanvasNodeSize(node).height * scale.value)}px` })
function getMinimapViewportRect(viewport: CanvasViewport, bounds: { x: number; y: number }, scaleValue: number, hostWidth: number, hostHeight: number) {
  return {
    left: (0 - viewport.x / viewport.zoom - bounds.x) * scaleValue,
    top: (0 - viewport.y / viewport.zoom - bounds.y) * scaleValue,
    width: hostWidth / viewport.zoom * scaleValue,
    height: hostHeight / viewport.zoom * scaleValue,
  }
}
const viewportStyle = computed(() => {
  const rect = getMinimapViewportRect(props.viewport, bounds.value, scale.value, props.hostWidth, props.hostHeight)
  return { left: `${rect.left}px`, top: `${rect.top}px`, width: `${rect.width}px`, height: `${rect.height}px` }
})
function navigate(event: MouseEvent) {
  const element = event.currentTarget as HTMLElement
  const rect = element.getBoundingClientRect()
  emit('navigate', { x: (event.clientX - rect.left) / scale.value + bounds.value.x, y: (event.clientY - rect.top) / scale.value + bounds.value.y })
}
</script>

<template>
  <div class="canvas-minimap" data-canvas-no-pan data-canvas-no-zoom :style="{ width: `${width}px`, height: `${height}px` }" @pointerdown.stop @pointermove.stop @pointerup.stop @wheel.stop @dblclick.stop @click.stop="navigate">
    <span v-for="node in nodes" :key="node.id" class="canvas-minimap__node" :style="nodeStyle(node)" />
    <span class="canvas-minimap__viewport" :style="viewportStyle" />
  </div>
</template>

<style scoped>
.canvas-minimap { position: relative; overflow: hidden; border: 1px solid #cbd5e1; border-radius: 6px; background: #f8fafc; cursor: crosshair; }
.canvas-minimap__node { position: absolute; border-radius: 2px; background: #64748b; }
.canvas-minimap__viewport { position: absolute; border: 1px solid #2563eb; background: rgb(37 99 235 / 12%); pointer-events: none; }
</style>
