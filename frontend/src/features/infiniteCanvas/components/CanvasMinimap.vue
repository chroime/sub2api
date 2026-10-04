<script setup lang="ts">
import { computed } from 'vue'
import type { CanvasNode, CanvasViewport } from '../types'

const props = withDefaults(defineProps<{ nodes: CanvasNode[]; viewport: CanvasViewport; width?: number; height?: number }>(), { nodes: () => [], width: 200, height: 120 })
const emit = defineEmits<{ (event: 'navigate', point: { x: number; y: number }): void }>()
const bounds = computed(() => {
  if (!props.nodes.length) return { x: -200, y: -120, width: 400, height: 240 }
  const xs = props.nodes.map((node) => node.position.x)
  const ys = props.nodes.map((node) => node.position.y)
  const xe = props.nodes.map((node) => node.position.x + (node.size?.width ?? 240))
  const ye = props.nodes.map((node) => node.position.y + (node.size?.height ?? 140))
  const x = Math.min(...xs) - 80
  const y = Math.min(...ys) - 80
  return { x, y, width: Math.max(400, Math.max(...xe) - x + 80), height: Math.max(240, Math.max(...ye) - y + 80) }
})
const scale = computed(() => Math.min(props.width / bounds.value.width, props.height / bounds.value.height))
const nodeStyle = (node: CanvasNode) => ({ left: `${(node.position.x - bounds.value.x) * scale.value}px`, top: `${(node.position.y - bounds.value.y) * scale.value}px`, width: `${Math.max(3, (node.size?.width ?? 240) * scale.value)}px`, height: `${Math.max(3, (node.size?.height ?? 140) * scale.value)}px` })
const viewportStyle = computed(() => ({ left: `${(0 - props.viewport.x / props.viewport.zoom - bounds.value.x) * scale.value}px`, top: `${(0 - props.viewport.y / props.viewport.zoom - bounds.value.y) * scale.value}px`, width: `${props.width / props.viewport.zoom * scale.value}px`, height: `${props.height / props.viewport.zoom * scale.value}px` }))
function navigate(event: MouseEvent) {
  const element = event.currentTarget as HTMLElement
  const rect = element.getBoundingClientRect()
  emit('navigate', { x: (event.clientX - rect.left) / scale.value + bounds.value.x, y: (event.clientY - rect.top) / scale.value + bounds.value.y })
}
</script>

<template>
  <div class="canvas-minimap" :style="{ width: `${width}px`, height: `${height}px` }" @click="navigate">
    <span v-for="node in nodes" :key="node.id" class="canvas-minimap__node" :style="nodeStyle(node)" />
    <span class="canvas-minimap__viewport" :style="viewportStyle" />
  </div>
</template>

<style scoped>
.canvas-minimap { position: relative; overflow: hidden; border: 1px solid #cbd5e1; border-radius: 6px; background: #f8fafc; cursor: crosshair; }
.canvas-minimap__node { position: absolute; border-radius: 2px; background: #64748b; }
.canvas-minimap__viewport { position: absolute; border: 1px solid #2563eb; background: rgb(37 99 235 / 12%); pointer-events: none; }
</style>
