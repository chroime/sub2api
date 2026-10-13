import { describe, expect, it } from 'vitest'
import {
  buildCanvasUrl,
  isCanvasAuthClearMessage,
  isCanvasReadyMessage,
  normalizeCanvasOrigin,
} from '@/features/canvasHost/protocol'

describe('canvas host protocol', () => {
  it('accepts only version-one ready messages', () => {
    expect(isCanvasReadyMessage({ type: 'sub2api.canvas.ready', version: 1 })).toBe(true)
    expect(isCanvasReadyMessage({ type: 'sub2api.canvas.ready', version: 2 })).toBe(false)
    expect(isCanvasReadyMessage({ type: 'sub2api.auth.init', version: 1 })).toBe(false)
  })

  it('accepts version-one clear messages only', () => {
    expect(isCanvasAuthClearMessage({ type: 'sub2api.auth.clear', version: 1, reason: 'logout' })).toBe(true)
    expect(isCanvasAuthClearMessage({ type: 'sub2api.auth.clear', version: 2 })).toBe(false)
  })

  it('normalizes an explicit origin and rejects paths or unsupported schemes', () => {
    expect(normalizeCanvasOrigin('https://canvas.example/')).toBe('https://canvas.example')
    expect(() => normalizeCanvasOrigin('https://canvas.example/path')).toThrow()
    expect(() => normalizeCanvasOrigin('javascript:alert(1)')).toThrow()
  })

  it('never places credentials in the canvas URL', () => {
    expect(buildCanvasUrl('https://canvas.example')).toBe('https://canvas.example/canvas')
  })
})
