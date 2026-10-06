export const SMART_OPERATIONS_SECTIONS = [
  {
    id: 'overview',
    path: '/admin/upstream-governance',
    labelKey: 'governance.smartOperations.sections.overview',
    descriptionKey: 'governance.smartOperations.descriptions.overview',
    icon: 'globe',
  },
  {
    id: 'import',
    path: '/admin/upstream-governance/import',
    labelKey: 'governance.smartOperations.sections.import',
    descriptionKey: 'governance.smartOperations.descriptions.import',
    icon: 'upload',
  },
  {
    id: 'models',
    path: '/admin/upstream-governance/models',
    labelKey: 'governance.smartOperations.sections.models',
    descriptionKey: 'governance.smartOperations.descriptions.models',
    icon: 'cpu',
  },
  {
    id: 'monitor',
    path: '/admin/upstream-governance/monitor',
    labelKey: 'governance.smartOperations.sections.monitor',
    descriptionKey: 'governance.smartOperations.descriptions.monitor',
    icon: 'chart',
  },
  {
    id: 'history',
    path: '/admin/upstream-governance/history',
    labelKey: 'governance.smartOperations.sections.history',
    descriptionKey: 'governance.smartOperations.descriptions.history',
    icon: 'clock',
  },
] as const

export type SmartOperationsSection = (typeof SMART_OPERATIONS_SECTIONS)[number]['id']

export function resolveSmartOperationsSection(value: unknown): SmartOperationsSection {
  return SMART_OPERATIONS_SECTIONS.find((section) => section.id === value)?.id ?? 'overview'
}
