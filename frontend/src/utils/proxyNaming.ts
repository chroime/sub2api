export interface ProxyNamingRule {
  country: string
  start: number | string
  digits: number | string
  separator: string
}

export interface ProxyNamingRow {
  country?: string
  name?: string
  manualName?: boolean
}

export const DEFAULT_PROXY_NAMING_RULE: ProxyNamingRule = {
  country: 'us', start: 1, digits: 3, separator: ''
}

// ISO 3166-1 country/region codes. The view resolves localized names with Intl.DisplayNames.
export const PROXY_NAMING_COUNTRIES = [
  'ad', 'ae', 'af', 'ag', 'ai', 'al', 'am', 'ao', 'aq', 'ar', 'as', 'at', 'au', 'aw', 'ax', 'az',
  'ba', 'bb', 'bd', 'be', 'bf', 'bg', 'bh', 'bi', 'bj', 'bl', 'bm', 'bn', 'bo', 'bq', 'br', 'bs',
  'bt', 'bv', 'bw', 'by', 'bz', 'ca', 'cc', 'cd', 'cf', 'cg', 'ch', 'ci', 'ck', 'cl', 'cm', 'cn',
  'co', 'cr', 'cu', 'cv', 'cw', 'cx', 'cy', 'cz', 'de', 'dj', 'dk', 'dm', 'do', 'dz', 'ec', 'ee',
  'eg', 'eh', 'er', 'es', 'et', 'fi', 'fj', 'fk', 'fm', 'fo', 'fr', 'ga', 'gb', 'gd', 'ge', 'gf',
  'gg', 'gh', 'gi', 'gl', 'gm', 'gn', 'gp', 'gq', 'gr', 'gs', 'gt', 'gu', 'gw', 'gy', 'hk', 'hm',
  'hn', 'hr', 'ht', 'hu', 'id', 'ie', 'il', 'im', 'in', 'io', 'iq', 'ir', 'is', 'it', 'je', 'jm',
  'jo', 'jp', 'ke', 'kg', 'kh', 'ki', 'km', 'kn', 'kp', 'kr', 'kw', 'ky', 'kz', 'la', 'lb', 'lc',
  'li', 'lk', 'lr', 'ls', 'lt', 'lu', 'lv', 'ly', 'ma', 'mc', 'md', 'me', 'mf', 'mg', 'mh', 'mk',
  'ml', 'mm', 'mn', 'mo', 'mp', 'mq', 'mr', 'ms', 'mt', 'mu', 'mv', 'mw', 'mx', 'my', 'mz', 'na',
  'nc', 'ne', 'nf', 'ng', 'ni', 'nl', 'no', 'np', 'nr', 'nu', 'nz', 'om', 'pa', 'pe', 'pf', 'pg',
  'ph', 'pk', 'pl', 'pm', 'pn', 'pr', 'ps', 'pt', 'pw', 'py', 'qa', 're', 'ro', 'rs', 'ru', 'rw',
  'sa', 'sb', 'sc', 'sd', 'se', 'sg', 'sh', 'si', 'sj', 'sk', 'sl', 'sm', 'sn', 'so', 'sr', 'ss',
  'st', 'sv', 'sx', 'sy', 'sz', 'tc', 'td', 'tf', 'tg', 'th', 'tj', 'tk', 'tl', 'tm', 'tn', 'to',
  'tr', 'tt', 'tv', 'tw', 'tz', 'ua', 'ug', 'um', 'us', 'uy', 'uz', 'va', 'vc', 've', 'vg', 'vi',
  'vn', 'vu', 'wf', 'ws', 'ye', 'yt', 'za', 'zm', 'zw'
] as const

function validCountry(country: string): boolean {
  return /^(?:[a-z]{2}|proxy)$/i.test(country.trim())
}

function validInteger(value: number | string, maximum: number): boolean {
  if (typeof value === 'string' && !/^\d+$/.test(value.trim())) return false
  const number = Number(value)
  return Number.isInteger(number) && number >= 1 && number <= maximum
}

export function proxyNamingRuleError(rule: ProxyNamingRule): boolean {
  return !validCountry(rule.country)
    || !validInteger(rule.start, 999_999_999)
    || !validInteger(rule.digits, 9)
    || !['', '-', '_'].includes(rule.separator)
}

export function generateProxyNames(
  rows: ProxyNamingRow[],
  rule: ProxyNamingRule,
  reservedNames: Iterable<string> = []
): string[] {
  const invalidRule = proxyNamingRuleError(rule)
  const occupied = new Set(Array.from(reservedNames, name => name.trim().toLowerCase()))
  for (const row of rows) {
    if (row.manualName) occupied.add((row.name ?? '').trim().toLowerCase())
  }

  let sequence = Number(rule.start)
  const digits = Number(rule.digits)
  return rows.map(row => {
    if (row.manualName) return (row.name ?? '').trim()
    const country = (row.country?.trim() || rule.country.trim()).toLowerCase()
    if (invalidRule || !validCountry(country)) return ''

    let name: string
    do {
      name = `${country}${rule.separator}${String(sequence++).padStart(digits, '0')}`
    } while (occupied.has(name))
    occupied.add(name)
    return name
  })
}
