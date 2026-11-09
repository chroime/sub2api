import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { applyTheme, getInitialTheme, setTheme, startThemeSync } from '../theme'

describe('theme preferences', () => {
  let stopSync: (() => void) | undefined

  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.clear()
  })

  afterEach(() => {
    stopSync?.()
    stopSync = undefined
    vi.restoreAllMocks()
  })

  it('prefers a persisted theme and applies it to the document', () => {
    localStorage.setItem('theme', 'light')
    expect(getInitialTheme()).toBe('light')
    applyTheme(getInitialTheme())
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    setTheme('dark')
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
  })

  it('uses the system preference when no theme is persisted', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList)
    expect(getInitialTheme()).toBe('dark')
  })

  it('updates an already-open page when another tab changes its theme', () => {
    localStorage.setItem('theme', 'dark')
    stopSync = startThemeSync()
    expect(document.documentElement.classList.contains('dark')).toBe(true)

    localStorage.setItem('theme', 'light')
    window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'light', storageArea: localStorage }))
    expect(document.documentElement.classList.contains('dark')).toBe(false)

    localStorage.setItem('theme', 'dark')
    window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'dark', storageArea: localStorage }))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('ignores unrelated storage and session-storage theme changes', () => {
    localStorage.setItem('theme', 'light')
    stopSync = startThemeSync()
    window.dispatchEvent(new StorageEvent('storage', { key: 'locale', newValue: 'dark', storageArea: localStorage }))
    window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'dark', storageArea: sessionStorage }))
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('resynchronizes on focus, pageshow and becoming visible after another tab changed the preference', () => {
    localStorage.setItem('theme', 'light')
    stopSync = startThemeSync()
    for (const event of ['focus', 'pageshow']) {
      localStorage.setItem('theme', 'dark')
      applyTheme('light')
      window.dispatchEvent(new Event(event))
      expect(document.documentElement.classList.contains('dark')).toBe(true)
    }
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible')
    localStorage.setItem('theme', 'light')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('follows system changes only when there is no explicit saved preference', () => {
    const media = new EventTarget() as MediaQueryList
    Object.defineProperty(media, 'matches', { value: false, configurable: true })
    vi.spyOn(window, 'matchMedia').mockReturnValue(media)
    stopSync = startThemeSync()
    Object.defineProperty(media, 'matches', { value: true, configurable: true })
    media.dispatchEvent(new Event('change'))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    setTheme('light')
    media.dispatchEvent(new Event('change'))
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('uses the system preference after another tab clears the stored theme', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() } as unknown as MediaQueryList)
    localStorage.setItem('theme', 'light')
    stopSync = startThemeSync()
    localStorage.clear()
    window.dispatchEvent(new StorageEvent('storage', { key: null, newValue: null, storageArea: localStorage }))
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })

  it('does not keep changing the document after synchronization is stopped', () => {
    localStorage.setItem('theme', 'light')
    stopSync = startThemeSync()
    stopSync()
    localStorage.setItem('theme', 'dark')
    window.dispatchEvent(new StorageEvent('storage', { key: 'theme', newValue: 'dark', storageArea: localStorage }))
    window.dispatchEvent(new Event('focus'))
    expect(document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('applies a chosen theme when browser storage is unavailable', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('storage unavailable') })
    expect(() => setTheme('dark')).not.toThrow()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
  })
})
