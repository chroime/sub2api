import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ModelRunTable from './ModelRunTable.vue'
import { modelRun } from './__tests__/model-fixtures'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
enableAutoUnmount(afterEach)
describe('candy review display', () => {
  it.each(['numeric_correct', 'numeric_incorrect'])('shows an automatic %s without requiring human review', verdict => {
    const run = modelRun(); run.result!.candy_verdict = verdict
    const wrapper = mount(ModelRunTable, { props: { items: [run], loading: false, disabled: false, filtered: false, batchId: '', targetLabel: () => 'Group' } })
    expect(wrapper.text()).toContain(`governance.modelMonitoring.review_${verdict === 'numeric_correct' ? 'pass' : `auto_${verdict}`}`)
    expect(wrapper.text()).not.toContain('governance.modelMonitoring.review_pending')
  })
  it('treats numerical correctness as pass even with an earlier human failure', () => {
    const wrapper = mount(ModelRunTable, { props: { items: [modelRun({ review: 'fail' })], loading: false, disabled: false, filtered: false, batchId: '', targetLabel: () => 'Group' } })
    expect(wrapper.text()).toContain('governance.modelMonitoring.review_pass')
    expect(wrapper.text()).not.toContain('review_auto_')
  })
  it('keeps ambiguous and incomplete answers pending', () => {
    const run = modelRun(); run.result!.candy_verdict = 'needs_review'
    const wrapper = mount(ModelRunTable, { props: { items: [run], loading: false, disabled: false, filtered: false, batchId: '', targetLabel: () => 'Group' } })
    expect(wrapper.text()).toContain('governance.modelMonitoring.review_pending')
  })
})
