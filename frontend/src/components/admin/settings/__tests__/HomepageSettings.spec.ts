import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import HomepageSettings from '../HomepageSettings.vue'
import Select from '@/components/common/Select.vue'
import { readHomepagePreview } from '@/utils/homepagePreview'

describe('homepage settings', () => {
  afterEach(() => { vi.restoreAllMocks(); localStorage.clear() })

  it('offers four modes and retains custom content while editing another mode', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [] })
    const wrapper = mount(HomepageSettings, { props: { modelValue: 'brand', content: '<h1>Retained</h1>', draft: {} }, global: { plugins: [router] } })
    expect(wrapper.findComponent(Select).props('options').map((option: { value: string }) => option.value)).toEqual(['classic', 'brand', 'compact', 'custom'])
    expect(wrapper.find('textarea').exists()).toBe(false)
    await wrapper.setProps({ modelValue: 'custom' })
    expect(wrapper.get('textarea').element.value).toBe('<h1>Retained</h1>')
    await wrapper.get('textarea').setValue('<h1>Edited</h1>')
    expect(wrapper.emitted('update:content')?.[0]).toEqual(['<h1>Edited</h1>'])
    wrapper.unmount()
  })

  it('opens an administrator preview of unsaved public fields without submitting the form', async () => {
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/admin/settings/homepage-preview', name: 'HomepagePreview', component: { template: '<div />' } }] })
    const open = vi.spyOn(window, 'open').mockReturnValue(null)
    const wrapper = mount(HomepageSettings, { props: { modelValue: 'brand', content: '', draft: { site_name: 'Unsaved site', smtp_password: 'secret' } }, global: { plugins: [router] } })
    expect(wrapper.get('[data-testid="homepage-preview"]').attributes('type')).toBe('button')
    await wrapper.get('[data-testid="homepage-preview"]').trigger('click')
    expect(open).toHaveBeenCalledOnce()
    const href = new URL(String(open.mock.calls[0][0]), 'http://localhost')
    expect(href.pathname).toBe('/admin/settings/homepage-preview')
    expect(readHomepagePreview(href.searchParams.get('draft'))).toEqual({ homepage_template: 'brand', home_content: '', site_name: 'Unsaved site' })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })
})
