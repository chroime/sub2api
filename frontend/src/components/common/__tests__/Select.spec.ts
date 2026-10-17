import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../Select.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const originalInnerWidth = window.innerWidth
const originalInnerHeight = window.innerHeight
let unmountWrapper: (() => void) | undefined

const setViewportWidth = (width: number) => {
  Object.defineProperty(window, 'innerWidth', {
    configurable: true,
    value: width,
  })
}

const setViewportHeight = (height: number) => {
  Object.defineProperty(window, 'innerHeight', {
    configurable: true,
    value: height,
  })
}

const mockTriggerRect = (left: number, width: number) => {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    x: left,
    y: 20,
    top: 20,
    right: left + width,
    bottom: 60,
    left,
    width,
    height: 40,
    toJSON: () => ({}),
  })
}

const openSelect = async () => {
  const wrapper = mount(Select, {
    props: {
      modelValue: null,
      options: [
        {
          value: 'example',
          label: 'very-long-unbroken-option-value-that-must-not-overflow',
        },
      ],
    },
  })
  unmountWrapper = () => wrapper.unmount()

  await wrapper.get('button').trigger('click')
  await nextTick()

  return document.body.querySelector<HTMLElement>('.select-dropdown-portal')
}

afterEach(() => {
  unmountWrapper?.()
  unmountWrapper = undefined
  document.body.innerHTML = ''
  setViewportWidth(originalInnerWidth)
  setViewportHeight(originalInnerHeight)
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('Select dropdown viewport constraints', () => {
  it('preserves the existing 200px minimum width when space is available', async () => {
    setViewportWidth(1024)
    mockTriggerRect(20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('20px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('996px')
  })

  it('shifts a right-edge dropdown left so its options remain readable', async () => {
    setViewportWidth(320)
    mockTriggerRect(220, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('112px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('200px')
  })

  it('clamps a trigger left of the viewport to the safe padding', async () => {
    setViewportWidth(320)
    mockTriggerRect(-20, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('8px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('304px')
  })

  it('clamps an offscreen-right trigger position to the viewport boundary', async () => {
    setViewportWidth(320)
    mockTriggerRect(400, 80)

    const dropdown = await openSelect()

    expect(dropdown).not.toBeNull()
    expect(dropdown?.style.left).toBe('112px')
    expect(dropdown?.style.minWidth).toBe('200px')
    expect(dropdown?.style.maxWidth).toBe('200px')
  })

  it('uses the natural dropdown height when deciding to flip above the trigger', async () => {
    setViewportHeight(500)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 20, y: 360, top: 360, right: 220, bottom: 400, left: 20,
      width: 200, height: 40, toJSON: () => ({}),
    })
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function () {
      return this.classList.contains('select-dropdown-portal') ? 176 : 0
    })
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function () {
      return this.classList.contains('select-dropdown-portal') ? 43 : 0
    })

    const dropdown = await openSelect()
    await nextTick()

    expect(dropdown?.style.bottom).toBe('144px')
    expect(dropdown?.style.maxHeight).toBe('348px')
  })

  it('uses the options list height when the portal itself is max-height constrained', async () => {
    setViewportHeight(500)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 20, y: 360, top: 360, right: 220, bottom: 400, left: 20,
      width: 200, height: 40, toJSON: () => ({}),
    })
    vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockImplementation(function () {
      return this.classList.contains('select-options') ? 176 : 32
    })
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function () {
      return this.classList.contains('select-dropdown-portal') ? 43 : 0
    })

    const dropdown = await openSelect()
    await nextTick()

    expect(dropdown?.style.bottom).toBe('144px')
    expect(dropdown?.style.maxHeight).toBe('348px')
  })

  it('caps a dropdown to the available height above a bottom-edge trigger', async () => {
    setViewportHeight(300)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 20, y: 240, top: 240, right: 220, bottom: 280, left: 20,
      width: 200, height: 40, toJSON: () => ({}),
    })

    const dropdown = await openSelect()
    await nextTick()

    expect(dropdown?.style.bottom).toBe('64px')
    expect(dropdown?.style.maxHeight).toBe('228px')
  })

  it('caps a dropdown when there is not enough room on either side', async () => {
    setViewportHeight(220)
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
      x: 20, y: 90, top: 90, right: 220, bottom: 130, left: 20,
      width: 200, height: 40, toJSON: () => ({}),
    })

    const dropdown = await openSelect()
    await nextTick()

    expect(dropdown?.style.maxHeight).toBe('78px')
  })
})

describe('Select remote search', () => {
  const mountRemoteSelect = (props: Record<string, unknown> = {}) => {
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        remote: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
        ...props,
      },
    })
    unmountWrapper = () => wrapper.unmount()
    return wrapper
  }

  const openDropdown = async () => {
    const dropdown = document.body.querySelector<HTMLElement>('.select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    return dropdown as HTMLElement
  }

  const typeSearchQuery = async (query: string) => {
    const dropdown = await openDropdown()
    const input = dropdown.querySelector<HTMLInputElement>('.select-search-input')
    expect(input).not.toBeNull()
    input!.value = query
    input!.dispatchEvent(new Event('input'))
    await nextTick()
  }

  it('emits debounced search events and skips local filtering in remote mode', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('zzz')

    // 防抖窗口内不触发。
    expect(wrapper.emitted('search')).toBeUndefined()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toEqual([['zzz']])
    // 远程模式不做本地过滤：无命中的 query 下选项仍完整展示（由父组件更新 options）。
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toContain('Alpha account')
    expect(labels).toContain('Beta account')
  })

  it('does not emit search when the dropdown closes and the query resets', async () => {
    vi.useFakeTimers()
    const wrapper = mountRemoteSelect()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('hidden')

    // 关闭下拉：排队中的防抖定时器应被取消，也不应因 query 重置而尾随 emit。
    await wrapper.get('button').trigger('click')
    await nextTick()
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
  })

  it('shows the loading text instead of empty text while loading with no options', async () => {
    const wrapper = mountRemoteSelect({ options: [], loading: true })
    await wrapper.get('button').trigger('click')
    await nextTick()

    const dropdown = await openDropdown()
    expect(dropdown.querySelector('.select-empty')?.textContent).toContain('common.loading')
  })

  it('keeps local filtering and emits nothing when remote is not set', async () => {
    vi.useFakeTimers()
    const wrapper = mount(Select, {
      props: {
        modelValue: null,
        searchable: true,
        options: [
          { value: 'alpha', label: 'Alpha account' },
          { value: 'beta', label: 'Beta account' },
        ],
      },
    })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    await nextTick()

    await typeSearchQuery('alpha')
    await vi.advanceTimersByTimeAsync(300)

    expect(wrapper.emitted('search')).toBeUndefined()
    const dropdown = await openDropdown()
    const labels = [...dropdown.querySelectorAll('.select-option-label')].map((el) => el.textContent)
    expect(labels).toEqual(['Alpha account'])
  })
})

describe('Select disabled state', () => {
  it('closes an already open portal when its parent form becomes disabled', async () => {
    const wrapper = mount(Select, {
      props: { modelValue: null, options: [{ value: 'newapi', label: 'New API' }] },
    })
    unmountWrapper = () => wrapper.unmount()
    await wrapper.get('button').trigger('click')
    await nextTick()
    expect(document.body.querySelector('.select-dropdown-portal')).not.toBeNull()

    await wrapper.setProps({ disabled: true })
    await nextTick()
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    document.body.querySelector<HTMLElement>('.select-dropdown-portal [role="option"]')?.click()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
