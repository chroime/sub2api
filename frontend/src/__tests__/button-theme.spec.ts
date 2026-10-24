import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

function styleBlock(selector: string) {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return styleSource.match(new RegExp(`${escapedSelector}\\s*\\{[\\s\\S]*?\\n  \\}`))?.[0] || ''
}

describe('shared button theme styles', () => {
  it('keeps primary buttons filled when the gradient image is unavailable', () => {
    const block = styleBlock('.btn-primary')

    expect(block).toContain('background-color: #0d9488;')
    expect(block).toContain('@apply bg-gradient-to-r from-primary-500 to-primary-600;')
  })

  it('keeps disabled primary buttons visibly filled in light theme', () => {
    const block = styleBlock('.btn-primary:disabled')

    expect(block).toContain('background-color: #5eead4;')
    expect(block).toContain('background-image: linear-gradient')
  })
})
