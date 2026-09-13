import { describe, expect, it } from 'vitest'
import { renderPublicDocsMarkdown } from '../publicDocsMarkdown'

describe('public documentation Markdown', () => {
  it('renders headings, code and tables with unique, linkable chapter IDs', () => {
    const result = renderPublicDocsMarkdown('## 快速开始\n\n[跳转](#快速开始)\n\n## 快速开始\n\n```js\nconst ok = true\n```\n\n| Model | Input |\n| --- | --- |\n| example | 1 |')
    const root = document.createElement('div')
    root.innerHTML = result.html
    expect(result.toc.map(item => item.text)).toEqual(['快速开始', '快速开始'])
    expect(new Set(result.toc.map(item => item.id)).size).toBe(2)
    expect(root.querySelector('a')?.hash).toBe(`#${encodeURIComponent(result.toc[0]!.id)}`)
    expect(root.querySelector('pre code')?.textContent).toContain('const ok = true')
    expect(root.querySelector('table td')?.textContent).toBe('example')
    expect(root.querySelector('h2')?.id).toBe(result.toc[0]!.id)
  })

  it('removes active HTML, unsafe links, styles and embedded frames', () => {
    const result = renderPublicDocsMarkdown('<script>alert(1)</script><iframe src="https://example.com"></iframe><style>body{display:none}</style><form><input></form><img src="x" onerror="alert(1)"><a href="javascript:alert(1)">bad</a><h2 style="position:fixed" id="__proto__">Safe</h2>')
    const root = document.createElement('div')
    root.innerHTML = result.html
    expect(root.querySelector('script, iframe, style, form, input')).toBeNull()
    expect(root.querySelector('[onerror], [style]')).toBeNull()
    expect(root.querySelector('a')?.getAttribute('href')).toBeNull()
    expect(result.toc[0]?.text).toBe('Safe')
    expect(root.querySelector('h2')?.id).toMatch(/^docs-/)
  })

  it('marks external links safe and returns no chapters for an empty document', () => {
    const result = renderPublicDocsMarkdown('[SDK](https://example.com)')
    expect(result.html).toContain('rel="noopener noreferrer"')
    expect(renderPublicDocsMarkdown('')).toEqual({ html: '', toc: [] })
  })
})
