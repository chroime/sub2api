import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import Icon from '@/components/icons/Icon.vue'
import { resolveSmartOperationsSection, SMART_OPERATIONS_SECTIONS } from './smartOperations'

describe('resolveSmartOperationsSection', () => {
  it.each(['overview', 'import', 'models', 'monitor', 'history'])(
    'accepts the supported %s section',
    (section) => expect(resolveSmartOperationsSection(section)).toBe(section),
  )

  it.each([undefined, null, '', 'unknown', 'Models', 'models/extra', ['models'], {}, 42])(
    'defaults malformed or absent route parameters %j to overview',
    (value) => expect(resolveSmartOperationsSection(value)).toBe('overview'),
  )
})

describe('shared Smart Operations section icons', () => {
  it('provides renderable icons for every sidebar and top-tab section', () => {
    expect(SMART_OPERATIONS_SECTIONS).toHaveLength(5)
    for (const section of SMART_OPERATIONS_SECTIONS) {
      const icon = mount(Icon, { props: { name: section.icon } })
      expect(icon.get('path').attributes('d'), section.id).toBeTruthy()
      icon.unmount()
    }
  })
})
