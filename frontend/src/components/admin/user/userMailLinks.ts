import type { UserBulkEmailLinks } from '@/api/admin/users'

/** Defaults are scoped to the administrator's current site and remain editable. */
export function currentSiteUserMailLinks(href: string): UserBulkEmailLinks {
  const empty = { platform_url: '', docs_url: '', model_plaza_url: '', canvas_url: '' }
  try {
    const current = new URL(href)
    if (!['http:', 'https:'].includes(current.protocol)) return empty
    const hostname = current.hostname
    const loopback = hostname === 'localhost' || /^127\./.test(hostname) || hostname === '[::1]'
    const ipAddress = /^(?:\d{1,3}\.){3}\d{1,3}$/.test(hostname) || hostname.includes(':')
    return {
      platform_url: current.origin,
      docs_url: `${current.origin}/docs`,
      model_plaza_url: `${current.origin}/model-plaza`,
      canvas_url: loopback ? 'https://canvas.localhost' : ipAddress ? '' : `https://canvas.${hostname}`
    }
  } catch {
    return empty
  }
}
