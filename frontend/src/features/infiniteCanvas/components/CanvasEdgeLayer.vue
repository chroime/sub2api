<script setup lang="ts">
import { computed } from 'vue'
import type { CanvasEdge, CanvasNode } from '../types'

const props = withDefaults(defineProps<{ nodes: CanvasNode[]; edges: CanvasEdge[] }>(), { nodes: () => [], edges: () => [] })
const pathData = computed(() => props.edges.flatMap((edge) => {
  const source = props.nodes.find((node) => node.id === edge.sourceNodeId)
  const target = props.nodes.find((node) => node.id === edge.targetNodeId)
  if (!source || !target) return []
  const sourceSize = source.size ?? { width: 240, height: 140 }
  const targetSize = target.size ?? { width: 240, height: 140 }
  const x1 = source.position.x + sourceSize.width
  const y1 = source.position.y + sourceSize.height / 2
  const x2 = target.position.x
  const y2 = target.position.y + targetSize.height / 2
  const bend = Math.max(48, Math.abs(x2 - x1) / 2)
  return [{ ...edge, path: `M ${x1} ${y1} C ${x1 + bend} ${y1}, ${x2 - bend} ${y2}, ${x2} ${y2}` }]
}))
</script>

<template>
  <svg class="canvas-edge-layer" aria-hidden="true">
    <path v-for="edge in pathData" :key="edge.id" :d="edge.path" fill="none" stroke="#94a3b8" stroke-width="2" />
  </svg>
</template>

<style scoped>
.canvas-edge-layer { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; pointer-events: none; }
</style>
