// @vitest-environment node
import postcss from 'postcss'
import tailwindcss from 'tailwindcss'
import { readFileSync } from 'node:fs'
import { beforeAll, describe, expect, it } from 'vitest'
import config from '../../tailwind.config.js'
import gradientBackgrounds from '../../postcss/gradient-backgrounds.js'

let root
const fallback = 'var(--app-gradient-to-color, var(--app-gradient-from-color, transparent))'

function declarations(selector) {
  const properties = {}
  root.walkRules(rule => {
    if (!rule.selectors.includes(selector) && rule.selector !== selector) return
    rule.walkDecls(declaration => { properties[declaration.prop] = declaration.value })
  })
  return properties
}

beforeAll(async () => {
  const result = await postcss([tailwindcss({
    ...config,
    content: [{ raw: `<div class="bg-gradient-to-br bg-gradient-to-r bg-gradient-to-b
      from-primary-500 to-primary-600 from-blue-500 to-indigo-600
      from-[#635bff] to-[#4f46e5] from-amber-500 to-orange-600
      from-primary-500/20 dark:from-primary-900/20 dark:to-primary-800/20
      from-40% via-50% to-60% bg-white dark:bg-dark-900 bg-clip-text text-transparent
      text-gradient progress-bar"></div>` }],
  }), gradientBackgrounds()]).process(readFileSync(new URL('../style.css', import.meta.url), 'utf8'), { from: undefined })
  root = result.root
})

describe('shared gradient backgrounds', () => {
  it.each(['.bg-gradient-to-br', '.bg-gradient-to-r', '.bg-gradient-to-b'])(
    'keeps a solid background underneath %s when its gradient cannot be drawn', selector => {
      expect(declarations(selector)['background-color']).toBe(fallback)
    },
  )

  it.each([
    ['.from-primary-500', '--app-gradient-from-color', '#14b8a6'],
    ['.to-primary-600', '--app-gradient-to-color', '#0d9488'],
    ['.from-blue-500', '--app-gradient-from-color', '#3b82f6'],
    ['.to-indigo-600', '--app-gradient-to-color', '#4f46e5'],
    ['.from-amber-500', '--app-gradient-from-color', '#f59e0b'],
    ['.to-orange-600', '--app-gradient-to-color', '#ea580c'],
    ['.from-\\[\\#635bff\\]', '--app-gradient-from-color', '#635bff'],
  ])('derives the fallback from the actual %s palette color', (selector, property, color) => {
    expect(declarations(selector)[property]).toBe(color)
  })

  it('preserves translucent decorations without painting an extra layer of tint', () => {
    expect(declarations('.from-primary-500\\/20')['--app-gradient-from-color']).toBe('initial')
    const darkRules = []
    root.walkDecls('--app-gradient-to-color', declaration => {
      if (declaration.parent.selector.includes('dark\\:to-primary-800\\/20')) darkRules.push(declaration.value)
    })
    expect(darkRules).toEqual(['initial'])
  })

  it('retains the original gradient stops for arbitrary payment provider colors', () => {
    const from = declarations('.from-\\[\\#635bff\\]')
    expect(from['--tw-gradient-from']).toContain('#635bff')
    expect(from['--tw-gradient-stops']).toContain('var(--tw-gradient-from)')
    expect(declarations('.to-\\[\\#4f46e5\\]')['--tw-gradient-to']).toContain('#4f46e5')
  })

  it('keeps gradient text and progress components filled after their images are removed', () => {
    expect(declarations('.text-gradient')['background-color']).toBe('rgb(13 148 136 / var(--tw-bg-opacity, 1))')
    expect(declarations('.progress-bar')['background-color']).toBe('rgb(20 184 166 / var(--tw-bg-opacity, 1))')
  })

  it('supplies explicit stop defaults while retaining custom token-latency stop positions', () => {
    const defaults = declarations('*, ::before, ::after')
    expect(defaults['--tw-gradient-from-position']).toBe('0%')
    expect(defaults['--tw-gradient-via-position']).toBe('50%')
    expect(defaults['--tw-gradient-to-position']).toBe('100%')
    expect(declarations('.from-40\\%')['--tw-gradient-from-position']).toBe('40%')
    expect(declarations('.via-50\\%')['--tw-gradient-via-position']).toBe('50%')
    expect(declarations('.to-60\\%')['--tw-gradient-to-position']).toBe('60%')
  })
})
