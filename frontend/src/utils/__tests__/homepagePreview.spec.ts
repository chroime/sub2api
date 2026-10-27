import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createHomepagePreview, readHomepagePreview, HOMEPAGE_PREVIEW_KEY } from '../homepagePreview'

describe('homepage preview drafts', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.useRealTimers()
  })

  it('keeps only public homepage fields and never persists administrator secrets', () => {
    const id = createHomepagePreview({
      homepage_template: 'brand', site_name: 'Draft brand', contact_info: 'Public support',
      smtp_password: 'smtp-secret', astrbot_api_key: 'bot-secret', token: 'auth-secret',
    })
    expect(readHomepagePreview(id)).toEqual({ homepage_template: 'brand', site_name: 'Draft brand', contact_info: 'Public support' })
    expect(localStorage.getItem(HOMEPAGE_PREVIEW_KEY)).not.toMatch(/secret|smtp|astrbot|token/)
  })

  it('returns a snapshot and only accepts the most recent matching ID', () => {
    const settings = { homepage_template: 'classic', site_name: 'First' }
    const first = createHomepagePreview(settings)
    const snapshot = readHomepagePreview(first)
    settings.site_name = 'Second'
    const second = createHomepagePreview(settings)
    expect(second).not.toBe(first)
    expect(readHomepagePreview(first)).toBeNull()
    expect(snapshot?.site_name).toBe('First')
    expect(readHomepagePreview(second)?.site_name).toBe('Second')
    expect(readHomepagePreview('invalid')).toBeNull()
  })

  it('expires after 30 minutes and tolerates corrupt or unavailable storage', () => {
    vi.useFakeTimers()
    const id = createHomepagePreview({ homepage_template: 'compact' })
    vi.advanceTimersByTime(30 * 60 * 1000)
    expect(readHomepagePreview(id)).toBeNull()
    localStorage.setItem(HOMEPAGE_PREVIEW_KEY, '{broken')
    expect(readHomepagePreview(id)).toBeNull()
    vi.useRealTimers()
  })

  it('validates persisted types and resolves legacy selection on creation', () => {
    const id = createHomepagePreview({ home_content: '<h1>Custom</h1>', site_name: { private: 'value' } })
    expect(readHomepagePreview(id)).toEqual({ homepage_template: 'custom', home_content: '<h1>Custom</h1>' })
    const stored = JSON.parse(localStorage.getItem(HOMEPAGE_PREVIEW_KEY)!)
    stored.settings.homepage_template = 'unknown'
    localStorage.setItem(HOMEPAGE_PREVIEW_KEY, JSON.stringify(stored))
    expect(readHomepagePreview(id)).toBeNull()
  })
})
