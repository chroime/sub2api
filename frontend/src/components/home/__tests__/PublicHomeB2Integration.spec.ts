import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PublicHomeB2Integration from '../PublicHomeB2Integration.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
vi.mock('@/i18n', () => ({ i18n: { global: { t: (key: string) => key } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn(), showError: vi.fn() }) }))

const messages = {
  home: { public: { b2: {
    integrationTitle: '1 minute integration',
    integrationDescription: 'Use the OpenAI SDK with a new Base URL.',
    fullDocs: 'Read the documentation',
    codeLanguage: 'Code language',
    copyCode: 'Copy code',
    copied: 'Copied',
    copyFailed: 'Copy failed',
  } } },
}

const wrappers: ReturnType<typeof mount>[] = []
let resize: () => void

function mountIntegration(props: Partial<{ baseUrl: string; model: string; docsHref: string }> = {}) {
  const wrapper = mount(PublicHomeB2Integration, {
    attachTo: document.body,
    props: { baseUrl: 'https://api.example.test/v1', model: 'gpt-4o-mini', docsHref: '/docs', ...props },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: messages } })] },
  })
  wrappers.push(wrapper)
  return wrapper
}

function output(wrapper: ReturnType<typeof mount>) {
  return wrapper.get('#code-output').element.textContent || ''
}

