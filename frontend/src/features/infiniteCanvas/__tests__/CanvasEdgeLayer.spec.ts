import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import CanvasEdgeLayer from '../components/CanvasEdgeLayer.vue'
import type { CanvasEdge, CanvasNode } from '../types'

const nodes: CanvasNode[] = [
  { id: 'prompt', type: 'prompt', position: { x: 0, y: 100 }, metadata: {} },
  { id: 'config', type: 'config', position: { x: 500, y: 100 }, metadata: {} },
]

function renderEdges(edges: CanvasEdge[]) {
  return mount(CanvasEdgeLayer, { props: { nodes, edges } }).findAll('path')
}

describe('CanvasEdgeLayer routing', () => {
  it('routes a reverse edge with control points that follow the backwards direction', () => {
    const [path] = renderEdges([
      { id: 'reverse', sourceNodeId: 'config', targetNodeId: 'prompt', kind: 'config' },
    ])

    expect(path.attributes('d')).toBe('M 760 265 C 380 265, 380 230, 0 230')
  })

  it('uses separate vertical lanes for bidirectional edges', () => {
    const paths = renderEdges([
      { id: 'forward', sourceNodeId: 'prompt', targetNodeId: 'config', kind: 'prompt' },
      { id: 'reverse', sourceNodeId: 'config', targetNodeId: 'prompt', kind: 'config' },
    ])

    expect(paths[0].attributes('d')).toBe('M 300 230 C 400 206, 400 241, 500 265')
    expect(paths[1].attributes('d')).toBe('M 760 265 C 380 289, 380 254, 0 230')
  })

  it('renders one path for duplicate legacy edges with the same logical connection', () => {
    const paths = renderEdges([
      { id: 'legacy-a', sourceNodeId: 'prompt', targetNodeId: 'config', kind: 'prompt' },
      { id: 'legacy-b', sourceNodeId: 'prompt', targetNodeId: 'config', kind: 'prompt' },
    ])

    expect(paths).toHaveLength(1)
  })
})
