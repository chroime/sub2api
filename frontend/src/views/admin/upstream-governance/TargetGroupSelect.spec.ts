import { DOMWrapper, enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { h, nextTick } from 'vue'
import TargetGroupSelect from './TargetGroupSelect.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

enableAutoUnmount(afterEach)
afterEach(() => { document.body.innerHTML = '' })

const groups = [
  { id: 1, name: 'GPT group', platform: 'openai', rate_multiplier: 0.8 },
  { id: 2, name: 'DeepSeek group', platform: 'deepseek', rate_multiplier: 2 },
  { id: 3, name: 'Mixed group', platform: 'composite', rate_multiplier: 1 },
]
function mountSelector(modelValue: number[], props: Partial<InstanceType<typeof TargetGroupSelect>['$props']> = {}) {
  const wrapper: VueWrapper = mount(TargetGroupSelect, {
    props: { groups, modelValue, ...props, 'onUpdate:modelValue': (value: number[]) => wrapper.setProps({ modelValue: value }) },
    attachTo: document.body,
  })
  return wrapper
}
function searchFor(wrapper: VueWrapper) {
  const listboxId = wrapper.get('button').attributes('aria-controls')
  const input = document.querySelector<HTMLInputElement>(`input[aria-controls="${listboxId}"]`)
  expect(input).not.toBeNull()
  return new DOMWrapper(input!)
}
function optionsFor(wrapper: VueWrapper) {
  const listboxId = wrapper.get('button').attributes('aria-controls')
  return [...document.getElementById(listboxId)!.querySelectorAll<HTMLElement>('[role=option]')]
}
async function openSelector(wrapper: VueWrapper) {
  await wrapper.get('button').trigger('click')
  await flushPromises()
  return searchFor(wrapper)
}
async function keydown(element: Element, key: string, options: KeyboardEventInit = {}) {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...options })
  element.dispatchEvent(event)
  await nextTick()
  return event
}

