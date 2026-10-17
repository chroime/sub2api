import { describe, expect, it } from 'vitest'
import en from '../locales/en/index'
import zh from '../locales/zh/index'

describe('IQ monitoring labels', () => {
  it('uses monitoring wording in the user navigation and page title', () => {
    expect(zh.nav.iqDetection).toBe('智商监测')
    expect(zh.iqDetection.title).toBe('智商监测')
    expect(en.nav.iqDetection).toBe('IQ Monitoring')
    expect(en.iqDetection.title).toBe('IQ Monitoring')
  })
})
