import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const routerPath = resolve(dirname(fileURLToPath(import.meta.url)), '../index.ts')
const routerSource = readFileSync(routerPath, 'utf8')

describe('Infinite Canvas route', () => {
  it('registers the authenticated non-admin lazy route', () => {
    expect(routerSource).toContain("path: '/infinite-canvas'")
    expect(routerSource).toContain("name: 'InfiniteCanvas'")
    expect(routerSource).toContain("import('@/views/user/InfiniteCanvasView.vue')")
    expect(routerSource).toMatch(/path: '\/infinite-canvas',[\s\S]*?requiresAuth: true,[\s\S]*?requiresAdmin: false/)
  })
})
