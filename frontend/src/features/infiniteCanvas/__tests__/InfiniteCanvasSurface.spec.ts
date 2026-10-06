import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import InfiniteCanvasSurface from '../components/InfiniteCanvasSurface.vue'
import type { CanvasProject } from '../types'

function project(overrides: Partial<CanvasProject> = {}): CanvasProject {
  const now = new Date('2026-01-01T00:00:00.000Z')
  return {
    id: 'fixture', title: 'Fixture', createdAt: now, updatedAt: now,
    viewport: { x: 0, y: 0, zoom: 2 }, backgroundMode: 'grid',
    nodes: [
      { id: 'source', type: 'prompt', position: { x: 100, y: 20 }, metadata: {} },
      { id: 'target', type: 'config', position: { x: 400, y: 20 }, metadata: {} },
    ], edges: [], ...overrides,
  }
}

describe('InfiniteCanvasSurface interactions', () => {
  const rafCallbacks: FrameRequestCallback[] = []
  beforeEach(() => {
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { rafCallbacks.push(callback); return rafCallbacks.length })
    vi.stubGlobal('cancelAnimationFrame', () => undefined)
    Object.defineProperty(HTMLElement.prototype, 'setPointerCapture', { configurable: true, value: () => undefined })
    Object.defineProperty(HTMLElement.prototype, 'releasePointerCapture', { configurable: true, value: () => undefined })
  })
  afterEach(() => {
    rafCallbacks.splice(0)
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('converts zoomed node drag screen movement to one world-coordinate delta', async () => {
    const wrapper = mount(InfiniteCanvasSurface, { props: { project: project() } })
    const node = wrapper.find('.canvas-node')
    await node.trigger('pointerdown', { button: 0, pointerId: 1, clientX: 100, clientY: 50 })
    await node.trigger('pointermove', { pointerId: 1, clientX: 110, clientY: 50 })
    expect(rafCallbacks).toHaveLength(1)
    rafCallbacks.shift()!(0)
    await nextTick()
    await nextTick()
    expect(wrapper.emitted('node-move')?.at(-1)).toEqual(['source', { x: 105, y: 20 }])
    await node.trigger('pointerup', { pointerId: 1, clientX: 110, clientY: 50 })
  })

  it('emits incremental node drag movement across multiple RAF frames', async () => {
    const wrapper = mount(InfiniteCanvasSurface, { props: { project: project() } })
    const node = wrapper.find('.canvas-node')
    await node.trigger('pointerdown', { button: 0, pointerId: 6, clientX: 100, clientY: 50 })
    await node.trigger('pointermove', { pointerId: 6, clientX: 110, clientY: 50 })
    rafCallbacks.shift()!(0)
    await node.trigger('pointermove', { pointerId: 6, clientX: 120, clientY: 50 })
    rafCallbacks.shift()!(0)
    expect(wrapper.emitted('node-move')).toEqual([
      ['source', { x: 105, y: 20 }],
      ['source', { x: 110, y: 20 }],
    ])
    await node.trigger('pointerup', { pointerId: 6, clientX: 120, clientY: 50 })
  })

  it('accumulates multiple blank-pan moves before a single RAF flush', async () => {
    const wrapper = mount(InfiniteCanvasSurface, { props: { project: project({ viewport: { x: 0, y: 0, zoom: 1 } }) } })
    const surface = wrapper.find('.canvas-surface')
    await surface.trigger('pointerdown', { button: 0, pointerId: 2, clientX: 0, clientY: 0 })
    await surface.trigger('pointermove', { pointerId: 2, clientX: 5, clientY: 3 })
    await surface.trigger('pointermove', { pointerId: 2, clientX: 12, clientY: 8 })
    expect(rafCallbacks).toHaveLength(1)
    rafCallbacks.shift()!(0)
    expect(wrapper.emitted('viewport-update')?.at(-1)?.[0]).toMatchObject({ x: 12, y: 8 })
  })

  it('suppresses node drag while space is held and creates an edge through an output handle', async () => {
    const wrapper = mount(InfiniteCanvasSurface, { props: { project: project({ viewport: { x: 0, y: 0, zoom: 1 } }) } })
    const nodes = wrapper.findAll('.canvas-node')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true }))
    await nodes[0].trigger('pointerdown', { button: 0, pointerId: 3, clientX: 100, clientY: 50 })
    await nodes[0].trigger('pointermove', { pointerId: 3, clientX: 110, clientY: 50 })
    expect(wrapper.emitted('node-move')).toBeUndefined()
    window.dispatchEvent(new KeyboardEvent('keyup', { key: ' ', bubbles: true }))
    await nextTick()
    await nodes[0].find('.canvas-node__handle--output').trigger('pointerdown', { button: 0, pointerId: 4 })
    await nodes[1].trigger('pointerdown', { button: 0, pointerId: 5, clientX: 400, clientY: 50 })
    expect(wrapper.emitted('edge-create')?.at(-1)).toEqual([{ sourceNodeId: 'source', targetNodeId: 'target', kind: 'reference' }])
  })

  it('clears a pending edge source when the active project changes', async () => {
    const wrapper = mount(InfiniteCanvasSurface, { props: { project: project() } })
    await wrapper.findAll('.canvas-node')[0].find('.canvas-node__handle--output').trigger('pointerdown', { button: 0, pointerId: 7 })
    await wrapper.setProps({ project: project({ id: 'next-project' }) })
    await wrapper.findAll('.canvas-node')[1].trigger('pointerdown', { button: 0, pointerId: 8, clientX: 400, clientY: 50 })
    expect(wrapper.emitted('edge-create')).toBeUndefined()
  })
})
