import api, { type Site } from '@/api/admin/upstream-governance'

type IconSite = Pick<Site, 'id' | 'version' | 'base_url' | 'platform' | 'proxy_id'>
const icons = new Map<string, { expires: number; value: Promise<string> }>()

export function loadSiteIcon(site: IconSite): Promise<string> {
  const key = JSON.stringify([site.id, site.version, site.base_url, site.platform, site.proxy_id])
  const cached = icons.get(key)
  if (cached && cached.expires > Date.now()) return cached.value
  const entry: { expires: number; value: Promise<string> } = {
    expires: Date.now() + 3600000,
    value: Promise.resolve().then(() => api.siteIcon(site.id)).then(result => {
      if (!/^data:image\/(?:png|jpeg|gif|webp|vnd\.microsoft\.icon|x-icon|svg\+xml);base64,[A-Za-z0-9+/=]+$/.test(result.data_url) || result.data_url.length > 720000) return ''
      return result.data_url
    }).catch(() => {
      entry.expires = Date.now() + 30000
      return ''
    }),
  }
  if (icons.size >= 256) icons.clear()
  icons.set(key, entry)
  return entry.value
}
