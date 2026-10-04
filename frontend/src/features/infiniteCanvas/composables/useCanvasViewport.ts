import { ref, type Ref } from 'vue'
import type { CanvasPoint, CanvasViewport } from '../types'

export const MIN_CANVAS_ZOOM = 0.05
export const MAX_CANVAS_ZOOM = 5

function clampZoom(value: number): number {
  return Math.min(MAX_CANVAS_ZOOM, Math.max(MIN_CANVAS_ZOOM, value))
}

export function useCanvasViewport(initial: CanvasViewport = { x: 0, y: 0, zoom: 1 }): {
  viewport: Ref<CanvasViewport>
  panBy: (x: number, y: number) => void
  zoomAt: (zoom: number, screenPoint: CanvasPoint) => void
  resetZoom: () => void
  screenToWorld: (point: CanvasPoint) => CanvasPoint
  worldToScreen: (point: CanvasPoint) => CanvasPoint
} {
  const viewport = ref<CanvasViewport>({ ...initial, zoom: clampZoom(initial.zoom) })

  const panBy = (x: number, y: number) => {
    viewport.value = { ...viewport.value, x: viewport.value.x + x, y: viewport.value.y + y }
  }

  const zoomAt = (zoom: number, screenPoint: CanvasPoint) => {
    const previousZoom = viewport.value.zoom
    const nextZoom = clampZoom(zoom)
    const worldPoint = {
      x: (screenPoint.x - viewport.value.x) / previousZoom,
      y: (screenPoint.y - viewport.value.y) / previousZoom,
    }
    viewport.value = {
      x: screenPoint.x - worldPoint.x * nextZoom,
      y: screenPoint.y - worldPoint.y * nextZoom,
      zoom: nextZoom,
    }
  }

  const resetZoom = () => {
    viewport.value = { ...viewport.value, zoom: 1 }
  }

  const screenToWorld = (point: CanvasPoint): CanvasPoint => ({
    x: (point.x - viewport.value.x) / viewport.value.zoom,
    y: (point.y - viewport.value.y) / viewport.value.zoom,
  })

  const worldToScreen = (point: CanvasPoint): CanvasPoint => ({
    x: point.x * viewport.value.zoom + viewport.value.x,
    y: point.y * viewport.value.zoom + viewport.value.y,
  })

  return { viewport, panBy, zoomAt, resetZoom, screenToWorld, worldToScreen }
}

