import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GroupBadge from '../GroupBadge.vue'
import GroupOptionItem from '../GroupOptionItem.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: { server_utc_offset: '+08:00' } }) }))

describe('explicit group identity', () => {
  it('renders the configured identity before the platform and name as text', () => {
    const label = '<img src=x onerror=alert(1)>'
    const wrapper = mount(GroupBadge, { props: { name: 'Main group', platform: 'openai', identity: { label, color: 'teal' }, rateMultiplier: 0.5 } })
    expect(wrapper.get('[data-group-identity]').text()).toBe(label)
    expect(wrapper.get('[data-group-identity]').classes()).toContain('bg-teal-700')
    expect(wrapper.get('[data-group-identity]').find('svg').attributes('aria-hidden')).toBe('true')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text().indexOf(label)).toBeLessThan(wrapper.text().indexOf('Main group'))
    expect(wrapper.text()).toContain('0.5x')
  })

  it.each([undefined, {}, { label: '   ', color: 'rose' }])('keeps legacy groups unmarked for identity %j', identity => {
    const wrapper = mount(GroupBadge, { props: { name: '自营候选', platform: 'grok', identity, rateMultiplier: 1 } })
    expect(wrapper.find('[data-group-identity]').exists()).toBe(false)
    expect(wrapper.text()).toContain('自营候选')
    expect(wrapper.text()).toContain('1x')
  })

  it.each([
    ['#ffffff', 'rgb(255, 255, 255)', 'rgb(0, 0, 0)'],
    ['#000000', 'rgb(0, 0, 0)', 'rgb(255, 255, 255)'],
    ['#777777', 'rgb(119, 119, 119)', 'rgb(0, 0, 0)'],
    ['#FFFF00', 'rgb(255, 255, 0)', 'rgb(0, 0, 0)'],
    ['#123456', 'rgb(18, 52, 86)', 'rgb(255, 255, 255)']
  ])('uses a contrasting foreground for validated custom color %s', (color, background, foreground) => {
    const wrapper = mount(GroupBadge, { props: { name: 'Group', identity: { label: 'Custom', color } } })
    const badge = wrapper.get<HTMLElement>('[data-group-identity]')
    expect(badge.element.style.backgroundColor).toBe(background)
    expect(badge.element.style.color).toBe(foreground)
  })

  it.each(['red; background-image:url(https://evil.test)', 'var(--untrusted)', '#fff', '#fffffff', 'constructor'])('rejects unapproved CSS color %s', color => {
    const wrapper = mount(GroupBadge, { props: { name: 'Group', identity: { label: 'Custom', color } } })
    const badge = wrapper.get('[data-group-identity]')
    expect(badge.attributes('style')).toBeUndefined()
    expect(badge.classes()).toContain('bg-teal-700')
    expect(wrapper.html()).not.toContain(color)
  })

  it('preserves custom and peak rates next to identity and description in options', () => {
    const wrapper = mount(GroupOptionItem, { props: {
      name: 'Priority group', platform: 'openai', identity: { label: '自营', color: 'blue' }, description: 'Account details',
      rateMultiplier: 1, userRateMultiplier: 0.35, peakRateEnabled: true, peakStart: '14:00', peakEnd: '18:00', peakRateMultiplier: 2, selected: true,
    } })
    expect(wrapper.get('[data-group-identity]').text()).toBe('自营')
    expect(wrapper.text()).toContain('Priority group')
    expect(wrapper.text()).toContain('Account details')
    expect(wrapper.get('.line-through').text()).toBe('1x')
    expect(wrapper.text()).toContain('0.35x')
    expect(wrapper.text()).toContain('14:00-18:00 ×2 (UTC+08:00)')
  })

  it('keeps subscription validity semantics when identity is present', () => {
    const wrapper = mount(GroupBadge, { props: { name: 'Monthly', identity: { label: 'Partner', color: 'violet' }, subscriptionType: 'subscription', daysRemaining: 0, rateMultiplier: 0.5 } })
    expect(wrapper.get('[data-group-identity]').text()).toBe('Partner')
    expect(wrapper.text()).toContain('admin.users.expired')
    expect(wrapper.text()).not.toContain('0.5x')
  })
})
