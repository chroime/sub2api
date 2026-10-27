import { describe, expect, it } from 'vitest'
import {
  normalizeProxyImportFields,
  parseProxyInput,
  proxyImportValidationError,
  type ProxyImportDraft
} from '../proxyImport'

describe('parseProxyInput', () => {
  it('defaults an IPv4 endpoint to SOCKS5', () => {
    expect(parseProxyInput('198.51.100.27:1080')).toEqual({
      protocol: 'socks5', host: '198.51.100.27', port: 1080, username: '', password: ''
    })
  })

  it('recognizes the provider host:port:user:password format', () => {
    expect(parseProxyInput('198.51.100.27:18080:demo_user:DemoPass42')).toEqual({
      protocol: 'socks5', host: '198.51.100.27', port: 18080,
      username: 'demo_user', password: 'DemoPass42'
    })
  })

  it('keeps every remaining colon and reserved character in a provider password', () => {
    expect(parseProxyInput('proxy.example:1080:demo:Case:Sensitive@/#?%3A')).toEqual({
      protocol: 'socks5', host: 'proxy.example', port: 1080,
      username: 'demo', password: 'Case:Sensitive@/#?%3A'
    })
  })

  it('preserves a raw percent-encoded-looking password in shorthand userinfo', () => {
    expect(parseProxyInput('demo:Pass%40Word+More@proxy.example:1080')).toEqual({
      protocol: 'socks5', host: 'proxy.example', port: 1080,
      username: 'demo', password: 'Pass%40Word+More'
    })
  })

  it('recognizes shorthand userinfo with a password containing colons', () => {
    expect(parseProxyInput('demo:Case:Sensitive@proxy.example:1080')).toEqual({
      protocol: 'socks5', host: 'proxy.example', port: 1080,
      username: 'demo', password: 'Case:Sensitive'
    })
  })

  it.each(['http', 'https', 'socks5', 'socks5h'])('retains the explicit %s protocol', (protocol) => {
    expect(parseProxyInput(`${protocol}://demo:PassWord@proxy.example:1080`)).toEqual({
      protocol, host: 'proxy.example', port: 1080, username: 'demo', password: 'PassWord'
    })
  })

  it('normalizes only the explicit protocol case', () => {
    expect(parseProxyInput('HTTP://DemoUser:MixedCase@Proxy.Example:8080')).toEqual({
      protocol: 'http', host: 'Proxy.Example', port: 8080,
      username: 'DemoUser', password: 'MixedCase'
    })
  })

  it.each([
    ['http://proxy.example:80', 'http', 80],
    ['https://proxy.example:443', 'https', 443]
  ])('retains an explicitly supplied default port in %s', (line, protocol, port) => {
    expect(parseProxyInput(line as string)).toEqual({
      protocol, host: 'proxy.example', port, username: '', password: ''
    })
  })

  it('decodes URI credentials once without interpreting plus as a space', () => {
    expect(parseProxyInput('socks5://demo%40user:p%3AA%40B%2FC%23D%25%253A+@proxy.example:1080')).toEqual({
      protocol: 'socks5', host: 'proxy.example', port: 1080,
      username: 'demo@user', password: 'p:A@B/C#D%%3A+'
    })
  })

  it('decodes UTF-8 URI credentials', () => {
    expect(parseProxyInput('https://%E7%94%A8%E6%88%B7:%E5%AF%86%E7%A0%81@proxy.example:443')).toEqual({
      protocol: 'https', host: 'proxy.example', port: 443, username: '用户', password: '密码'
    })
  })

  it('retains legacy raw URI password characters', () => {
    expect(parseProxyInput('socks5://demo:Pass:Word/#?@proxy.example:1080')).toEqual({
      protocol: 'socks5', host: 'proxy.example', port: 1080,
      username: 'demo', password: 'Pass:Word/#?'
    })
  })

  it.each([
    'proxy.example:1080:demo:',
    'proxy.example:1080::secret',
    'https://demo:@proxy.example:443',
    'https://:secret@proxy.example:443',
    'https://demo:secret%00tail@proxy.example:443',
    'https://demo%00tail:secret@proxy.example:443',
    'https://demo:secret%0Atail@proxy.example:443'
  ])('leaves incomplete or unsupported authentication for correction: %s', (line) => {
    expect(parseProxyInput(line)).toBeNull()
  })

  it.each([
    'demo:1080:secret@proxy.example:8080',
    'source.example:0:demo:secret@proxy.example:8080',
    '999.1.1.1:1080:demo:secret@proxy.example:8080'
  ])('does not guess between provider credentials and userinfo in %s', (line) => {
    expect(parseProxyInput(line)).toBeNull()
  })

  it('accepts surrounding paste whitespace and a domain endpoint', () => {
    expect(parseProxyInput('  proxy.example:65535\r\n')).toEqual({
      protocol: 'socks5', host: 'proxy.example', port: 65535, username: '', password: ''
    })
  })

  it.each([
    'localhost:1',
    'proxy_service.internal:1080',
    'xn--bcher-kva.example:1080',
    'bücher.example:1080',
    'proxy.example.:1080'
  ])('accepts the existing hostname form %s', (line) => {
    expect(parseProxyInput(line)).not.toBeNull()
  })

  it.each([
    '',
    '  ',
    'proxy.example',
    'proxy.example:1080:demo',
    'demo@proxy.example:1080',
    'http://proxy.example',
    'http://proxy.example:8080/path',
    'http://proxy.example:8080/',
    'http://proxy.example:8080?query',
    'socks5://proxy.example:1080#fragment',
    'ftp://proxy.example:21',
    'socks4://proxy.example:1080',
    'http:/proxy.example:8080',
    'socks5://demo:bad%2@proxy.example:1080',
    'socks5://demo:%FF@proxy.example:1080',
    '198.51.100.999:1080',
    '198.51.100:1080',
    '198.51.100.1234:1080',
    'proxy example:1080',
    'proxy\texample:1080',
    'proxy.example/path:1080',
    'proxy.example\\path:1080',
    'proxy.example?query:1080',
    'proxy.example#fragment:1080',
    'proxy..example:1080',
    '.proxy.example:1080',
    '-proxy.example:1080',
    'proxy-.example:1080',
    '[proxy.example]:1080',
    ':1080',
    'proxy.example:0',
    'proxy.example:65536',
    'proxy.example:-1',
    'proxy.example:+1080',
    'proxy.example:1080.5',
    'proxy.example:1e3',
    'proxy.example:0x50',
    'proxy.example:port',
    'proxy.example:',
    'http://proxy.example:0',
    'https://proxy.example:65536'
  ])('leaves an invalid or uncertain input for manual correction: %s', (line) => {
    expect(parseProxyInput(line)).toBeNull()
  })
})

