import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Icon from '@/components/icons/Icon.vue'
import { resolveSmartOperationsSection, SMART_OPERATIONS_SECTIONS } from './smartOperations'

describe('resolveSmartOperationsSection', () => {
  it.each(['overview', 'groups', 'pricing', 'automation', 'history', 'models', 'account-quality', 'settings'])(
    'accepts the supported %s section',
    (section) => expect(resolveSmartOperationsSection(section)).toBe(section),
  )

  it.each([['import', 'groups'], ['monitor', 'automation'], ['local-monitor', 'models'], ['notifications', 'settings']])(
    'maps the pre-regrouping %s section to %s',
    (legacy, section) => expect(resolveSmartOperationsSection(legacy)).toBe(section),
  )

  it('does not resolve inherited object keys as legacy sections', () => {
    expect(resolveSmartOperationsSection('toString')).toBe('overview')
    expect(resolveSmartOperationsSection('__proto__')).toBe('overview')
  })

  it.each([undefined, null, '', 'unknown', 'Models', 'models/extra', ['models'], {}, 42])(
    'defaults malformed or absent route parameters %j to overview',
    (value) => expect(resolveSmartOperationsSection(value)).toBe('overview'),
  )
})

describe('shared Smart Operations section icons', () => {
  it('provides renderable icons for every sidebar and top-tab section', () => {
    expect(SMART_OPERATIONS_SECTIONS.map(section => `${section.scope}:${section.id}`)).toEqual([
      'site:overview', 'site:groups', 'site:pricing', 'site:automation', 'site:history', 'global:models', 'global:account-quality', 'global:settings',
    ])
    for (const section of SMART_OPERATIONS_SECTIONS) {
      const icon = mount(Icon, { props: { name: section.icon } })
      expect(icon.get('path').attributes('d'), section.id).toBeTruthy()
      icon.unmount()
    }
  })
})
