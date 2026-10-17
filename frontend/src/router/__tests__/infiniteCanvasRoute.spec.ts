import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const routerPath = resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts')
const routerSource = readFileSync(routerPath, 'utf8')

describe('Built-in Infinite Canvas removal', () => {
  it('does not register the removed built-in canvas route or embed view', () => {
    expect(routerSource).not.toContain("path: '/infinite-canvas'")
    expect(routerSource).not.toContain("name: 'InfiniteCanvas'")
    expect(routerSource).not.toContain("CanvasEmbedView.vue")
  })
})
