import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterAll, afterEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import en from '@/i18n/locales/en/governance'
import zh from '@/i18n/locales/zh/governance'
import SmartOperationsTabs from './SmartOperationsTabs.vue'

vi.hoisted(() => { vi.stubGlobal('__INTLIFY_JIT_COMPILATION__', true) })
afterAll(() => vi.unstubAllGlobals())
enableAutoUnmount(afterEach)
afterEach(() => vi.restoreAllMocks())

function renderTabs(modelValue: 'overview' | 'import' | 'models' | 'monitor' | 'history' = 'overview', disabled = false, locale = 'zh') {
  return mount(SmartOperationsTabs, {
    props: { modelValue, disabled },
    attachTo: document.body,
    global: {
      plugins: [createI18n({ legacy: false, locale, messages: { en: { governance: en }, zh: { governance: zh } } })]
    }
  })
}

function mockNarrowRailLayout() {
  vi.spyOn(Element.prototype, 'clientWidth', 'get').mockReturnValue(343)
  vi.spyOn(Element.prototype, 'scrollWidth', 'get').mockReturnValue(602)
  const positions: Record<string, { left: number; width: number }> = {
    'governance-overview-tab': { left: 6, width: 112 },
    'governance-import-tab': { left: 122, width: 112 },
    'governance-models-tab': { left: 238, width: 112 },
    'governance-monitor-tab': { left: 354, width: 126 },
    'governance-history-tab': { left: 484, width: 112 }
  }
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
    const tab = positions[this.id]
    const left = tab ? 16 + tab.left - (this.parentElement?.parentElement?.scrollLeft || 0) : 16
    const width = tab?.width ?? 343
    return { left, right: left + width, top: 80, bottom: 124, width, height: 44, x: left, y: 80, toJSON: () => ({}) }
  })
}

