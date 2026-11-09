export type ThemeMode = 'dark' | 'light'

export function getInitialTheme(): ThemeMode {
  try {
    const savedTheme = localStorage.getItem('theme')
    if (savedTheme === 'dark' || savedTheme === 'light') return savedTheme
  } catch {
    // Storage can be disabled while the page is still allowed to render.
  }
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function applyTheme(mode: ThemeMode): void {
  document.documentElement.classList.toggle('dark', mode === 'dark')
}

export function setTheme(mode: ThemeMode): void {
  applyTheme(mode)
  try {
    localStorage.setItem('theme', mode)
  } catch {
    // Keep the selected appearance usable even when persistence is blocked.
  }
}

/** Keep every open page on this origin in sync, including restored auth pages. */
export function startThemeSync(): () => void {
  const sync = () => applyTheme(getInitialTheme())
  const systemPreference = window.matchMedia('(prefers-color-scheme: dark)')
  const onStorage = (event: StorageEvent) => {
    if (event.key !== 'theme' && event.key !== null) return
    try {
      if (event.storageArea && event.storageArea !== localStorage) return
    } catch {
      return
    }
    sync()
  }
  const onVisibility = () => {
    if (document.visibilityState === 'visible') sync()
  }
  const onSystemChange = () => {
    try {
      const saved = localStorage.getItem('theme')
      if (saved === 'light' || saved === 'dark') return
    } catch {
      // Without a saved preference, continue to follow the operating system.
    }
    sync()
  }

  sync()
  window.addEventListener('storage', onStorage)
  window.addEventListener('focus', sync)
  window.addEventListener('pageshow', sync)
  document.addEventListener('visibilitychange', onVisibility)
  systemPreference.addEventListener?.('change', onSystemChange)

  return () => {
    window.removeEventListener('storage', onStorage)
    window.removeEventListener('focus', sync)
    window.removeEventListener('pageshow', sync)
    document.removeEventListener('visibilitychange', onVisibility)
    systemPreference.removeEventListener?.('change', onSystemChange)
  }
}
