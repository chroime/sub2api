import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import TransportSelect from './TransportSelect.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
describe('inference protocol logos', () => {
  it('renders the selected vendor logo and logos for all dropdown options', async () => {
    const wrapper = mount(TransportSelect, { props: { modelValue: 'openai' }, attachTo: document.body })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('openai')
    await wrapper.get('button').trigger('click')
    const options = document.querySelectorAll('[role=option]')
    expect(options).toHaveLength(3)
    for (const option of options) expect(option.querySelector('svg')).not.toBeNull()
    expect([...options].map(option => option.textContent?.trim())).toEqual(['OpenAI', 'Anthropic', 'Gemini'])
    ;(options[1] as HTMLElement).click()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual(['anthropic'])
    wrapper.unmount()
  })
})
