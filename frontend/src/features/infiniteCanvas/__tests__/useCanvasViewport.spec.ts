import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { useCanvasViewport } from '../composables/useCanvasViewport'
import InfiniteCanvasSurface from '../components/InfiniteCanvasSurface.vue'

describe('useCanvasViewport', () => {
  it('keeps the world point under the cursor fixed while zooming and clamps zoom', () => {
    const viewport = useCanvasViewport({ x: 100, y: 50, zoom: 1 })
    const cursor = { x: 300, y: 200 }
    const worldBefore = viewport.screenToWorld(cursor)
    viewport.zoomAt(2, cursor)
    const worldAfter = viewport.screenToWorld(cursor)
    expect(worldAfter.x).toBeCloseTo(worldBefore.x)
    expect(worldAfter.y).toBeCloseTo(worldBefore.y)
    viewport.zoomAt(100, cursor)
    expect(viewport.viewport.value.zoom).toBe(5)
    viewport.zoomAt(0.001, cursor)
    expect(viewport.viewport.value.zoom).toBe(0.05)
  })

  it('round trips screen and world coordinates', () => {
    const viewport = useCanvasViewport({ x: -20, y: 40, zoom: 1.5 })
    const world = { x: 80, y: -10 }
    expect(viewport.screenToWorld(viewport.worldToScreen(world))).toEqual(world)
    viewport.panBy(10, -5)
    expect(viewport.viewport.value).toMatchObject({ x: -10, y: 35 })
    viewport.resetZoom()
    expect(viewport.viewport.value.zoom).toBe(1)
  })

  it('mounts the canvas surface with a fixture project', () => {
    const now = new Date()
    const wrapper = mount(InfiniteCanvasSurface, {
      props: {
        project: {
          id: 'fixture', title: 'Fixture', createdAt: now, updatedAt: now,
          viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid', nodes: [], edges: [],
        },
      },
    })
    expect(wrapper.find('.canvas-surface').exists()).toBe(true)
    wrapper.unmount()
  })
})