describe('target group selection', () => {
  it('keeps vendor logos and incompatible groups visible while toggling multiple compatible destinations', async () => {
    const wrapper = mountSelector([1], { transport: 'openai' })
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('openai')
    expect(wrapper.get('button').attributes('title')).toContain('0.8×')
    await openSelector(wrapper)
    const options = optionsFor(wrapper)
    expect(options).toHaveLength(3)
    expect(options.every(option => option.querySelector('svg'))).toBe(true)
    expect(options[1]!.getAttribute('aria-disabled')).toBe('true')
    expect(options[1]!.textContent).toContain('governance.targetProtocolMismatch')
    options[1]!.click()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(options[2]!.textContent).toContain('governance.compositeGroup')
    options[2]!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([[1, 3]])
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('button').text()).toContain('+1')
    expect(options[0]!.getAttribute('aria-selected')).toBe('true')
    expect(options[2]!.getAttribute('aria-selected')).toBe('true')
    options[0]!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([[3]])
    expect(options[0]!.getAttribute('aria-selected')).toBe('false')
  })

  it('searches bulk destinations across vendors and clears to an empty array', async () => {
    const wrapper = mountSelector([2])
    expect(wrapper.getComponent(PlatformIcon).props('platform')).toBe('deepseek')
    const search = await openSelector(wrapper)
    expect(optionsFor(wrapper).every(option => option.getAttribute('aria-disabled') === 'false')).toBe(true)
    await search.setValue('gPt')
    expect(optionsFor(wrapper)).toHaveLength(1)
    expect(optionsFor(wrapper)[0]!.textContent).toContain('GPT group')
    optionsFor(wrapper)[0]!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.[0]).toEqual([[2, 1]])
    await search.setValue('missing destination')
    expect(optionsFor(wrapper)).toHaveLength(0)
    expect(document.body.textContent).toContain('governance.noMatchingGroups')
    document.querySelector<HTMLButtonElement>('[data-test=clear-targets]')!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')?.[1]).toEqual([[]])
    expect(document.activeElement).toBe(search.element)
    expect(document.querySelector<HTMLButtonElement>('[data-test=clear-targets]')!.disabled).toBe(true)
  })

  it('uses Arrow and Enter to toggle selections, skips incompatible options and exposes the active option to assistive technology', async () => {
    const wrapper = mountSelector([], { transport: 'openai' })
    await wrapper.get('button').trigger('keydown', { key: 'ArrowDown' })
    await flushPromises()
    const search = searchFor(wrapper)
    const options = optionsFor(wrapper)
    expect(document.activeElement).toBe(search.element)
    const listbox = document.getElementById(search.attributes('aria-controls'))!
    expect(listbox.getAttribute('role')).toBe('listbox')
    expect(listbox.getAttribute('aria-multiselectable')).toBe('true')
    expect(search.attributes('aria-activedescendant')).toBeUndefined()
    expect((await keydown(search.element, 'ArrowDown')).defaultPrevented).toBe(true)
    expect(search.attributes('aria-activedescendant')).toBe(options[0]!.id)
    await keydown(search.element, 'Enter')
    await keydown(search.element, 'ArrowDown')
    expect(search.attributes('aria-activedescendant')).toBe(options[2]!.id)
    await keydown(search.element, 'Enter')
    expect(wrapper.props('modelValue')).toEqual([1, 3])
    await keydown(search.element, 'ArrowUp')
    expect(search.attributes('aria-activedescendant')).toBe(options[0]!.id)
    await keydown(search.element, 'Enter')
    expect(wrapper.props('modelValue')).toEqual([3])
    await search.setValue('Mixed')
    expect(search.attributes('aria-activedescendant')).toBeUndefined()
    await keydown(search.element, 'Escape')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })

  it.each(['Enter', ' '])('preserves native %j activation of the focused clear button', async key => {
    const wrapper = mountSelector([1, 3], { transport: 'openai' })
    const search = await openSelector(wrapper)
    await keydown(search.element, 'ArrowDown')
    expect((await keydown(search.element, 'Tab')).defaultPrevented).toBe(true)
    const clear = document.querySelector<HTMLButtonElement>('[data-test=clear-targets]')!
    expect(document.activeElement).toBe(clear)
    const activation = await keydown(clear, key)
    expect(activation.defaultPrevented).toBe(false)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    // jsdom does not synthesize native button clicks for keyboard events.
    // An uncancelled key must remain available to the browser's activation.
    clear.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
    expect(wrapper.props('modelValue')).toEqual([])
    expect(document.activeElement).toBe(search.element)
  })

  it('moves Tab between search and clear, then closes without cancelling navigation from the trigger', async () => {
    const wrapper = mountSelector([1])
    let search = await openSelector(wrapper)
    await keydown(search.element, 'Tab')
    let clear = document.querySelector<HTMLButtonElement>('[data-test=clear-targets]')!
    expect(document.activeElement).toBe(clear)
    expect((await keydown(clear, 'Tab', { shiftKey: true })).defaultPrevented).toBe(true)
    expect(document.activeElement).toBe(search.element)
    expect((await keydown(search.element, 'Tab', { shiftKey: true })).defaultPrevented).toBe(false)
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)

    search = await openSelector(wrapper)
    await keydown(search.element, 'Tab')
    clear = document.querySelector<HTMLButtonElement>('[data-test=clear-targets]')!
    expect((await keydown(clear, 'Tab')).defaultPrevented).toBe(false)
    expect(document.querySelector('[role=listbox]')).toBeNull()
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })

  it('retains search focus after a pointer selection so keyboard selection and Escape still work', async () => {
    const wrapper = mountSelector([], { transport: 'openai' })
    const search = await openSelector(wrapper)
    const pointer = new MouseEvent('mousedown', { bubbles: true, cancelable: true })
    optionsFor(wrapper)[0]!.dispatchEvent(pointer)
    expect(pointer.defaultPrevented).toBe(true)
    optionsFor(wrapper)[0]!.click()
    await nextTick()
    expect(document.activeElement).toBe(search.element)
    await keydown(search.element, 'ArrowDown')
    await keydown(search.element, 'ArrowDown')
    await keydown(search.element, 'Enter')
    expect(wrapper.props('modelValue')).toEqual([1, 3])
    await keydown(search.element, 'Escape')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })

  it('allows the hundredth target, blocks new targets at the limit and permits removing one to choose another', async () => {
    const manyGroups = Array.from({ length: 101 }, (_, index) => ({ id: index + 1, name: `Group ${index + 1}`, platform: 'openai', rate_multiplier: 1 }))
    const wrapper = mountSelector(manyGroups.slice(0, 99).map(group => group.id), { groups: manyGroups, transport: 'openai' })
    await openSelector(wrapper)
    optionsFor(wrapper)[99]!.click()
    await nextTick()
    expect(wrapper.props('modelValue')).toHaveLength(100)
    expect(document.body.textContent).toContain('governance.targetLimit')
    expect(optionsFor(wrapper)[100]!.getAttribute('aria-disabled')).toBe('true')
    expect(optionsFor(wrapper)[0]!.getAttribute('aria-disabled')).toBe('false')
    optionsFor(wrapper)[100]!.click()
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    optionsFor(wrapper)[0]!.click()
    await nextTick()
    expect(wrapper.props('modelValue')).toHaveLength(99)
    expect(optionsFor(wrapper)[100]!.getAttribute('aria-disabled')).toBe('false')
    optionsFor(wrapper)[100]!.click()
    await nextTick()
    expect(wrapper.props('modelValue')).toEqual(manyGroups.slice(1).map(group => group.id))
  })

  it('shows missing selected destinations after group refresh and allows clearing without silently dropping their IDs', async () => {
    const wrapper = mountSelector([1, 3])
    await wrapper.setProps({ groups: groups.filter(group => group.id !== 1) })
    expect(wrapper.get('button').text()).toContain('governance.unavailableTarget')
    expect(wrapper.get('button').text()).toContain('+1')
    expect(wrapper.get('button').attributes('title')).toContain('Mixed group')
    expect(wrapper.props('modelValue')).toEqual([1, 3])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await openSelector(wrapper)
    document.querySelector<HTMLButtonElement>('[data-test=clear-targets]')!.click()
    await nextTick()
    expect(wrapper.emitted('update:modelValue')).toEqual([[[]]])
  })

  it('keeps listbox and active-option IDs unique across multiple selector instances', async () => {
    const first = mountSelector([])
    const second = mountSelector([])
    const firstSearch = await openSelector(first)
    const secondSearch = await openSelector(second)
    expect(firstSearch.attributes('aria-controls')).not.toBe(secondSearch.attributes('aria-controls'))
    for (const [wrapper, search] of [[first, firstSearch], [second, secondSearch]] as const) {
      await keydown(search.element, 'ArrowDown')
      const activeID = search.attributes('aria-activedescendant')
      const activeOption = document.getElementById(activeID)!
      expect(activeOption.getAttribute('role')).toBe('option')
      expect(document.getElementById(wrapper.get('button').attributes('aria-controls'))!.contains(activeOption)).toBe(true)
    }
  })

  it('keeps the teleported menu inside a short mobile viewport', async () => {
    const originalHeight = window.innerHeight
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 160 })
    vi.spyOn(HTMLButtonElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 8, y: 20, top: 20, right: 288, bottom: 60, left: 8,
      width: 280, height: 40, toJSON: () => ({}),
    })
    try {
      const wrapper = mountSelector([])
      await openSelector(wrapper)
      const menu = document.querySelector<HTMLElement>('[role="listbox"]')?.parentElement
      expect(menu?.style.top).toBe('66px')
      expect(menu?.style.maxHeight).toBe('86px')
    } finally {
      Object.defineProperty(window, 'innerHeight', { configurable: true, value: originalHeight })
      vi.restoreAllMocks()
    }
  })

  it('closes only the popup on the first Escape inside a real dialog, then lets Escape close the dialog', async () => {
    const dialog = mount(BaseDialog, {
      props: { show: true, title: 'Import settings' },
      slots: { default: () => h(TargetGroupSelect, { modelValue: [1], groups, transport: 'openai' }) },
      attachTo: document.body,
    })
    await flushPromises()
    const selector = dialog.getComponent(TargetGroupSelect)
    const search = await openSelector(selector)
    await keydown(search.element, 'Escape')
    expect(dialog.emitted('close')).toBeUndefined()
    expect(selector.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(selector.get('button').element)
    await keydown(selector.get('button').element, 'Escape')
    expect(dialog.emitted('close')).toHaveLength(1)
  })
})
