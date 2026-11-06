import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GovernanceHistory from './GovernanceHistory.vue'
import type { Binding } from '@/api/admin/upstream-governance'

vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, params?: Record<string, unknown>) => params?.group ? `${key}:${String(params.group)}` : key,
}) }))

const binding: Binding = { id: 5, site_id: 1, remote_group_id: 'upstream-7', platform: 'openai', local_group_id: 3, local_group_ids: [3, 4], account_id: 8, account_name: 'upstream.example--0.2', probe_enabled: false, probe_model: '', probe_interval_minutes: 30 }

describe('imported binding names', () => {
  it('shows current group and account names alongside IDs and refreshes renamed groups', async () => {
    const wrapper = mount(GovernanceHistory, { props: { bindings: [binding], remoteGroups: [{ id: 'upstream-7', name: 'Codex discount' }], groups: [{ id: 3, name: 'Local OpenAI' }, { id: 4, name: 'Local mixed' }] } })
    expect(wrapper.get('[data-test=binding-upstream]').text()).toContain('Codex discount')
    expect(wrapper.get('[data-test=binding-upstream]').text()).toContain('#upstream-7')
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('upstream.example--0.2')
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('#8')
    expect(wrapper.get('[data-test=binding-targets]').text()).toContain('Local OpenAI、Local mixed')
    await wrapper.setProps({ remoteGroups: [{ id: 'upstream-7', name: 'Renamed upstream group' }], bindings: [{ ...binding, account_name: 'Custom account name' }] })
    expect(wrapper.get('[data-test=binding-upstream]').text()).toContain('Renamed upstream group')
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('Custom account name')
    // Active checks are retired: the imported account list offers no probe actions.
    expect(wrapper.findAll('button')).toHaveLength(0)
    wrapper.unmount()
  })

  it('keeps IDs for missing names and preserves pending-import behavior', () => {
    const wrapper = mount(GovernanceHistory, { props: { bindings: [{ ...binding, account_name: undefined }, { ...binding, id: 6, account_id: 0, account_name: undefined }] } })
    expect(wrapper.findAll('[data-test=binding-upstream]')[0]!.text()).toContain('governance.remoteGroup')
    expect(wrapper.findAll('[data-test=binding-upstream]')[0]!.text()).toContain('#upstream-7')
    expect(wrapper.findAll('[data-test=binding-account]')[0]!.text()).toContain('governance.accountNameUnavailable')
    expect(wrapper.findAll('[data-test=binding-account]')[0]!.text()).toContain('#8')
    expect(wrapper.findAll('[data-test=binding-targets]')[0]!.text()).toContain('#3、#4')
    const pending = wrapper.findAll('article')[1]!
    expect(pending.text()).toContain('governance.pendingImport')
    wrapper.unmount()
  })

  it('retains a deleted account name for that historical binding', () => {
    const wrapper = mount(GovernanceHistory, { props: { bindings: [{ ...binding, account_deleted: true }] } })
    expect(wrapper.get('[data-test=binding-account]').text()).toContain(binding.account_name)
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('governance.accountDeleted')
    wrapper.unmount()
  })

})
