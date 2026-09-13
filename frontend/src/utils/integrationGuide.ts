import { sanitizeUrl } from '@/utils/url'
import zhTemplate from '@/content/public-docs.zh.md?raw'
import enTemplate from '@/content/public-docs.en.md?raw'

export function buildIntegrationGuide(locale: string, baseUrl = ''): string {
  const base = (sanitizeUrl(baseUrl, { allowRelative: true }) || window.location.origin).replace(/\/+$/, '')
  const gateway = base.replace(/\/v1$/i, '')
  const endpoint = `${gateway}/v1`
  const values: Record<string, string> = {
    BASE_URL: endpoint,
    GATEWAY_URL: gateway,
    BASE_URL_JSON: JSON.stringify(endpoint),
    GATEWAY_URL_JSON: JSON.stringify(gateway),
  }
  return (locale.startsWith('zh') ? zhTemplate : enTemplate)
    .replace(/\{\{(BASE_URL|GATEWAY_URL|BASE_URL_JSON|GATEWAY_URL_JSON)\}\}/g, (_, key: string) => values[key]!)
}
