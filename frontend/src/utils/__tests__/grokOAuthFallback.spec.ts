import { describe, expect, it } from 'vitest'
import { isGrokSSOBrowserRequired } from '@/utils/grokOAuthFallback'

describe('Grok SSO browser fallback detection', () => {
  it('recognizes a browser-required failure embedded in a batch item message', () => {
    expect(
      isGrokSSOBrowserRequired(
        '#1: GROK_SSO_BROWSER_REQUIRED: xAI requires browser verification to approve this device'
      )
    ).toBe(true)
  })

  it('does not treat unrelated SSO failures as browser-required', () => {
    expect(isGrokSSOBrowserRequired('GROK_SSO_UNAUTHORIZED: cookie expired')).toBe(false)
  })
})
