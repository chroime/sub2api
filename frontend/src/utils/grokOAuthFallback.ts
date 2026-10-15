export const GROK_SSO_BROWSER_REQUIRED = 'GROK_SSO_BROWSER_REQUIRED'

/**
 * xAI may return the browser challenge code inside a per-item batch message.
 * Keep the check independent from Axios so the create-account flow can offer
 * the existing PKCE browser authorization path for both response shapes.
 */
export function isGrokSSOBrowserRequired(value: unknown): boolean {
  if (typeof value === 'string') {
    return value.includes(GROK_SSO_BROWSER_REQUIRED)
  }
  if (!value || typeof value !== 'object') return false
  const record = value as Record<string, unknown>
  return [record.code, record.reason, record.message].some(
    (candidate) => typeof candidate === 'string' && candidate.includes(GROK_SSO_BROWSER_REQUIRED)
  )
}
