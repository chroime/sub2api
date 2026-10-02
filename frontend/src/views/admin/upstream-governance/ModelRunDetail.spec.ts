import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ModelRunDetail from './ModelRunDetail.vue'
import Select from '@/components/common/Select.vue'
import api, { type ModelRun } from '@/api/admin/upstream-model-monitoring'
import { modelConfig, modelRun } from './__tests__/model-fixtures'
import { modelHTMLArtifacts, modelPreviewDocument } from './model-preview'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('@/api/admin/upstream-model-monitoring', () => ({ default: { run: vi.fn(), review: vi.fn() } }))
enableAutoUnmount(afterEach)
const setup = () => mount(ModelRunDetail, { props: { siteId: 1, runId: 'run-medium' }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } })
describe('model evidence and isolated artifact preview', () => {
  beforeEach(() => { vi.resetAllMocks(); vi.mocked(api.run).mockResolvedValue(modelRun()) })
  it('preserves unknown usage and TTFT instead of showing zero and separates numerical answer from review', async () => {
    const wrapper = setup(); await flushPromises()
    expect(wrapper.get('[data-test=model-ttft]').text()).toBe('governance.modelMonitoring.unknown')
    expect(wrapper.get('[data-test=model-candy-reference]').text()).toContain('governance.modelMonitoring.candyReference')
    expect(wrapper.get('[data-test=model-candy-reference]').text()).toContain('21')
    expect(wrapper.get('[data-test=model-review]').text()).toContain('governance.modelMonitoring.review_pending')
    await wrapper.findAll('nav button').find(button => button.text() === 'governance.modelMonitoring.tokens')!.trigger('click')
    expect(wrapper.get('[data-test=declared-input]').text()).toBe('governance.modelMonitoring.unknown')
    expect(wrapper.get('[data-test=declared-output]').text()).toBe('governance.modelMonitoring.unknown')
    expect(wrapper.get('[data-test=model-token-audit]').text()).toContain('input-sha256')
  })
  it('keeps generated HTML out of the administrator DOM and only plays it in an opaque sandbox', async () => {
    const html = '<!doctype html><html><script>window.parent.testCanary="bad"</script><body><img src="https://example.test/a"><svg id="pelican"></svg></body></html>'
    const run = modelRun(); run.request = { config: modelConfig, template: 'pelican', effort: 'medium', sample: 1 }; run.result = { ...run.result!, response_text: html, html }
    vi.mocked(api.run).mockResolvedValue(run)
    const wrapper = setup(); await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.find('script').exists()).toBe(false); expect(wrapper.find('#pelican').exists()).toBe(false)
    expect(wrapper.get('[data-test=model-original-response]').text()).toBe(html)
    await wrapper.get('[data-test=model-preview-play]').trigger('click')
    const frame = wrapper.get('iframe')
    expect(frame.attributes('sandbox')).toBe('allow-scripts')
    expect(frame.attributes('sandbox')).not.toContain('allow-same-origin')
    expect(frame.attributes('srcdoc')).toContain("default-src 'none'")
    expect(frame.attributes('srcdoc')).toContain("connect-src 'none'")
    expect(frame.attributes('srcdoc')).toContain("form-action 'none'")
    expect(frame.attributes('referrerpolicy')).toBe('no-referrer')
    expect(frame.attributes('srcdoc')).toContain(html)
  })
  it('retains exact HTML block bytes and places preview restrictions before untrusted content', () => {
    const body = '<svg>\n  <text>原文</text>\n</svg>'
    const source = `Explanation\n\x60\x60\x60html\n${body}\n\x60\x60\x60\n`
    expect(modelHTMLArtifacts(source)).toEqual([{ source: 'block:1', html: body }])
    const preview = modelPreviewDocument(body)
    expect(preview.indexOf('Content-Security-Policy')).toBeLessThan(preview.indexOf('<svg>'))
    expect(preview.endsWith(body)).toBe(true)
    expect(modelHTMLArtifacts('<script>alert(1)</script>')).toEqual([])
  })
  it('saves explicit human review and does not equate the numerical verdict with proof validation', async () => {
    vi.mocked(api.review).mockResolvedValue({ ...modelRun(), review: 'pass', review_note: 'Verified upper and lower bound', reviewer_id: 7, reviewed_at: '2026-09-27T13:00:00Z', review_version: 1 })
    const wrapper = setup(); await flushPromises()
    wrapper.getComponent(Select).vm.$emit('update:modelValue', 'pass')
    await wrapper.get('[data-test=model-review-note]').setValue('Verified upper and lower bound')
    await wrapper.get('[data-test=model-review-save]').trigger('click'); await flushPromises()
    expect(api.review).toHaveBeenCalledWith(1, 'run-medium', 'pass', 'Verified upper and lower bound')
    expect(wrapper.emitted('reviewed')?.[0]?.[0]).toMatchObject({ review: 'pass', reviewer_id: 7 })
  })
  it('does not display an earlier site or run response after navigation', async () => {
    let finish!: (run: ModelRun) => void
    vi.mocked(api.run).mockReturnValueOnce(new Promise(resolve => { finish = resolve }))
    const wrapper = setup()
    vi.mocked(api.run).mockResolvedValue({ ...modelRun(), id: 'run-new', result: { ...modelRun().result!, response_text: 'new-site-answer' } })
    await wrapper.setProps({ siteId: 2, runId: 'run-new' }); await flushPromises()
    finish({ ...modelRun(), result: { ...modelRun().result!, response_text: 'old-site-answer' } }); await flushPromises()
    expect(wrapper.get('[data-test=model-original-response]').text()).toBe('new-site-answer')
  })
})
