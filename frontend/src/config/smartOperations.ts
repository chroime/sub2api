// Site sections work on the upstream picked in the left rail; global sections
// span every upstream (model monitoring keeps the rail for its upstream mode).
export const SMART_OPERATIONS_SECTIONS = [
  {
    id: 'overview',
    path: '/admin/upstream-governance',
    labelKey: 'governance.smartOperations.sections.overview',
    descriptionKey: 'governance.smartOperations.descriptions.overview',
    icon: 'globe',
    scope: 'site',
  },
  {
    id: 'groups',
    path: '/admin/upstream-governance/groups',
    labelKey: 'governance.smartOperations.sections.groups',
    descriptionKey: 'governance.smartOperations.descriptions.groups',
    icon: 'upload',
    scope: 'site',
  },
  {
    id: 'pricing',
    path: '/admin/upstream-governance/pricing',
    labelKey: 'governance.smartOperations.sections.pricing',
    descriptionKey: 'governance.smartOperations.descriptions.pricing',
    icon: 'dollar',
    scope: 'site',
  },
  {
    id: 'automation',
    path: '/admin/upstream-governance/automation',
    labelKey: 'governance.smartOperations.sections.automation',
    descriptionKey: 'governance.smartOperations.descriptions.automation',
    icon: 'bolt',
    scope: 'site',
  },
  {
    id: 'history',
    path: '/admin/upstream-governance/history',
    labelKey: 'governance.smartOperations.sections.history',
    descriptionKey: 'governance.smartOperations.descriptions.history',
    icon: 'clock',
    scope: 'site',
  },
  {
    id: 'models',
    path: '/admin/upstream-governance/models',
    labelKey: 'governance.smartOperations.sections.models',
    descriptionKey: 'governance.smartOperations.descriptions.models',
    icon: 'cpu',
    scope: 'global',
  },
  {
    id: 'account-quality',
    path: '/admin/upstream-governance/account-quality',
    labelKey: 'governance.smartOperations.sections.accountQuality',
    descriptionKey: 'governance.smartOperations.descriptions.accountQuality',
    icon: 'shield',
    scope: 'global',
  },
  {
    id: 'settings',
    path: '/admin/upstream-governance/settings',
    labelKey: 'governance.smartOperations.sections.settings',
    descriptionKey: 'governance.smartOperations.descriptions.settings',
    icon: 'cog',
    scope: 'global',
  },
] as const

export type SmartOperationsSection = (typeof SMART_OPERATIONS_SECTIONS)[number]['id']

/**
 * Section ids from before the regrouping. Old links, readiness items and
 * workbench tasks still use them; local-monitor opens model monitoring in its
 * local mode and notifications opens the delivery records in settings.
 */
export const LEGACY_SMART_OPERATIONS_SECTIONS = {
  import: 'groups',
  monitor: 'automation',
  'local-monitor': 'models',
  notifications: 'settings',
} as const satisfies Record<string, SmartOperationsSection>

export type LegacySmartOperationsSection = keyof typeof LEGACY_SMART_OPERATIONS_SECTIONS

export function isLegacySmartOperationsSection(value: unknown): value is LegacySmartOperationsSection {
  return typeof value === 'string' && Object.prototype.hasOwnProperty.call(LEGACY_SMART_OPERATIONS_SECTIONS, value)
}

export function resolveSmartOperationsSection(value: unknown): SmartOperationsSection {
  if (isLegacySmartOperationsSection(value)) return LEGACY_SMART_OPERATIONS_SECTIONS[value]
  return SMART_OPERATIONS_SECTIONS.find((section) => section.id === value)?.id ?? 'overview'
}
