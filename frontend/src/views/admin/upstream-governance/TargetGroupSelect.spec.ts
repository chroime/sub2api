import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TargetGroupSelect from './TargetGroupSelect.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const groups = [
  { id: 1, name: 'GPT group', platform: 'openai', rate_multiplier: 0.8 },
  { id: 2, name: 'DeepSeek group', platform: 'deepseek', rate_multiplier: 2 },
  { id: 3, name: 'Mixed group', platform: 'composite', rate_multiplier: 1 },
]
describe('target group selection', () => {
  it('shows the selected vendor and all existing group logos, keeps incompatible groups visible but disabled', async () => {
    const wrapper = mount(TargetGroupSelect, { props: { modelValue: 1, groups, transport: 'openai' }, attachTo: document.body })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('openai')
    expect(wrapper.text()).toContain('0.8×')
    await wrapper.get('button').trigger('click')
    const options = [...document.querySelectorAll<HTMLElement>('[role=option]')]
    expect(options).toHaveLength(3)
    expect(options.every(option => option.querySelector('svg'))).toBe(true)
    expect(options[1]!.getAttribute('aria-disabled')).toBe('true')
    expect(options[1]!.textContent).toContain('governance.targetProtocolMismatch')
    options[1]!.click()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(options[2]!.textContent).toContain('governance.compositeGroup')
    options[2]!.click()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([3])
    wrapper.unmount()
  })
  it('allows every existing vendor in bulk assignment and clears to an unresolved ID', async () => {
    const wrapper = mount(TargetGroupSelect, { props: { modelValue: 2, groups }, attachTo: document.body })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('deepseek')
    await wrapper.get('button').trigger('click')
    expect([...document.querySelectorAll('[role=option]')].every(option => option.getAttribute('aria-disabled') === 'false')).toBe(true)
    await wrapper.get('[aria-label="Clear selection"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([0])
    wrapper.unmount()
  })
})
