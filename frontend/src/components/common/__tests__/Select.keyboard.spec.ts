import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import Select from '../Select.vue'
import BaseDialog from '../BaseDialog.vue'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
let wrapper: ReturnType<typeof mount>
afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

async function press(key: string) {
  document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
  await nextTick()
}

async function open(searchable: boolean | 'auto' = 'auto') {
  wrapper = mount(Select, {
    attachTo: document.body,
    props: { modelValue: 'alpha', searchable, options: [
      { value: 'alpha', label: 'Alpha' },
      { value: 'disabled', label: 'Disabled', disabled: true },
      { value: 'beta', label: 'Beta' },
    ] },
  })
  wrapper.get<HTMLButtonElement>('button').element.focus()
  await press('ArrowDown')
  await nextTick()
}

describe('Select keyboard focus', () => {
  it.each([false, 'auto'] as const)('selects an enabled option without a search input (%s)', async (searchable) => {
    await open(searchable)
    expect(document.activeElement).toBe(document.querySelector('[role="listbox"]'))
    await press('ArrowDown')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
  })

  it('allows Escape to close a non-searchable dropdown and restore trigger focus', async () => {
    await open(false)
    await press('Escape')
    expect(wrapper.get('button').attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(wrapper.get('button').element)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps focus in the search input when search is enabled', async () => {
    await open(true)
    expect(document.activeElement).toBe(document.querySelector('.select-search-input'))
    await press('ArrowDown')
    await press('Enter')
    expect(wrapper.emitted('update:modelValue')).toEqual([['beta']])
  })

  it.each([false, true])('closes only the dropdown on the first Escape inside a dialog (searchable=%s)', async (searchable) => {
    wrapper = mount(defineComponent({
      components: { BaseDialog, CommonSelect: Select },
      setup: () => ({ show: ref(true), draft: ref('Unsaved site name'), value: ref('alpha'), searchable }),
      template: `<BaseDialog :show="show" title="Edit site" @close="show = false">
        <input v-model="draft" data-test="draft" />
        <CommonSelect v-model="value" :searchable="searchable" :options="[{ value: 'alpha', label: 'Alpha' }, { value: 'beta', label: 'Beta' }]" />
      </BaseDialog>`,
    }), { attachTo: document.body })
    await nextTick()
    const select = wrapper.getComponent(Select)
    const dialog = wrapper.getComponent(BaseDialog)
    const trigger = select.get<HTMLButtonElement>('button')
    trigger.element.focus()
    await press('ArrowDown')
    await nextTick()
    expect(document.querySelector('[role="listbox"]')).not.toBeNull()

    await press('Escape')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.querySelector('[role="dialog"]')).not.toBeNull()
    expect(dialog.props('show')).toBe(true)
    expect(document.querySelector<HTMLInputElement>('[data-test="draft"]')?.value).toBe('Unsaved site name')
    expect(document.activeElement).toBe(trigger.element)
    expect(dialog.emitted('close')).toBeUndefined()

    await press('Escape')
    expect(dialog.emitted('close')).toHaveLength(1)
    expect(dialog.props('show')).toBe(false)
  })
})
