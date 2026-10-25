import { describe, expect, it } from 'vitest'
import {
  DEFAULT_PROXY_NAMING_RULE,
  generateProxyNames,
  proxyNamingRuleError,
  type ProxyNamingRule
} from '../proxyNaming'

const rule = (overrides: Partial<ProxyNamingRule> = {}): ProxyNamingRule => ({
  country: 'us', start: 1, digits: 3, separator: '', ...overrides
})

describe('proxyNamingRuleError', () => {
  it('accepts the initial naming rule', () => {
    expect(proxyNamingRuleError(DEFAULT_PROXY_NAMING_RULE)).toBe(false)
    expect(generateProxyNames([{}, {}], DEFAULT_PROXY_NAMING_RULE)).toEqual(['us001', 'us002'])
  })

  it.each(['us', ' JP ', 'proxy', 'PROXY'])(
    'accepts a country prefix or the generic fallback: %s', (country) => {
      expect(proxyNamingRuleError(rule({ country }))).toBe(false)
    }
  )

  it.each(['', 'u', 'usa', '美国', 'u1', 'us-', 'pRo xy'])(
    'rejects malformed country prefixes: %s', (country) => {
      expect(proxyNamingRuleError(rule({ country }))).toBe(true)
    }
  )

  it.each([0, -1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, 1_000_000_000, '', ' ', '1.5', '1e2', '+1'])(
    'rejects unusable starting numbers: %s', (start) => {
      expect(proxyNamingRuleError(rule({ start }))).toBe(true)
    }
  )

  it.each([0, -1, 1.5, Number.NaN, Number.POSITIVE_INFINITY, 10, '', ' ', '2.0', '1e0'])(
    'rejects unusable sequence widths: %s', (digits) => {
      expect(proxyNamingRuleError(rule({ digits }))).toBe(true)
    }
  )

  it.each([' ', '/', '.', '--', ':'])(
    'rejects unsupported separators: %s', (separator) => {
      expect(proxyNamingRuleError(rule({ separator }))).toBe(true)
    }
  )

  it('accepts numeric strings from form fields', () => {
    expect(proxyNamingRuleError(rule({ start: ' 09 ', digits: '4' }))).toBe(false)
    expect(generateProxyNames([{}, {}], rule({ start: ' 09 ', digits: '4' }))).toEqual(['us0009', 'us0010'])
  })
})

describe('generateProxyNames', () => {
  it('allocates sequential country names in input order', () => {
    expect(generateProxyNames([{}, {}, {}], rule())).toEqual(['us001', 'us002', 'us003'])
  })

  it('uses a configured start, width and separator', () => {
    expect(generateProxyNames([{}, {}], rule({ country: 'JP', start: 12, digits: 4, separator: '-' })))
      .toEqual(['jp-0012', 'jp-0013'])
    expect(generateProxyNames([{}], rule({ country: 'proxy', digits: 1, separator: '_' })))
      .toEqual(['proxy_1'])
  })

  it('allows a number to exceed the minimum display width without truncating it', () => {
    expect(generateProxyNames([{}, {}, {}], rule({ start: 99, digits: 2 })))
      .toEqual(['us99', 'us100', 'us101'])
  })

  it('retains a shared sequence across individual country choices', () => {
    expect(generateProxyNames([{ country: 'us' }, { country: 'JP' }, { country: '' }, { country: 'de' }], rule()))
      .toEqual(['us001', 'jp002', 'us003', 'de004'])
  })

  it('trims manual names without overwriting them or consuming their own number', () => {
    expect(generateProxyNames([
      {},
      { name: '  自定义节点  ', manualName: true },
      { country: 'jp' },
      { name: '   ', manualName: true },
      { name: 'old default' }
    ], rule())).toEqual(['us001', '自定义节点', 'jp002', '', 'us003'])
  })

  it('reserves names of later manual rows before assigning automatic names', () => {
    expect(generateProxyNames([{}, { name: ' US001 ', manualName: true }, {}], rule()))
      .toEqual(['us002', 'US001', 'us003'])
  })

  it('skips reserved names case insensitively and keeps the shared sequence', () => {
    expect(generateProxyNames([{}, { country: 'jp' }, {}], rule(), [' US001 ', 'us002', 'JP004']))
      .toEqual(['us003', 'jp005', 'us006'])
  })

  it('supports a set of existing names without mutating it', () => {
    const existing = new Set(['us001'])
    expect(generateProxyNames([{}, {}], rule(), existing)).toEqual(['us002', 'us003'])
    expect([...existing]).toEqual(['us001'])
  })

  it('leaves conflicting manual names visible for the caller to validate', () => {
    expect(generateProxyNames([
      { name: 'us001', manualName: true },
      { name: 'US001', manualName: true },
      {}
    ], rule(), ['US001'])).toEqual(['us001', 'US001', 'us002'])
  })

  it('does not generate names with an invalid rule while retaining manual names', () => {
    expect(generateProxyNames([{}, { name: ' custom ', manualName: true }, {}], rule({ start: '' })))
      .toEqual(['', 'custom', ''])
  })

  it('leaves invalid per-row prefixes blank for correction', () => {
    expect(generateProxyNames([{ country: 'invalid' }, {}, { country: ' jp ' }], rule()))
      .toEqual(['', 'us001', 'jp002'])
  })

  it('returns no names for an empty import', () => {
    expect(generateProxyNames([], rule())).toEqual([])
  })
})
