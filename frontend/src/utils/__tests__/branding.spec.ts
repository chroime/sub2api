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

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
  })

  it('uses the default static favicon for unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/xeno-alien-spin-still.svg')
  })

  it.each(['', '/logo.svg', '/xeno-alien-emotions.svg', '/xeno-alien-spin.svg', '/xeno-alien-spin.svg?rev=20260914', '/xeno-alien-spin.svg?rev=20260914-round-head-2'])(
    'uses the static approved mascot for default branding %s',
    (logo) => {
      updateFavicon(logo)

      const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
      expect(link?.getAttribute('href')).toBe('/xeno-alien-spin-still.svg')
      expect(link?.type).toBe('image/svg+xml')
    },
  )

  it('resets a configured favicon when the logo setting is cleared', () => {
    updateFavicon('/custom-logo.png')
    updateFavicon('')

    expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toBe('/xeno-alien-spin-still.svg')
  })

  it.each([
    ['data:image/svg+xml;base64,PHN2Zy8+', 'image/svg+xml'],
    ['/custom-logo.svg?v=2#mark', 'image/svg+xml'],
    ['https://example.com/custom-logo.png', 'image/png'],
  ])('preserves a custom logo and its MIME type: %s', (logo, mimeType) => {
    updateFavicon(logo)

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe(logo)
    expect(link?.type).toBe(mimeType)
  })

  it('creates a default favicon if the document has none', () => {
    document.head.innerHTML = ''
    updateFavicon('')

    expect(document.querySelector('link[rel="icon"]')?.getAttribute('href')).toBe('/xeno-alien-spin-still.svg')
  })
})
