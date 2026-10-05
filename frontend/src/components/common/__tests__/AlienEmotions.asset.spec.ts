import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import postcss, { type AtRule, type Rule } from 'postcss'
import { describe, expect, it } from 'vitest'

function loadAsset() {
  const source = readFileSync(resolve('public/xeno-alien-emotions.svg'), 'utf8')
  const document = new DOMParser().parseFromString(source, 'image/svg+xml')
  const stylesheet = postcss.parse(document.querySelector('style')?.textContent ?? '')
  return { document, stylesheet }
}

function declarations(rule: Rule) {
  const values: Record<string, string> = {}
  rule.walkDecls(declaration => { values[declaration.prop] = declaration.value })
  return values
}

function keyframeAt(animation: AtRule, progress: string) {
  return animation.nodes?.find((node): node is Rule =>
    node.type === 'rule' && node.selectors.includes(progress),
  )
}

describe('standalone alien emotions SVG', () => {
  it('is valid, accessible vector artwork on a transparent canvas', () => {
    const { document } = loadAsset()
    expect(document.querySelector('parsererror')).toBeNull()
    const root = document.documentElement
    expect(root.localName).toBe('svg')
    expect(root.namespaceURI).toBe('http://www.w3.org/2000/svg')
    const viewBox = root.getAttribute('viewBox')?.trim().split(/[\s,]+/).map(Number)
    expect(viewBox).toHaveLength(4)
    expect(viewBox?.every(Number.isFinite)).toBe(true)
    expect(viewBox?.[2]).toBeGreaterThan(0)
    expect(viewBox?.[3]).toBeGreaterThan(0)
    expect(document.querySelector('title')?.textContent?.trim()).toBeTruthy()
    expect(document.querySelector('desc')?.textContent?.trim()).toBeTruthy()
    expect(document.querySelectorAll('path').length).toBeGreaterThan(0)
    for (const rectangle of document.querySelectorAll('svg > rect')) {
      const coversCanvas =
        ['100%', String(viewBox?.[2])].includes(rectangle.getAttribute('width') ?? '') &&
        ['100%', String(viewBox?.[3])].includes(rectangle.getAttribute('height') ?? '')
      if (coversCanvas) expect(rectangle.getAttribute('fill')).toBe('none')
    }
  })

  it('uses only the approved local mascot raster and no external dependencies', () => {
    const { document, stylesheet } = loadAsset()
    expect(document.querySelector('script, foreignObject')).toBeNull()
    const images = Array.from(document.querySelectorAll('image'))
    expect(images).toHaveLength(1)
    const href = images[0].getAttribute('href') ?? ''
    expect(href.startsWith('data:image/png;base64,')).toBe(true)
    const png = Buffer.from(href.slice('data:image/png;base64,'.length), 'base64')
    expect(png.subarray(1, 4).toString()).toBe('PNG')
    expect(png[25]).toBe(6) // RGBA source keeps the mascot transparent around its silhouette.
    for (const element of document.querySelectorAll('*')) {
      for (const attribute of Array.from(element.attributes)) {
        if (attribute.localName === 'href') {
          expect(attribute.value.startsWith('data:image/png;base64,') || attribute.value.startsWith('#')).toBe(true)
        }
        expect(attribute.localName).not.toMatch(/^on/i)
        for (const url of attribute.value.matchAll(/url\(\s*['"]?([^)'"\s]+)['"]?\s*\)/g)) {
          expect(url[1]).toMatch(/^#/)
        }
      }
    }
    stylesheet.walkAtRules('import', () => { throw new Error('SVG must be self-contained') })
    stylesheet.walkDecls(declaration => {
      for (const url of declaration.value.matchAll(/url\(\s*['"]?([^)'"\s]+)['"]?\s*\)/g)) {
        expect(url[1]).toMatch(/^#/)
      }
    })
  })

  it('synchronizes facial and body animation on one eased twelve-second timeline', () => {
    const { document, stylesheet } = loadAsset()
    let commonTiming: Record<string, string> | undefined
    stylesheet.walkRules(rule => {
      if (rule.selectors.includes('.motion')) commonTiming = declarations(rule)
    })
    expect(commonTiming).toMatchObject({
      'animation-duration': '12s',
      'animation-iteration-count': 'infinite',
    })
    expect(commonTiming?.['animation-timing-function']).toMatch(/^cubic-bezier\(/)
    stylesheet.walkDecls('animation-name', declaration => {
      const rule = declaration.parent as Rule
      const elements = document.querySelectorAll(rule.selector)
      expect(elements.length, `Unused animation ${declaration.value}`).toBeGreaterThan(0)
      for (const element of elements) expect(element.classList.contains('motion')).toBe(true)
    })
    stylesheet.walkDecls(declaration => {
      if (declaration.prop === 'animation-duration') expect(declaration.value).toBe('12s')
      if (declaration.prop === 'animation-timing-function') expect(declaration.value).not.toMatch(/steps\(|step-/)
    })
  })

  it('closes every animation loop with identical start and end geometry', () => {
    const { stylesheet } = loadAsset()
    const animations: AtRule[] = []
    stylesheet.walkAtRules('keyframes', animation => { animations.push(animation) })
    expect(animations.length).toBeGreaterThan(0)
    for (const animation of animations) {
      const start = keyframeAt(animation, '0%')
      const end = keyframeAt(animation, '100%')
      expect(start, `Missing 0% in ${animation.params}`).toBeDefined()
      expect(end, `Missing 100% in ${animation.params}`).toBeDefined()
      expect(declarations(end!)).toEqual(declarations(start!))
    }
  })

  it('keeps a visible static mascot for reduced-motion users', () => {
    const { document, stylesheet } = loadAsset()
    let freezesAllAnimations = false
    stylesheet.walkAtRules('media', media => {
      if (!/prefers-reduced-motion\s*:\s*reduce/.test(media.params)) return
      media.walkRules(rule => {
        if (!rule.selectors.includes('*')) return
        rule.walkDecls('animation', declaration => {
          if (declaration.value === 'none') freezesAllAnimations = true
        })
      })
      media.walkDecls('display', declaration => { expect(declaration.value).not.toBe('none') })
    })
    expect(freezesAllAnimations).toBe(true)
    const animatedPaths = document.querySelectorAll('path.motion')
    expect(animatedPaths.length).toBeGreaterThan(0)
    for (const path of animatedPaths) expect(path.getAttribute('d')?.trim()).toBeTruthy()
  })
})
