export const homepageTemplates = ['classic', 'brand', 'compact', 'custom'] as const
export type HomepageTemplate = typeof homepageTemplates[number]

export interface HomepagePresentationProps {
  siteName: string
  siteLogo: string
  subtitle: string
  docUrl: string
  apiBaseUrl: string
  destination: string
  authenticated: boolean
  showModelPlazaEntry: boolean
  currentYear: number
}

export function isHomepageTemplate(value: unknown): value is HomepageTemplate {
  return typeof value === 'string' && homepageTemplates.some(template => template === value)
}

export function resolveHomepageTemplate(settings: {
  homepage_template?: unknown
  home_content?: string
  compact_home_enabled?: boolean
}): HomepageTemplate {
  if (isHomepageTemplate(settings.homepage_template)) return settings.homepage_template
  if (settings.home_content?.trim()) return 'custom'
  if (settings.compact_home_enabled) return 'compact'
  return 'classic'
}
