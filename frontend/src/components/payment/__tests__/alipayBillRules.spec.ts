import { describe, expect, it } from 'vitest'
import QRCode from 'qrcode'
import { decodeAlipayQRPixels, isAlipayQRUrl, parseAlipayBillAmount, parseAlipayBillRules, serializeAlipayBillRules, validateAlipayBillRules } from '../alipayBillRules'
import type { AlipayBillQRRule } from '@/types/payment'

const rule: AlipayBillQRRule = { id: 'ten', amount: '10.00', qr_url: 'https://qr.alipay.com/fixed10', enabled: true, sort_order: 0 }

describe('fixed Alipay amount and QR rules', () => {
  it.each([['0.01', 1], ['10', 1000], ['12.3', 1230], ['19.99', 1999], ['1000000.00', 100000000]])('preserves exact cents for %s', (input, cents) => {
    expect(parseAlipayBillAmount(input)).toBe(cents)
  })

  it.each(['0', '-1', '1e2', '0.001', '12.345', '01.00', '1,000.00', 'Infinity', '1000000.01', '10000000000000000'])('rejects an amount that would need rounding or reinterpretation: %s', input => {
    expect(parseAlipayBillAmount(input)).toBeNull()
  })

  it.each(['https://example.com/code', 'http://qr.alipay.com/code', 'https://qr.alipay.com.evil.test/code', 'https://qr.alipay.com@evil.test/code', 'https://user@qr.alipay.com/code', 'https://qr.alipay.com/', 'https://qr.alipay.com:444/code', 'https://qr.alipay.com:443/code', 'https://qr.alipay.com////', 'https://qr.alipay.com/code#', 'https://qr.alipay.com/code#fragment', 'alipays://pay'])('rejects unsupported QR payload %s', url => {
    expect(isAlipayQRUrl(url)).toBe(false)
  })

  it('deduplicates enabled rules by integer cents, allowing a disabled replacement', () => {
    const replacement = { ...rule, id: 'replacement', amount: '10', sort_order: 1 }
    expect(validateAlipayBillRules([rule, replacement])).toBe('billRuleDuplicateAmount')
    expect(validateAlipayBillRules([rule, { ...replacement, enabled: false }])).toBeNull()
  })

  it('saves canonical amounts without rounding and retains explicit disabled state', () => {
    expect(JSON.parse(serializeAlipayBillRules([{ ...rule, amount: '12.3', enabled: false }]))).toEqual([{ ...rule, amount: '12.30', enabled: false }])
    expect(() => serializeAlipayBillRules([{ ...rule, amount: '12.345' }])).toThrow('billRuleAmountInvalid')
  })

  it('preserves malformed stored configuration as an error instead of silently removing it', () => {
    expect(parseAlipayBillRules('{bad json')).toBeNull()
    expect(parseAlipayBillRules(JSON.stringify([{ ...rule, amount: 10 }]))).toBeNull()
    expect(parseAlipayBillRules(JSON.stringify([rule]))).toEqual([rule])
  })

  it('decodes actual QR pixels without BarcodeDetector and returns null for a blank image', () => {
    const qr = QRCode.create(rule.qr_url, { errorCorrectionLevel: 'M' })
    const scale = 5
    const margin = 4
    const width = (qr.modules.size + 2 * margin) * scale
    const pixels = new Uint8ClampedArray(width * width * 4).fill(255)
    for (let y = 0; y < qr.modules.size; y++) {
      for (let x = 0; x < qr.modules.size; x++) {
        if (!qr.modules.get(y, x)) continue
        for (let dy = 0; dy < scale; dy++) for (let dx = 0; dx < scale; dx++) {
          const offset = (((y + margin) * scale + dy) * width + (x + margin) * scale + dx) * 4
          pixels[offset] = pixels[offset + 1] = pixels[offset + 2] = 0
        }
      }
    }
    expect(decodeAlipayQRPixels(pixels, width, width)).toBe(rule.qr_url)
    expect(decodeAlipayQRPixels(new Uint8ClampedArray(100 * 100 * 4).fill(255), 100, 100)).toBeNull()
  })
})
