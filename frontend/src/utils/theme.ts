export type ThemeMode = 'dark' | 'light'

export function getInitialTheme(): ThemeMode {
  const savedTheme = localStorage.getItem('theme')
  if (savedTheme === 'dark' || savedTheme === 'light') return savedTheme
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function applyTheme(mode: ThemeMode): void {
  document.documentElement.classList.toggle('dark', mode === 'dark')
}

export function setTheme(mode: ThemeMode): void {
  applyTheme(mode)
  localStorage.setItem('theme', mode)
}
