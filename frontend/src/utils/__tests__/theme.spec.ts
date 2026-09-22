import { beforeEach, describe, expect, it, vi } from 'vitest'
import { applyTheme, getInitialTheme, setTheme } from '../theme'

describe('theme preferences', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.clear()
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
})
