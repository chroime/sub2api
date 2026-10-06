import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TransportSelect from './TransportSelect.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { governanceProviders } from './providers'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('inference protocol logos', () => {
  it('renders the selected vendor logo and logos for all dropdown options', async () => {
    const wrapper = mount(TransportSelect, { props: { modelValue: 'openai' }, attachTo: document.body })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('openai')
    await wrapper.get('button').trigger('click')
    const options = document.querySelectorAll('[role=option]')
    expect(options).toHaveLength(10)
    for (const option of options) expect(option.querySelector('svg')).not.toBeNull()
    expect([...options].map(option => option.textContent?.trim())).toEqual(governanceProviders.map(provider => provider.label))
    ;(options[1] as HTMLElement).click()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['anthropic'])
    wrapper.unmount()
  })
  it('shows all platforms but disables incompatible protocols and the NewAPI Antigravity route', async () => {
    const wrapper = mount(TransportSelect, { props: { modelValue: '', sitePlatform: 'newapi', remotePlatform: 'unknown' }, attachTo: document.body })
    await wrapper.get('button').trigger('click')
    let options = [...document.querySelectorAll<HTMLElement>('[role=option]')]
    expect(options.filter(option => option.getAttribute('aria-disabled') === 'true')).toHaveLength(1)
    const antigravity = options[3]!
    expect(antigravity.textContent).toContain('governance.antigravityNeedsSub2API')
    antigravity.click()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.setProps({ sitePlatform: 'sub2api', remotePlatform: 'grok' })
    options = [...document.querySelectorAll<HTMLElement>('[role=option]')]
    expect(options.filter(option => option.getAttribute('aria-disabled') === 'false').map(option => option.textContent?.trim())).toEqual(['OpenAI', 'Grok / xAI'])
    expect(options[1]!.textContent).toContain('governance.protocolMismatch')
    wrapper.unmount()
  })
  it('shows all ten vendor logos in the protocol filter and can clear the filter', async () => {
    const wrapper = mount(TransportSelect, { props: { modelValue: 'deepseek', allowAll: true }, attachTo: document.body })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('deepseek')
    await wrapper.get('button').trigger('click')
    const options = [...document.querySelectorAll<HTMLElement>('[role=option]')]
    expect(options).toHaveLength(11)
    expect(options.slice(1).every(option => option.querySelector('svg'))).toBe(true)
    options[0]!.click()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([''])
    wrapper.unmount()
  })
})