function fitMetrics(wrapper: ReturnType<typeof mount>, width: number, contentWidth: number, height = 262, contentHeight = 168) {
  const panel = wrapper.get<HTMLElement>('#code-panel').element
  const pre = wrapper.get<HTMLElement>('#code-output').element
  panel.style.padding = '16px 20px'
  Object.defineProperties(panel, {
    clientWidth: { configurable: true, value: width },
    clientHeight: { configurable: true, value: height },
  })
  Object.defineProperties(pre, {
    scrollWidth: { configurable: true, value: contentWidth },
    scrollHeight: { configurable: true, value: contentHeight },
  })
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true)
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: () => void) { resize = callback }
    observe() {}
    disconnect() {}
  })
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockResolvedValue(undefined) } })
  Object.defineProperty(document, 'execCommand', { configurable: true, value: vi.fn().mockReturnValue(false) })
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('PublicHomeB2Integration', () => {
  it('renders the compact B2 integration shell with a single icon-only copy control', () => {
    const wrapper = mountIntegration()
    expect(wrapper.get('.integration').attributes('aria-labelledby')).toBe('integration-title')
    expect(wrapper.get('#integration-title').text()).toBe('1 minute integration')
    expect(wrapper.get('.integration-copy a').attributes('href')).toBe('/docs')
    expect(wrapper.get('.code-tool .code-toolbar .code-tabs').attributes('role')).toBe('tablist')
    expect(wrapper.findAll('[role="tab"]').map(tab => tab.text())).toEqual(['cURL', 'JavaScript', 'Python'])
    expect(wrapper.get('#copy-code').text()).toBe('')
    expect(wrapper.get('#copy-code').attributes('title')).toBe('Copy code')
    expect(wrapper.get('#copy-code').attributes('aria-label')).toBe('Copy code')
    expect(wrapper.get('#copy-code svg').exists()).toBe(true)
    expect(wrapper.find('.integration-line-number, .integration-benefit-icon, ul, .integration-code-scroll').exists()).toBe(false)
    expect(wrapper.findAll('pre')).toHaveLength(1)
    expect(output(wrapper).split('\n')).toHaveLength(8)
    expect(output(wrapper)).toContain("BASE_URL='https://api.example.test/v1'")
    expect(output(wrapper)).toContain('"model":"gpt-4o-mini"')
  })

  it('switches snippets and keeps one selected, focusable tab linked to the panel', async () => {
    const wrapper = mountIntegration()
    await wrapper.get('#tab-javascript').trigger('click')
    expect(output(wrapper).split('\n')).toHaveLength(11)
    expect(output(wrapper)).toContain('import OpenAI from "openai";')
    expect(output(wrapper)).toContain('baseURL: "https://api.example.test/v1"')
    expect(wrapper.get('#tab-javascript').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('#tab-javascript').attributes('tabindex')).toBe('0')
    expect(wrapper.get('#tab-curl').attributes('tabindex')).toBe('-1')
    expect(wrapper.get('#code-panel').attributes('aria-labelledby')).toBe('tab-javascript')
    expect(wrapper.findAll('[role="tab"]').every(tab => tab.attributes('aria-controls') === 'code-panel')).toBe(true)
    await wrapper.get('#tab-python').trigger('click')
    expect(output(wrapper).split('\n')).toHaveLength(11)
    expect(output(wrapper)).toMatch(/^import os\nfrom openai import OpenAI/)
    expect(output(wrapper)).toContain('os.environ["API_KEY"]')
    vi.advanceTimersByTime(18000)
    await nextTick()
    expect(wrapper.get('#tab-python').attributes('aria-selected')).toBe('true')
  })

  it('supports wrapped arrow navigation plus Home and End with keyboard focus', async () => {
    const wrapper = mountIntegration()
    await wrapper.get('#tab-curl').trigger('keydown', { key: 'ArrowLeft' })
    expect(document.activeElement?.id).toBe('tab-python')
    expect(wrapper.get('#tab-python').attributes('aria-selected')).toBe('true')
    await wrapper.get('#tab-python').trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement?.id).toBe('tab-curl')
    await wrapper.get('#tab-curl').trigger('keydown', { key: 'End' })
    expect(document.activeElement?.id).toBe('tab-python')
    await wrapper.get('#tab-python').trigger('keydown', { key: 'Home' })
    expect(document.activeElement?.id).toBe('tab-curl')
  })

  it('updates the active code from dynamic props without losing tab selection', async () => {
    const wrapper = mountIntegration()
    await wrapper.get('#tab-javascript').trigger('click')
    await wrapper.setProps({ baseUrl: 'https://next.example.test/api/v1', model: 'next-model', docsHref: 'https://docs.example.test' })
    expect(wrapper.get('#tab-javascript').attributes('aria-selected')).toBe('true')
    expect(output(wrapper)).toContain('baseURL: "https://next.example.test/api/v1"')
    expect(output(wrapper)).toContain('model: "next-model"')
    expect(output(wrapper)).not.toContain('gpt-4o-mini')
    expect(wrapper.get('.integration-copy a').attributes('href')).toBe('https://docs.example.test')
  })

  it('quotes shell substitutions and apostrophes in both the base URL and JSON body', () => {
    const wrapper = mountIntegration({ baseUrl: "https://api.example.test/o'$(date)/v1", model: "model'$(date)" })
    expect(output(wrapper)).toContain(String.raw`BASE_URL='https://api.example.test/o'"'"'$(date)/v1'`)
    expect(output(wrapper)).toContain(String.raw`-d '{"model":"model'"'"'$(date)",`)
    expect(output(wrapper)).toContain('curl "$BASE_URL/chat/completions"')
  })

  it('serializes quotes, backslashes and line breaks as safe JS and Python string literals', async () => {
    const baseUrl = 'https://api.example.test/"\\\n</pre>'
    const model = 'model"\\\n<script>test</script>'
    const wrapper = mountIntegration({ baseUrl, model })
    await wrapper.get('#tab-javascript').trigger('click')
    const js = output(wrapper)
    expect(JSON.parse(js.match(/baseURL: (.+)\n/)![1]!)).toBe(baseUrl)
    expect(JSON.parse(js.match(/model: (.+),\n/)![1]!)).toBe(model)
    await wrapper.get('#tab-python').trigger('click')
    const python = output(wrapper)
    expect(JSON.parse(python.match(/base_url=(.+)\n/)![1]!)).toBe(baseUrl)
    expect(JSON.parse(python.match(/model=(.+),\n/)![1]!)).toBe(model)
    expect(wrapper.find('script').exists()).toBe(false)
    expect(wrapper.findAll('pre')).toHaveLength(1)
  })

  it('copies the active full snippet and shows icon tooltip feedback before resetting', async () => {
    const wrapper = mountIntegration()
    await wrapper.get('#tab-python').trigger('click')
    const code = output(wrapper)
    await wrapper.get('#copy-code').trigger('click')
    await flushPromises()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(code)
    expect(wrapper.get('#copy-code').attributes('aria-label')).toBe('Copied')
    expect(wrapper.get('#copy-code').attributes('title')).toBe('Copied')
    expect(wrapper.get('#copy-code').text()).toBe('')
    expect(wrapper.get('[role="status"]').text()).toBe('Copied')
    vi.advanceTimersByTime(2000)
    await nextTick()
    expect(wrapper.get('#copy-code').attributes('aria-label')).toBe('Copy code')
  })

  it('reports a failed copy without rejecting the click handler', async () => {
    vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('Denied'))
    const wrapper = mountIntegration()
    await wrapper.get('#copy-code').trigger('click')
    await flushPromises()
    expect(wrapper.get('#copy-code').attributes('aria-label')).toBe('Copy failed')
    expect(wrapper.get('[role="status"]').text()).toBe('Copy failed')
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('keeps focus on the copy control after the legacy clipboard fallback', async () => {
    vi.mocked(navigator.clipboard.writeText).mockRejectedValue(new Error('Denied'))
    vi.mocked(document.execCommand).mockReturnValue(true)
    const wrapper = mountIntegration()
    wrapper.get<HTMLButtonElement>('#copy-code').element.focus()
    await wrapper.get('#copy-code').trigger('click')
    await flushPromises()
    expect(wrapper.get('#copy-code').attributes('aria-label')).toBe('Copied')
    expect(document.activeElement?.id).toBe('copy-code')
  })

  it('does not apply a stale successful copy state after changing tabs', async () => {
    let completeCopy: () => void = () => {}
    vi.mocked(navigator.clipboard.writeText).mockImplementation(() => new Promise<void>(resolve => { completeCopy = resolve }))
    const wrapper = mountIntegration()
    await wrapper.get('#copy-code').trigger('click')
    await wrapper.get('#tab-javascript').trigger('click')
    completeCopy()
    await flushPromises()
    expect(wrapper.get('#copy-code').attributes('aria-label')).toBe('Copy code')
  })

  it('fits long unmodified code against the measured container width and expands again on resize', async () => {
    const longBaseUrl = `https://api.example.test/${'long-path-'.repeat(35)}/v1`
    const wrapper = mountIntegration({ baseUrl: longBaseUrl })
    fitMetrics(wrapper, 300, 1040)
    resize()
    await nextTick()
    expect(wrapper.get<HTMLElement>('#code-output').element.style.transform).toBe('scale(0.25)')
    expect(output(wrapper)).toContain(longBaseUrl)
    expect(wrapper.get<HTMLElement>('#code-output').element.style.fontSize).toBe('')
    fitMetrics(wrapper, 1200, 1040)
    resize()
    await nextTick()
    expect(wrapper.get<HTMLElement>('#code-output').element.style.transform).toBe('scale(1)')
  })

  it('also fits code height and remeasures after the active snippet changes', async () => {
    const wrapper = mountIntegration()
    fitMetrics(wrapper, 600, 300, 234, 404)
    await wrapper.get('#tab-python').trigger('click')
    await nextTick()
    expect(wrapper.get<HTMLElement>('#code-output').element.style.transform).toBe('scale(0.5)')
    expect(output(wrapper).split('\n')).toHaveLength(11)
  })

  it('never rounds an extremely long snippet down to an invisible zero scale', async () => {
    const wrapper = mountIntegration()
    fitMetrics(wrapper, 300, 10000000)
    resize()
    await nextTick()
    const transform = wrapper.get<HTMLElement>('#code-output').element.style.transform
    const scale = Number(transform.slice(6, -1))
    expect(scale).toBeGreaterThan(0)
    expect(scale * 10000000).toBeLessThanOrEqual(260)
  })
})
