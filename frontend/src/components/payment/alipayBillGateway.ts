/** Accept exact origins only; loopback HTTP is reserved for local development. */
export function parseEasyPayNotifyOrigins(raw: string): string[] | null {
  const entries = raw.split(/\r?\n/).map(value => value.trim()).filter(Boolean)
  if (!entries.length) return []
  const origins = new Set<string>()
  for (const entry of entries) {
    try {
      const url = new URL(entry)
      const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
      if (url.username || url.password || url.search || url.hash || url.pathname !== '/' ||
        (url.protocol !== 'https:' && !(url.protocol === 'http:' && loopback))) return null
      if (entry !== url.origin && entry !== `${url.origin}/`) return null
      origins.add(url.origin)
    } catch { return null }
  }
  return [...origins]
}

export function generateEasyPayKey(): string {
  return Array.from(crypto.getRandomValues(new Uint8Array(32)), byte => byte.toString(16).padStart(2, '0')).join('')
}