describe('manual proxy import drafts', () => {
  const draft: ProxyImportDraft = {
    protocol: 'socks5', host: 'proxy.example', port: '1080', username: '', password: ''
  }

  it('validates a corrected draft independently from its original input', () => {
    expect(proxyImportValidationError(draft)).toBeNull()
  })

  it.each([
    { username: 'demo', password: '' },
    { username: '', password: 'secret' },
    { username: 'demo', password: '  ' },
    { username: '  ', password: 'secret' },
    { username: 'demo', password: 'secret\u0000tail' },
    { username: 'demo\u0000tail', password: 'secret' },
    { username: 'demo', password: 'secret\ntail' }
  ])('identifies incomplete or unsupported edited credentials: %j', (credentials) => {
    expect(proxyImportValidationError({ ...draft, ...credentials })).toBe('credentials')
  })

  it.each(['', 'socks4', 'ftp'])('identifies an unsupported draft protocol %s', (protocol) => {
    expect(proxyImportValidationError({ ...draft, protocol })).toBe('protocol')
  })

  it.each(['', ' ', 'proxy example', 'https://proxy.example', 'proxy.example/path', '999.1.1.1', '[::1', '::gg'])('identifies an invalid draft host %s', (host) => {
    expect(proxyImportValidationError({ ...draft, host })).toBe('host')
  })

  it.each(['', ' ', '0', '65536', '-1', '+1080', '1080.5', '1e3', '0x50', 0, 65536, 1.5, NaN, Infinity])('identifies an invalid draft port %s', (port) => {
    expect(proxyImportValidationError({ ...draft, port })).toBe('port')
  })

  it.each([1, 65535, '1', '65535', ' 1080 ', '01080'])('accepts a decimal port at the boundary or after manual editing: %s', (port) => {
    expect(proxyImportValidationError({ ...draft, port })).toBeNull()
  })

  it('normalizes host, protocol, and port while preserving credential whitespace and encoding', () => {
    const corrected: ProxyImportDraft = {
      protocol: ' HTTPS ', host: '  [2001:db8::1]  ', port: ' 00443 ',
      username: '  DemoUser  ', password: '  Pass:%40Word  '
    }
    expect(proxyImportValidationError(corrected)).toBeNull()
    expect(normalizeProxyImportFields(corrected)).toEqual({
      protocol: 'https', host: '2001:db8::1', port: 443,
      username: '  DemoUser  ', password: '  Pass:%40Word  '
    })
    expect(corrected.host).toBe('  [2001:db8::1]  ')
    expect(corrected.port).toBe(' 00443 ')
  })

  it('accepts an IPv6 literal without brackets in the separate host field', () => {
    expect(proxyImportValidationError({ ...draft, host: '2001:db8::1' })).toBeNull()
    expect(normalizeProxyImportFields({ ...draft, host: '2001:db8::1' })).toEqual({
      protocol: 'socks5', host: '2001:db8::1', port: 1080, username: '', password: ''
    })
  })
})
