import jsQR from 'jsqr'
import type { AlipayBillQRRule } from '@/types/payment'

/** Work in integer cents so no rounding can silently change a QR's fixed amount. */
export function parseAlipayBillAmount(value: string): number | null {
  const match = /^(0|[1-9]\d{0,9})(?:\.(\d{1,2}))?$/.exec(value.trim())
  if (!match) return null
  const cents = Number(match[1]) * 100 + Number((match[2] || '').padEnd(2, '0'))
  return Number.isSafeInteger(cents) && cents > 0 && cents <= 100_000_000 ? cents : null
}

export function isAlipayQRUrl(value: string): boolean {
  const raw = value.trim()
  if (!raw.startsWith('https://qr.alipay.com/') || raw.length > 2048 || raw.includes('#') || /\s/.test(raw)) return false
  try {
    const url = new URL(raw)
    return url.protocol === 'https:' && url.hostname === 'qr.alipay.com' &&
      !url.port && !url.username && !url.password && url.pathname.replace(/\//g, '').length > 0
  } catch {
    return false
  }
}

export type AlipayBillRuleError = 'billRulesRequired' | 'billRuleAmountInvalid' | 'billRuleUrlInvalid' | 'billRuleDuplicateAmount' | 'billRulesInvalid'

export function validateAlipayBillRules(rules: AlipayBillQRRule[]): AlipayBillRuleError | null {
  if (!rules.length) return 'billRulesRequired'
  const activeAmounts = new Set<number>()
  const ids = new Set<string>()
  for (const rule of rules) {
    if (!/^[A-Za-z0-9_-]{1,64}$/.test(rule.id) || ids.has(rule.id) || !Number.isSafeInteger(rule.sort_order) || rule.sort_order < 0) return 'billRulesInvalid'
    ids.add(rule.id)
    const cents = parseAlipayBillAmount(rule.amount)
    if (cents === null) return 'billRuleAmountInvalid'
    if (!isAlipayQRUrl(rule.qr_url)) return 'billRuleUrlInvalid'
    if (rule.enabled) {
      if (activeAmounts.has(cents)) return 'billRuleDuplicateAmount'
      activeAmounts.add(cents)
    }
  }
  return null
}

/** Malformed stored rules remain visibly invalid rather than being silently overwritten. */
export function parseAlipayBillRules(raw: string | undefined): AlipayBillQRRule[] | null {
  if (!raw?.trim()) return []
  try {
    const rules: unknown = JSON.parse(raw)
    if (!Array.isArray(rules)) return null
    if (rules.some(rule => !rule || typeof rule.id !== 'string' || typeof rule.amount !== 'string' ||
      typeof rule.qr_url !== 'string' || typeof rule.enabled !== 'boolean' || typeof rule.sort_order !== 'number')) return null
    return rules
  } catch {
    return null
  }
}

export function serializeAlipayBillRules(rules: AlipayBillQRRule[]): string {
  const error = validateAlipayBillRules(rules)
  if (error) throw new Error(error)
  return JSON.stringify(rules.map(rule => {
    const cents = parseAlipayBillAmount(rule.amount)!
    return { ...rule, amount: `${Math.floor(cents / 100)}.${String(cents % 100).padStart(2, '0')}`, qr_url: rule.qr_url.trim() }
  }))
}

export function decodeAlipayQRPixels(data: Uint8ClampedArray, width: number, height: number): string | null {
  const decoded = jsQR(data, width, height, { inversionAttempts: 'attemptBoth' })
  return decoded?.data || null
}

/** Decode in the browser; the image never leaves the administrator's device. */
export async function decodeAlipayQRImage(file: File): Promise<string | null> {
  if (!file.type.startsWith('image/') || file.size > 10 * 1024 * 1024) throw new Error('billQRImageInvalid')
  const objectUrl = URL.createObjectURL(file)
  try {
    const image = new Image()
    image.src = objectUrl
    await image.decode()
    if (!image.naturalWidth || !image.naturalHeight) return null
    const scale = Math.min(1, 2400 / Math.max(image.naturalWidth, image.naturalHeight))
    const canvas = document.createElement('canvas')
    canvas.width = Math.max(1, Math.round(image.naturalWidth * scale))
    canvas.height = Math.max(1, Math.round(image.naturalHeight * scale))
    const context = canvas.getContext('2d', { willReadFrequently: true })
    if (!context) return null
    context.drawImage(image, 0, 0, canvas.width, canvas.height)
    return decodeAlipayQRPixels(context.getImageData(0, 0, canvas.width, canvas.height).data, canvas.width, canvas.height)
  } finally {
    URL.revokeObjectURL(objectUrl)
  }
}
