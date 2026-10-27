import { describe, expect, it } from 'vitest'
import { parseProxyInput } from '@/utils/proxyImport'

describe('proxy batch input parsing (IPv6 support)', () => {
  it.each([
    ['socks5://[2001:db8::1]:1080', 'socks5', '2001:db8::1', 1080],
    ['socks5h://[2001:db8::1]:1080', 'socks5h', '2001:db8::1', 1080],
    ['http://[::1]:8080', 'http', '::1', 8080],
    ['[2001:db8::1]:1080', 'socks5', '2001:db8::1', 1080],
    ['[::ffff:192.0.2.1]:1080', 'socks5', '::ffff:192.0.2.1', 1080]
  ])('recognizes %s and returns a host without brackets', (line, protocol, host, port) => {
    expect(parseProxyInput(line as string)).toEqual({
      protocol, host, port, username: '', password: ''
    })
  })

  it('recognizes credentials in an IPv6 URI', () => {
    expect(parseProxyInput('socks5://demo:PassWord@[2001:db8::1]:1080')).toEqual({
      protocol: 'socks5', host: '2001:db8::1', port: 1080,
      username: 'demo', password: 'PassWord'
    })
  })

  it('recognizes provider-format credentials after an IPv6 endpoint', () => {
    expect(parseProxyInput('[2001:db8::1]:1080:demo:Pass:Word@/#')).toEqual({
      protocol: 'socks5', host: '2001:db8::1', port: 1080,
      username: 'demo', password: 'Pass:Word@/#'
    })
  })

  it.each([
    'socks5://2001:db8::1:1080',
    '2001:db8::1:1080',
    '2001:1234::1:1080',
    'demo:PassWord@2001:db8::1:1080',
    '[2001:db8::1:1080',
    '2001:db8::1]:1080',
    '[2001:db8:::1]:1080',
    '[2001:db8::gg]:1080',
    '[1:2:3:4:5:6:7:8:9]:1080',
    '[127.0.0.1]:1080',
    '[::1]:65536'
  ])('leaves ambiguous or invalid IPv6 input for correction: %s', (line) => {
    expect(parseProxyInput(line)).toBeNull()
  })
})
