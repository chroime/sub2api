import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import GovernanceHistory from './GovernanceHistory.vue'
import type { Binding } from '@/api/admin/upstream-governance'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const binding: Binding = { id: 5, site_id: 1, remote_group_id: 'upstream-7', platform: 'openai', local_group_id: 3, local_group_ids: [3, 4], account_id: 8, account_name: 'upstream.example--0.2', probe_enabled: false, probe_model: '', probe_interval_minutes: 30 }

describe('imported binding names', () => {
  it('names confirmed key loss and local account protection in administrator history', () => {
    const wrapper = mount(GovernanceHistory, { props: { mode: 'history', bindings: [], events: { items: [
      { id: 1, site_id: 1, kind: 'key_missing_confirmed', resource: 'managed-marker', before: 'suspected_missing', after: 'confirmed_missing', acknowledged: false, created_at: '2026-09-30T08:00:00Z' },
      { id: 2, site_id: 1, kind: 'key_account_paused', resource: 'managed-marker', before: '', after: 'upstream_key_missing', acknowledged: false, created_at: '2026-09-30T08:01:00Z' },
    ], total: 2, page: 1, page_size: 20, pages: 1 }, checks: null } })
    expect(wrapper.text()).toContain('governance.keyMissingConfirmed')
    expect(wrapper.text()).toContain('governance.keyAccountPaused')
    wrapper.unmount()
  })
  it('labels automatic reauthorization events without exposing credential details', () => {
    const wrapper = mount(GovernanceHistory, {
      props: {
        mode: 'history',
        bindings: [],
        events: {
          items: [{ id: 9, site_id: 1, kind: 'auto_reauthorization_required', resource: '', before: '', after: 'verification_required', acknowledged: false, created_at: '2026-09-28T09:00:00Z' }],
          total: 1,
          page: 1,
          page_size: 20,
          pages: 1,
        },
        checks: null,
      },
    })
    expect(wrapper.text()).toContain('governance.autoReauthorizationRequired')
    expect(wrapper.text()).toContain('verification_required')
    expect(wrapper.text()).not.toContain('password')
    wrapper.unmount()
  })

  it('shows current group and account names alongside IDs and refreshes renamed groups', async () => {
    const wrapper = mount(GovernanceHistory, { props: { mode: 'bindings', bindings: [binding], remoteGroups: [{ id: 'upstream-7', name: 'Codex discount' }], groups: [{ id: 3, name: 'Local OpenAI' }, { id: 4, name: 'Local mixed' }], events: null, checks: null } })
    expect(wrapper.get('[data-test=binding-upstream]').text()).toContain('Codex discount')
    expect(wrapper.get('[data-test=binding-upstream]').text()).toContain('#upstream-7')
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('upstream.example--0.2')
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('#8')
    expect(wrapper.get('[data-test=binding-targets]').text()).toContain('Local OpenAI、Local mixed')
    await wrapper.setProps({ remoteGroups: [{ id: 'upstream-7', name: 'Renamed upstream group' }], bindings: [{ ...binding, account_name: 'Custom account name' }] })
    expect(wrapper.get('[data-test=binding-upstream]').text()).toContain('Renamed upstream group')
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('Custom account name')
    await wrapper.findAll('button')[0]!.trigger('click')
    expect(wrapper.emitted('configure')?.[0]).toEqual([{ ...binding, account_name: 'Custom account name' }, 'check'])
    wrapper.unmount()
  })

  it('keeps IDs for missing names and preserves pending-import behavior', () => {
    const wrapper = mount(GovernanceHistory, { props: { mode: 'bindings', bindings: [{ ...binding, account_name: undefined }, { ...binding, id: 6, account_id: 0, account_name: undefined }], events: null, checks: null } })
    expect(wrapper.findAll('[data-test=binding-upstream]')[0]!.text()).toContain('governance.remoteGroup')
    expect(wrapper.findAll('[data-test=binding-upstream]')[0]!.text()).toContain('#upstream-7')
    expect(wrapper.findAll('[data-test=binding-account]')[0]!.text()).toContain('governance.accountNameUnavailable')
    expect(wrapper.findAll('[data-test=binding-account]')[0]!.text()).toContain('#8')
    expect(wrapper.findAll('[data-test=binding-targets]')[0]!.text()).toContain('#3、#4')
    const pending = wrapper.findAll('article')[1]!
    expect(pending.text()).toContain('governance.pendingImport')
    expect(pending.findAll('button').every(button => button.attributes('disabled') !== undefined)).toBe(true)
    wrapper.unmount()
  })

  it('retains a deleted account name and disables checks for that historical binding', async () => {
    const wrapper = mount(GovernanceHistory, { props: { mode: 'bindings', bindings: [{ ...binding, account_deleted: true }], events: null, checks: null } })
    expect(wrapper.get('[data-test=binding-account]').text()).toContain(binding.account_name)
    expect(wrapper.get('[data-test=binding-account]').text()).toContain('governance.accountDeleted')
    const buttons = wrapper.findAll('button')
    expect(buttons.every(button => button.attributes('disabled') !== undefined)).toBe(true)
    await buttons[0]!.trigger('click')
    expect(wrapper.emitted('configure')).toBeUndefined()
    wrapper.unmount()
  })
})
