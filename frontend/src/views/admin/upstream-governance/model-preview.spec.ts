import { afterEach, describe, expect, it } from 'vitest'
import { modelHTMLArtifacts, modelPreviewCSP, modelPreviewDocument } from './model-preview'

let pageScript: HTMLScriptElement | undefined
afterEach(() => { pageScript?.remove(); pageScript = undefined })

describe('preview document CSP compatibility', () => {
  it('passes HTML as inert data to a nonced bootstrap that runs only inside the frame', () => {
    pageScript = document.createElement('script')
    pageScript.nonce = 'page-nonce-fixture'
    document.head.append(pageScript)
    const original = '<!doctype html><html lang="zh"><head><style>body{margin:0}</style></head><body><svg id="pelican"></svg><script nonce="from-model">requestAnimationFrame(draw)</script><script src="https://example.test/external.js"></script></body></html>'
    const preview = new DOMParser().parseFromString(modelPreviewDocument(original), 'text/html')
    expect(preview.querySelector('script:not([src])')?.getAttribute('nonce')).toBe('page-nonce-fixture')
    expect(preview.querySelectorAll('script')).toHaveLength(1)
    expect(preview.querySelector('script[src]')).toBeNull()
    expect(preview.querySelector('#pelican')).toBeNull()
    expect(preview.querySelector('script')?.textContent).toContain('\\u003c')
    expect(preview.querySelector('script')?.textContent).toContain('requestAnimationFrame(draw)')
    expect(preview.querySelector('meta[http-equiv]')?.getAttribute('content')).toBe(modelPreviewCSP)
    expect(document.querySelector('#pelican')).toBeNull()
    expect(modelHTMLArtifacts(original)).toEqual([{ source: 'original', html: original }])
    expect(original).toContain('nonce="from-model"')
    expect(original).not.toContain('page-nonce-fixture')
  })

  it('prevents closing script tags and raw HTML from escaping the bootstrap data', () => {
    pageScript = document.createElement('script')
    pageScript.nonce = 'fixture-nonce'
    document.head.append(pageScript)
    const original = '<script>const s = "</script><img src=https://example.test/tracker>"</script><svg onload="alert(1)"></svg>'
    const preview = new DOMParser().parseFromString(modelPreviewDocument(original), 'text/html')
    expect(preview.querySelectorAll('script')).toHaveLength(1)
    expect(preview.querySelector('img, svg')).toBeNull()
    expect(preview.querySelector('script')?.textContent).not.toContain('</script>')
  })

  it('keeps CSP before original content and omits unsupported directives when no nonce exists', () => {
    const original = '<svg><text>原文</text></svg>'
    const preview = modelPreviewDocument(original)
    expect(preview).toContain(original)
    expect(preview.indexOf('Content-Security-Policy')).toBeLessThan(preview.indexOf('<svg>'))
    expect(preview).not.toContain('navigate-to')
    expect(modelPreviewCSP).toContain("connect-src 'none'")
    expect(modelPreviewCSP).toContain("frame-src 'none'")
    expect(modelPreviewCSP).toContain("form-action 'none'")
  })
})
