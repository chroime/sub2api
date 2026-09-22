import { describe, expect, it } from 'vitest'
import { serializeInlineJson } from '../inlineJson'

describe('inline public settings JSON', () => {
  it('keeps Markdown script terminators inside data, without altering the document', () => {
    const settings = { docs_content: '</script><script>alert(1)</script>\n## Docs', docs_title: '<API>' }
    const serialized = serializeInlineJson(settings)
    expect(serialized).not.toContain('<')
    expect(JSON.parse(serialized)).toEqual(settings)
    const root = document.createElement('div')
    root.innerHTML = `<script>window.__APP_CONFIG__=${serialized};</script>`
    expect(root.querySelectorAll('script')).toHaveLength(1)
  })
})
