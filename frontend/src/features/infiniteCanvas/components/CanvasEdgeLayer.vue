<script setup lang="ts">
import { computed } from 'vue'
import { getCanvasNodeSize, type CanvasEdge, type CanvasNode } from '../types'

const props = withDefaults(defineProps<{ nodes: CanvasNode[]; edges: CanvasEdge[] }>(), { nodes: () => [], edges: () => [] })

const BIDIRECTIONAL_LANE_OFFSET = 24

const pathData = computed(() => {
  const logicalEdges = new Set<string>()
  return props.edges.flatMap((edge) => {
    const source = props.nodes.find((node) => node.id === edge.sourceNodeId)
    const target = props.nodes.find((node) => node.id === edge.targetNodeId)
    if (!source || !target) return []
    const logicalKey = `${edge.sourceNodeId}\u0000${edge.targetNodeId}\u0000${edge.kind}`
    if (logicalEdges.has(logicalKey)) return []
    logicalEdges.add(logicalKey)
    const sourceSize = getCanvasNodeSize(source)
    const targetSize = getCanvasNodeSize(target)
    const x1 = source.position.x + sourceSize.width
    const y1 = source.position.y + sourceSize.height / 2
    const x2 = target.position.x
    const y2 = target.position.y + targetSize.height / 2
    const bend = Math.max(48, Math.abs(x2 - x1) / 2)
    const hasReverse = props.edges.some((candidate) => candidate.id !== edge.id && candidate.sourceNodeId === edge.targetNodeId && candidate.targetNodeId === edge.sourceNodeId)
    const controlDirection = x2 === x1 ? (source.id < target.id ? 1 : -1) : Math.sign(x2 - x1)
    const lane = hasReverse ? (controlDirection > 0 ? -BIDIRECTIONAL_LANE_OFFSET : BIDIRECTIONAL_LANE_OFFSET) : 0
    return [{ ...edge, path: `M ${x1} ${y1} C ${x1 + controlDirection * bend} ${y1 + lane}, ${x2 - controlDirection * bend} ${y2 + lane}, ${x2} ${y2}` }]
  })
})
</script>

<template>
  <svg class="canvas-edge-layer" aria-hidden="true">
    <defs>
      <marker id="canvas-edge-arrow-prompt" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto" markerUnits="strokeWidth"><polygon points="0,0 8,4 0,8" fill="#2563eb" /></marker>
      <marker id="canvas-edge-arrow-config" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto" markerUnits="strokeWidth"><polygon points="0,0 8,4 0,8" fill="#7c3aed" /></marker>
      <marker id="canvas-edge-arrow-reference" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto" markerUnits="strokeWidth"><polygon points="0,0 8,4 0,8" fill="#ea580c" /></marker>
    </defs>
    <path v-for="edge in pathData" :key="edge.id" :class="`canvas-edge-layer__path canvas-edge-layer__path--${edge.kind}`" :d="edge.path" fill="none" stroke-width="2.5" :marker-end="`url(#canvas-edge-arrow-${edge.kind})`" />
  </svg>
</template>

<style scoped>
.canvas-edge-layer { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; pointer-events: none; }
.canvas-edge-layer__path { stroke-linecap: round; stroke-linejoin: round; opacity: .82; }
.canvas-edge-layer__path--prompt { stroke: #2563eb; }
.canvas-edge-layer__path--config { stroke: #7c3aed; stroke-dasharray: 7 4; }
.canvas-edge-layer__path--reference { stroke: #ea580c; stroke-dasharray: 3 4; }
</style>
