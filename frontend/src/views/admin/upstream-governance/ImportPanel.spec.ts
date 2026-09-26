import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi } from 'vitest'
import ImportPanel from './ImportPanel.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('@/api/admin/upstream-governance', () => ({
  default: { preview: vi.fn(), apply: vi.fn() },
}))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
const props = {
  siteId: 1,
  groups: [{ id: 3, name: 'Local', platform: 'openai', rate_multiplier: 2 }],
  snapshot: {
    id: 1,
    site_id: 1,
    site_version: 1,
    created_at: 'now',
    catalog: {
      groups: [
        {
          id: 'r',
          name: 'Remote',
          platform: 'openai',
          rate_multiplier: 1,
          user_rate_multiplier: null,
          resolved_rate_multiplier: 1,
          models: ['fixture-model'],
          prices: [],
          source: 'user',
          peak_rate_enabled: false,
        },
      ],
      channels: [],
      warnings: [],
    },
  },
}
describe('import confirmation', () => {
  it('allows mixed local groups for an explicitly chosen transport', async () => {
    const wrapper = mount(ImportPanel, { props: { ...props, groups: [{ id: 9, name: 'Mixed destination', platform: 'composite', rate_multiplier: 2 }] } })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    expect(wrapper.get('[data-test=target]').text()).toContain('Mixed destination')
  })
  it('requires selection, mapping, preview and explicit apply; displays partial result', async () => {
    vi.mocked(api.preview).mockResolvedValue({
      id: 'p',
      rows: [],
      expires_at: '2099-01-01',
      site_id: 1,
      site_version: 1,
      snapshot_id: 1,
      created_at: 'now',
    })
    vi.mocked(api.apply).mockResolvedValue({
      preview_id: 'p',
      items: [
        {
          remote_group_id: 'r',
          platform: 'openai',
          status: 'failed',
          error: 'fixture failure',
        },
      ],
    })
    const wrapper = mount(ImportPanel, { props })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    await wrapper.get('[data-test=target]').setValue(3)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.preview).toHaveBeenCalled()
    expect(api.apply).not.toHaveBeenCalled()
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(api.apply).toHaveBeenCalledWith(1, 'p')
    expect(wrapper.text()).toContain('fixture failure')
  })
  it('invalidates stale previews and requires another preview', async () => {
    vi.mocked(api.apply).mockRejectedValue({ status: 409 })
    const wrapper = mount(ImportPanel, { props })
    await wrapper.get('[data-test=select]').setValue(true)
    await wrapper.get('[data-test=platform]').setValue('openai')
    await wrapper.get('[data-test=target]').setValue(3)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    await wrapper.get('[data-test=apply]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.stale')
    expect(wrapper.find('[data-test=apply]').exists()).toBe(false)
  })
})
