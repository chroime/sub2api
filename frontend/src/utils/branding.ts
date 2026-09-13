import { sanitizeUrl } from '@/utils/url'

const BUILT_IN_SPIN_PATH = '/xeno-alien-spin.svg'
export const DEFAULT_SITE_LOGO = `${BUILT_IN_SPIN_PATH}?rev=20260914-round-head-2`
export const STATIC_SITE_LOGO = '/xeno-alien-spin-still.svg'

export function resolveSiteLogo(logoUrl = ''): string {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  const isBuiltInSpinLogo = sanitizedLogoUrl.startsWith(BUILT_IN_SPIN_PATH) &&
    new URL(sanitizedLogoUrl, 'https://branding.invalid').pathname === BUILT_IN_SPIN_PATH
  if (!sanitizedLogoUrl || sanitizedLogoUrl === '/logo.svg' || sanitizedLogoUrl === '/xeno-alien-emotions.svg' || isBuiltInSpinLogo) {
    return DEFAULT_SITE_LOGO
  }
  return sanitizedLogoUrl
}

export function updateFavicon(logoUrl: string): void {
  const siteLogo = resolveSiteLogo(logoUrl)
  const faviconUrl = siteLogo === DEFAULT_SITE_LOGO ? STATIC_SITE_LOGO : siteLogo

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  const dataMimeType = /^data:(image\/[^;,]+)[;,]/i.exec(faviconUrl)?.[1]
  const extension = new URL(faviconUrl, window.location.origin).pathname.split('.').pop()?.toLowerCase() || ''
  const imageMimeTypes: Record<string, string> = {
    svg: 'image/svg+xml',
    png: 'image/png',
    jpg: 'image/jpeg',
    jpeg: 'image/jpeg',
    gif: 'image/gif',
    webp: 'image/webp',
    avif: 'image/avif',
    ico: 'image/x-icon',
  }
  const mimeType = dataMimeType || imageMimeTypes[extension]
  if (mimeType) link.type = mimeType
  else link.removeAttribute('type')
  link.href = faviconUrl
}
