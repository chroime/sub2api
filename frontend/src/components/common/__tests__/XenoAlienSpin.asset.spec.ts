import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

function loadAsset() {
  const source = readFileSync(resolve('public/xeno-alien-spin.svg'), 'utf8')
  return new DOMParser().parseFromString(source, 'image/svg+xml')
}

function pathSize(path: string) {
  const numbers = [...path.matchAll(/[-+]?(?:\d*\.\d+|\d+)(?:e[-+]?\d+)?/gi)].map((match) => Number(match[0]))
  const xCoordinates = numbers.filter((_, index) => index % 2 === 0)
  const yCoordinates = numbers.filter((_, index) => index % 2 === 1)
  return {
    width: Math.max(...xCoordinates) - Math.min(...xCoordinates),
    height: Math.max(...yCoordinates) - Math.min(...yCoordinates),
  }
}

describe('xeno alien spin SVG', () => {
  it('keeps the animated head from collapsing during the turn', () => {
    const document = loadAsset()
    const head = document.querySelector('.animated-mascot [data-part="head-shape"]')
    const animation = head?.querySelector('animate[attributeName="d"]')
    const clippedHead = document.querySelector('#spin-head-clip [data-part="head"]')
    expect(head).not.toBeNull()
    expect(clippedHead).not.toBeNull()
    expect(clippedHead?.getAttribute('d')).toBe(head?.getAttribute('d'))
    const clipAnimation = clippedHead?.querySelector('animate[attributeName="d"]')
    expect(clipAnimation?.getAttribute('values')).toBe(animation?.getAttribute('values'))
    expect(clipAnimation?.getAttribute('keyTimes')).toBe(animation?.getAttribute('keyTimes'))

    const front = pathSize(head!.getAttribute('d')!)
    const values = (animation?.getAttribute('values') ?? head!.getAttribute('d')!).split(';')
    for (const value of values) {
      const size = pathSize(value)
      expect(size.width).toBeGreaterThanOrEqual(front.width * .94)
      expect(size.width).toBeLessThanOrEqual(front.width * 1.01)
      expect(Math.abs(size.height - front.height)).toBeLessThan(.2)
    }
  })

  it('turns the eyes and highlights with the face instead of fading a fixed front face', () => {
    const document = loadAsset()
    for (const part of ['eye-left-shape', 'eye-right-shape', 'glazeLeft', 'glazeRight', 'highlightLeft', 'highlightRight', 'glintLeft', 'glintRight']) {
      const animation = document.querySelector(`.animated-mascot [data-part="${part}"] animate[attributeName="d"]`)
      expect(animation, `${part} must follow the head turn`).not.toBeNull()
      const values = animation!.getAttribute('values')!.split(';')
      expect(new Set(values).size).toBeGreaterThan(2)
      expect(animation!.getAttribute('dur')).toBe('12s')
      expect(values[0]).toBe(values.at(-1))
    }
  })
})
