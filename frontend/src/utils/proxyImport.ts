import type { ProxyProtocol } from '@/types'

export type ProxyImportFields = {
  protocol: ProxyProtocol
  host: string
  port: number
  username: string
  password: string
}

export type ProxyImportDraft = {
  protocol: string
  host: string
  port: string | number
  username: string
  password: string
}

const protocols: readonly string[] = ['http', 'https', 'socks5', 'socks5h']

function isIPv6(host: string): boolean {
  if (!host.includes(':') || /[^0-9a-f:.]/i.test(host)) return false
  try {
    // Validate the literal without using URL's normalized host, port, or userinfo.
    new URL(`http://[${host}]:1`)
    return true
  } catch {
    return false
  }
}

function normalizedHost(host: string): string {
  const trimmed = host.trim()
  return trimmed.startsWith('[') && trimmed.endsWith(']') ? trimmed.slice(1, -1) : trimmed
}

function validHost(rawHost: string): boolean {
  const host = rawHost.trim()
  if (!host || /[\s/\\?#@]/.test(host)) return false
  if (host.startsWith('[') || host.endsWith(']')) {
    return host.startsWith('[') && host.endsWith(']') && isIPv6(host.slice(1, -1))
  }
  if (host.includes(':')) return isIPv6(host)

  const domain = host.endsWith('.') ? host.slice(0, -1) : host
  if (!domain || domain.length > 253) return false
  if (/^[\d.]+$/.test(domain) && domain.includes('.')) {
    const octets = domain.split('.')
    return octets.length === 4 && octets.every((octet) => /^\d{1,3}$/.test(octet) && Number(octet) <= 255)
  }
  // Keep international names and intranet names with underscores usable.
  return domain.split('.').every((label) => (
    label.length > 0 && label.length <= 63 &&
    /^[\p{L}\p{N}\p{M}_-]+$/u.test(label) && !label.startsWith('-') && !label.endsWith('-')
  ))
}

export function proxyImportValidationError(draft: ProxyImportDraft): 'protocol' | 'host' | 'port' | 'credentials' | null {
  if (!protocols.includes(draft.protocol.trim().toLowerCase())) return 'protocol'
  if (!validHost(draft.host)) return 'host'
  if (typeof draft.port === 'string' && !/^\d+$/.test(draft.port.trim())) return 'port'
  const port = Number(draft.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return 'port'
  // The server trims credentials and only enables authentication with both fields.
  if (Boolean(draft.username.trim()) !== Boolean(draft.password.trim())) return 'credentials'
  if ([draft.username, draft.password].some(value => Array.from(value).some(character => {
    const code = character.charCodeAt(0)
    return code < 32 || code === 127
  }))) return 'credentials'
  return null
}

/** Normalize a draft after proxyImportValidationError has accepted it. */
export function normalizeProxyImportFields(draft: ProxyImportDraft): ProxyImportFields {
  return {
    protocol: draft.protocol.trim().toLowerCase() as ProxyProtocol,
    host: normalizedHost(draft.host),
    port: Number(draft.port),
    username: draft.username,
    password: draft.password
  }
}

function parsedFields(draft: ProxyImportDraft): ProxyImportFields | null {
  if (/\s/.test(draft.host) || proxyImportValidationError(draft)) return null
  return normalizeProxyImportFields(draft)
}

/** Parse one pasted endpoint; return null when the row needs manual correction. */
export function parseProxyInput(line: string): ProxyImportFields | null {
  const trimmed = line.trim()
  if (!trimmed || /[\r\n\0]/.test(trimmed)) return null

  const scheme = trimmed.match(/^([a-z][a-z\d+.-]*):\/\//i)
  const protocol = scheme ? scheme[1].toLowerCase() : 'socks5'
  if (!protocols.includes(protocol)) return null
  const authority = scheme ? trimmed.slice(scheme[0].length) : trimmed

  let providerFields: ProxyImportFields | null = null
  if (!scheme) {
    // A complete IPv6 literal can resemble host:port:user:password; require brackets.
    if (isIPv6(authority)) return null
    const provider = authority.match(/^(\[[^\]]+\]|[^:\[\]]+):(\d+)(?::([^:]*):(.*))?$/)
    if (provider) {
      const [, host, port, username = '', password = ''] = provider
      providerFields = parsedFields({ protocol, host, port, username, password })
      if (!providerFields) return null
    }
  }

  const at = authority.lastIndexOf('@')
  if (at < 0 && providerFields) return providerFields
  let endpoint = authority
  let username = ''
  let password = ''
  if (at >= 0) {
    endpoint = authority.slice(at + 1)
    const userinfo = authority.slice(0, at)
    const separator = userinfo.indexOf(':')
    if (separator < 0) return providerFields
    username = userinfo.slice(0, separator)
    password = userinfo.slice(separator + 1)
    if (scheme) {
      try {
        username = decodeURIComponent(username)
        password = decodeURIComponent(password)
      } catch {
        return null
      }
    }
  }

  const hostPort = endpoint.match(/^(\[[^\]]+\]|[^:\[\]]+):(\d+)$/)
  if (!hostPort) return providerFields
  const uriFields = parsedFields({ protocol, host: hostPort[1], port: hostPort[2], username, password })
  // Both grammars can match numeric userinfo passwords; leave that choice to the user.
  if (providerFields && uriFields) return null
  return providerFields ?? uriFields
}
