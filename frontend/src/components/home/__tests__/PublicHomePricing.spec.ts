import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterAll, describe, expect, it, vi } from 'vitest'
import PublicHomePricing from '../PublicHomePricing.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())

const rows = [{ platform: 'openai', model: 'gpt-5.6-sol', inputPrice: 1, outputPrice: 2, cacheReadPrice: null, groupCount: 1, billingMode: 'token' }]

function mountPricing() {
  return mount(PublicHomePricing, {
    props: { rows, status: 'ready' },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en, zh } })], stubs: { Icon: { template: '<span />' } } },
  })
}

describe('PublicHomePricing', () => {
  it('keeps filters visible and resets a no-match query', async () => {
    const wrapper = mountPricing()
    const input = wrapper.get('input[type="search"]')
    await input.setValue('does-not-exist')
    expect(wrapper.text()).toContain('No models match these filters')
    expect(wrapper.get('input[type="search"]').exists()).toBe(true)
    await wrapper.get('button').trigger('click')
    expect(wrapper.text()).toContain('gpt-5.6-sol')
  })
})
