import { beforeEach, describe, expect, it } from 'vitest'
import { resolveSiteLogo, updateFavicon } from '@/utils/branding'

describe('resolveSiteLogo', () => {
  it.each([
    '',
    '/logo.svg',
    '/xeno-alien-emotions.svg',
    '/xeno-alien-spin.svg',
    '/xeno-alien-spin.svg?rev=20260914',
    '/xeno-alien-spin.svg?rev=20260913#brand',
    '/xeno-alien-spin.svg?rev=20260914-round-head-2',
  ])('refreshes built-in logo URLs to the current round-head asset: %s', (src) => {
    expect(resolveSiteLogo(src)).toBe('/xeno-alien-spin.svg?rev=20260914-round-head-2')
  })

  it.each([
    '/custom-logo.svg?rev=old#mark',
    '/custom/xeno-alien-spin.svg?rev=old',
    'https://example.com/xeno-alien-spin.svg?rev=old',
    'data:image/svg+xml;base64,PHN2Zy8+',
  ])('preserves custom and remote logo URLs: %s', (src) => {
    expect(resolveSiteLogo(src)).toBe(src)
  })
})

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/logo.svg">'
  })

  it('replaces the default favicon with the configured favicon', () => {
    updateFavicon('https://example.com/custom-favicon.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-favicon.png')
  })

  it.each(['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', '//example.com/favicon.png', '/\\example.com/favicon.png', '/\n/example.com/favicon.png'])(
    'uses the default static favicon for unsafe favicon URLs: %s',
    (favicon) => {
      updateFavicon(favicon)

      const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
      expect(link?.getAttribute('href')).toBe('/logo.svg')
      expect(link?.type).toBe('image/svg+xml')
    },
  )

  it.each(['', '/logo.svg'])(
    'uses the built-in static favicon for the default setting: %s',
    (favicon) => {
      updateFavicon(favicon)

      const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
      expect(link?.getAttribute('href')).toBe('/logo.svg')
      expect(link?.type).toBe('image/svg+xml')
    },
  )

  it('resets a configured favicon when the favicon setting is cleared', () => {
    updateFavicon('/custom-favicon.png')
    updateFavicon('')

    expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toBe('/logo.svg')
  })

  it.each([
    ['data:image/svg+xml;base64,PHN2Zy8+', 'image/svg+xml'],
    ['data:image/png;base64,aWNvbg==', 'image/png'],
    ['data:image/webp;base64,aWNvbg==', 'image/webp'],
    ['/custom-favicon.svg?v=2#mark', 'image/svg+xml'],
    ['https://example.com/custom-favicon.png', 'image/png'],
    ['/custom-favicon.jpg', 'image/jpeg'],
    ['/custom-favicon.jpeg', 'image/jpeg'],
    ['/custom-favicon.gif', 'image/gif'],
    ['/custom-favicon.webp', 'image/webp'],
    ['/custom-favicon.avif', 'image/avif'],
    ['/custom-favicon.ico', 'image/x-icon'],
    ['/xeno-alien-spin.svg?rev=custom', 'image/svg+xml'],
  ])('preserves a custom favicon and its MIME type: %s', (favicon, mimeType) => {
    updateFavicon(favicon)

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe(favicon)
    expect(link?.type).toBe(mimeType)
  })

  it('removes a stale MIME type when a new favicon URL has no extension', () => {
    updateFavicon('/custom-favicon.png')
    updateFavicon('https://example.com/favicon?id=2')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/favicon?id=2')
    expect(link?.hasAttribute('type')).toBe(false)
  })

  it('creates a default favicon if the document has none', () => {
    document.head.innerHTML = ''
    updateFavicon('')

    expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toBe('/logo.svg')
  })
})
