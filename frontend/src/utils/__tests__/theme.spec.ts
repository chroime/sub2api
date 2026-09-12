import { beforeEach, describe, expect, it } from 'vitest'
import { applyFixedDarkTheme, isFixedDarkTheme } from '../theme'

describe('fixed dark theme', () => {
  beforeEach(() => {
    document.documentElement.classList.remove('dark')
    localStorage.setItem('theme', 'light')
  })

  it('always applies dark regardless of persisted theme', () => {
    applyFixedDarkTheme()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(isFixedDarkTheme()).toBe(true)
  })
})
