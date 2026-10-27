import { describe, expect, it } from 'vitest'
import { resolveHomepageTemplate } from '../homepageTemplate'

describe('homepage selection', () => {
  it.each(['classic', 'brand', 'compact', 'custom'] as const)('honors explicit %s over legacy switches', (homepage_template) => {
    expect(resolveHomepageTemplate({ homepage_template, home_content: '<p>Old home</p>', compact_home_enabled: true })).toBe(homepage_template)
  })

  it('preserves custom and compact legacy sites while defaulting new sites to classic', () => {
    expect(resolveHomepageTemplate({ home_content: 'https://example.test', compact_home_enabled: true })).toBe('custom')
    expect(resolveHomepageTemplate({ home_content: ' \n ', compact_home_enabled: true })).toBe('compact')
    expect(resolveHomepageTemplate({})).toBe('classic')
    expect(resolveHomepageTemplate({ homepage_template: 'unrecognized' })).toBe('classic')
  })
})
