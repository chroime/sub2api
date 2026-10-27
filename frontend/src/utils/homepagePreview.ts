import type { PublicSettings } from '@/types'
import { isHomepageTemplate, resolveHomepageTemplate } from './homepageTemplate'

export const HOMEPAGE_PREVIEW_KEY = 'sub2api:homepage-preview'
const PREVIEW_LIFETIME = 30 * 60 * 1000
const stringFields = ['site_name', 'site_logo', 'site_subtitle', 'doc_url', 'api_base_url', 'home_content', 'contact_info'] as const
const booleanFields = ['compact_home_enabled', 'model_plaza_enabled', 'model_plaza_require_auth', 'backend_mode_enabled'] as const

// Explicit allowlist: never serialize the admin form, which also contains secrets.
function publicDraft(input: Record<string, unknown>): Partial<PublicSettings> {
  const output: Partial<PublicSettings> = {}
  for (const key of stringFields) if (typeof input[key] === 'string') output[key] = input[key]
  for (const key of booleanFields) if (typeof input[key] === 'boolean') output[key] = input[key]
  output.homepage_template = resolveHomepageTemplate({ ...output, homepage_template: input.homepage_template })
  return output
}

export function createHomepagePreview(input: Record<string, unknown>): string {
  const id = typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('')
  localStorage.setItem(HOMEPAGE_PREVIEW_KEY, JSON.stringify({ id, expiresAt: Date.now() + PREVIEW_LIFETIME, settings: publicDraft(input) }))
  return id
}

export function readHomepagePreview(id: unknown): Partial<PublicSettings> | null {
  try {
    if (typeof id !== 'string' || !id) return null
    const draft = JSON.parse(localStorage.getItem(HOMEPAGE_PREVIEW_KEY) || 'null')
    if (!draft || draft.id !== id || !Number.isFinite(draft.expiresAt) || draft.expiresAt <= Date.now()) return null
    if (!draft.settings || !isHomepageTemplate(draft.settings.homepage_template)) return null
    return publicDraft(draft.settings)
  } catch {
    return null
  }
}
