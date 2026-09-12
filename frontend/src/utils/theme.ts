/** Apply the product-wide fixed dark theme. */
export function applyFixedDarkTheme(): void {
  document.documentElement.classList.add('dark')
}

export function isFixedDarkTheme(): true {
  return true
}