describe('Smart Operations tabs', () => {
  it('labels every tab and exposes the selected tab and its associated panel', () => {
    const wrapper = renderTabs('models')
    const tablist = wrapper.get('[role="tablist"]')
    const tabs = tablist.findAll('[role="tab"]')

    expect(tablist.attributes('aria-label')).toBe('智能运维')
    expect(tablist.attributes('aria-orientation')).toBe('horizontal')
    expect(tabs.map(tab => tab.text())).toEqual(['运维总览', '分组导入', '模型监测', '自动化策略', '运行记录'])
    expect(tabs.map(tab => tab.attributes('id'))).toEqual([
      'governance-overview-tab', 'governance-import-tab', 'governance-models-tab', 'governance-monitor-tab', 'governance-history-tab'
    ])
    expect(tabs.map(tab => tab.attributes('aria-controls'))).toEqual([
      undefined, undefined, 'governance-models-panel', undefined, undefined
    ])
    expect(tabs.map(tab => tab.attributes('aria-selected'))).toEqual(['false', 'false', 'true', 'false', 'false'])
    expect(tabs.map(tab => tab.attributes('tabindex'))).toEqual(['-1', '-1', '0', '-1', '-1'])
    expect(tabs.every(tab => tab.get('svg').attributes('aria-hidden') === 'true')).toBe(true)
  })

  it('requests selection on click and waits for its controlled model to change', async () => {
    const wrapper = renderTabs()
    await wrapper.get('#governance-import-tab').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([['import']])
    expect(wrapper.get('#governance-overview-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('#governance-import-tab').attributes('aria-selected')).toBe('false')

    await wrapper.setProps({ modelValue: 'import' })
    expect(wrapper.get('#governance-import-tab').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('#governance-import-tab').attributes('aria-controls')).toBe('governance-import-panel')
    expect(wrapper.get('#governance-overview-tab').attributes('aria-controls')).toBeUndefined()
    expect(wrapper.get('#governance-import-tab').attributes('tabindex')).toBe('0')
    expect(wrapper.get('#governance-overview-tab').attributes('tabindex')).toBe('-1')
  })

  it('moves focus with arrow keys and wraps without activating a section', async () => {
    const wrapper = renderTabs()
    const overview = wrapper.get<HTMLButtonElement>('#governance-overview-tab')
    const history = wrapper.get<HTMLButtonElement>('#governance-history-tab')
    const imported = wrapper.get<HTMLButtonElement>('#governance-import-tab')
    overview.element.focus()

    await overview.trigger('keydown', { key: 'ArrowLeft' })
    expect(document.activeElement).toBe(history.element)
    expect(history.attributes('tabindex')).toBe('0')
    expect(overview.attributes('tabindex')).toBe('-1')

    await history.trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement).toBe(overview.element)
    await overview.trigger('keydown', { key: 'ArrowRight' })
    expect(document.activeElement).toBe(imported.element)
    expect(imported.attributes('tabindex')).toBe('0')
    expect(overview.attributes('aria-selected')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('moves to the first and last tab with Home and End without activating', async () => {
    const wrapper = renderTabs('models')
    const models = wrapper.get<HTMLButtonElement>('#governance-models-tab')
    const history = wrapper.get<HTMLButtonElement>('#governance-history-tab')
    models.element.focus()

    await models.trigger('keydown', { key: 'End' })
    expect(document.activeElement).toBe(history.element)
    await history.trigger('keydown', { key: 'Home' })
    expect(document.activeElement).toBe(wrapper.get('#governance-overview-tab').element)
    expect(models.attributes('aria-selected')).toBe('true')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it.each(['Enter', ' '])('activates the focused tab with %j', async key => {
    const wrapper = renderTabs()
    await wrapper.get('#governance-overview-tab').trigger('keydown', { key: 'ArrowRight' })
    const event = new KeyboardEvent('keydown', { key, cancelable: true, bubbles: true })
    wrapper.get('#governance-import-tab').element.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([['import']])
  })

  it('does not intercept vertical scrolling or ordinary typing', () => {
    const wrapper = renderTabs()
    for (const key of ['ArrowDown', 'ArrowUp', 'a']) {
      const event = new KeyboardEvent('keydown', { key, cancelable: true, bubbles: true })
      wrapper.get('#governance-overview-tab').element.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(false)
    }
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('follows external selection without stealing focus from outside the tablist', async () => {
    const wrapper = renderTabs()
    const outside = document.createElement('button')
    document.body.append(outside)
    outside.focus()
    try {
      await wrapper.setProps({ modelValue: 'history' })
      expect(wrapper.findAll('[role="tab"][tabindex="0"]')).toHaveLength(1)
      expect(wrapper.get('#governance-history-tab').attributes('tabindex')).toBe('0')
      expect(wrapper.get('#governance-history-tab').attributes('aria-selected')).toBe('true')
      expect(document.activeElement).toBe(outside)
    } finally {
      outside.remove()
    }
  })

  it('reveals a deep-linked selected tab on mount without moving focus', () => {
    mockNarrowRailLayout()
    const outside = document.createElement('button')
    document.body.append(outside)
    outside.focus()
    try {
      const wrapper = renderTabs('history')
      const rail = wrapper.element as HTMLElement
      expect(rail.scrollLeft).toBe(253)
      expect(rail.scrollTop).toBe(0)
      expect(document.activeElement).toBe(outside)
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    } finally {
      outside.remove()
    }
  })

  it('scrolls only the rail when an externally selected tab is outside its visible bounds', async () => {
    mockNarrowRailLayout()
    const wrapper = renderTabs()
    const rail = wrapper.element as HTMLElement
    rail.scrollTop = 37
    expect(rail.scrollLeft).toBe(0)

    await wrapper.setProps({ modelValue: 'import' })
    expect(rail.scrollLeft).toBe(0)
    await wrapper.setProps({ modelValue: 'history' })
    expect(rail.scrollLeft).toBe(253)
    await wrapper.setProps({ modelValue: 'models' })
    expect(rail.scrollLeft).toBe(238)
    await wrapper.setProps({ modelValue: 'overview' })
    expect(rail.scrollLeft).toBe(6)
    expect(rail.scrollTop).toBe(37)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('prevents pointer and keyboard activation while disabled and restores the tab stop', async () => {
    const wrapper = renderTabs('models', true)
    const tabs = wrapper.findAll<HTMLButtonElement>('[role="tab"]')
    expect(tabs).toHaveLength(5)
    expect(tabs.every(tab => tab.element.disabled && tab.attributes('tabindex') === '-1')).toBe(true)

    await wrapper.get('#governance-import-tab').trigger('click')
    await wrapper.get('#governance-models-tab').trigger('keydown', { key: 'ArrowRight' })
    await wrapper.get('#governance-models-tab').trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await wrapper.setProps({ disabled: false })
    expect(wrapper.get('#governance-models-tab').attributes('tabindex')).toBe('0')
    expect(wrapper.findAll('[role="tab"][tabindex="0"]')).toHaveLength(1)
  })

  it('renders English navigation labels when the locale changes', () => {
    const wrapper = renderTabs('overview', false, 'en')
    expect(wrapper.get('[role="tablist"]').attributes('aria-label')).toBe('Smart Operations')
    expect(wrapper.findAll('[role="tab"]').map(tab => tab.text())).toEqual([
      'Operations overview', 'Group import', 'Model monitoring', 'Automation policies', 'Run history'
    ])
  })
})
