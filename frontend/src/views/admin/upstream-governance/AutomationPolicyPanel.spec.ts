import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AutomationPolicyPanel from './AutomationPolicyPanel.vue'
import api from '@/api/admin/upstream-governance'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/upstream-governance', () => ({ default: { saveAutomation: vi.fn() } }))
enableAutoUnmount(afterEach)
const global = { stubs: { teleport: true, transition: false } }
async function opened(props: { siteId: number; configuration: typeof configuration }) {
  const wrapper = mount(AutomationPolicyPanel, { props, global })
  await wrapper.get('[data-test=automation-card-open]').trigger('click')
  return wrapper
}
const configuration = { version: 4, policy: { enabled: false, sync_rate: true, sync_name: true, pause_missing: true, restore_returned: true, missing_confirmations: 2, max_rate_increase_percent: 20 } }
describe('automation policy', () => {
  it('shows the persisted opt-in state and saves the reviewed policy with its version', async () => {
    vi.mocked(api.saveAutomation).mockResolvedValue({ ...configuration, version: 5, policy: { ...configuration.policy, enabled: true } })
    const wrapper = await opened({ siteId: 1, configuration })
    expect((wrapper.get('[data-test=automation-enabled]').element as HTMLInputElement).checked).toBe(false)
    expect(api.saveAutomation).not.toHaveBeenCalled()
    await wrapper.get('[data-test=automation-enabled]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.saveAutomation).toHaveBeenCalledWith(1, { version: 4, policy: { ...configuration.policy, enabled: true } })
    expect(wrapper.emitted('saved')?.[0]?.[0]).toMatchObject({ version: 5 })
    expect(configuration.policy.enabled).toBe(false)
    await wrapper.setProps({ configuration: wrapper.emitted('saved')![0]![0] as typeof configuration })
    expect(wrapper.text()).toContain('governance.automationSaved')
    // A successful save closes the dialog and the card shows the new state.
    expect(wrapper.find('[data-test=automation-enabled]').exists()).toBe(false)
    expect(wrapper.get('[data-test=automation-card-status]').text()).toBe('governance.automationOn')
  })
  it('keeps server conflicts visible and replaces drafts when the site changes', async () => {
    vi.mocked(api.saveAutomation).mockRejectedValue({ status: 409 })
    const wrapper = await opened({ siteId: 1, configuration })
    await wrapper.get('[data-test=automation-enabled]').setValue(true)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('governance.stale')
    await wrapper.setProps({ siteId: 2, configuration: { ...configuration, version: 1 } })
    expect(wrapper.find('[data-test=automation-enabled]').exists()).toBe(false)
    await wrapper.get('[data-test=automation-card-open]').trigger('click')
    expect((wrapper.get('[data-test=automation-enabled]').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.text()).not.toContain('governance.stale')
  })
})
