import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import CanvasEmbedView from '@/views/user/CanvasEmbedView.vue'

describe('CanvasEmbedView', () => {
  afterEach(() => vi.restoreAllMocks())

  function mountView() {
    return mount(CanvasEmbedView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
        },
      },
    })
  }

  it('loads the external canvas without putting credentials in the URL', () => {
    const wrapper = mountView()
    const iframe = wrapper.get('[data-canvas-iframe]')
    expect(iframe.attributes('src')).toBe('https://canvas.oreniva.com/canvas')
    expect(iframe.attributes('src')).not.toContain('token')
  })

  it('opens the same credential-free URL in a new tab', async () => {
    const open = vi.spyOn(window, 'open').mockReturnValue({} as Window)
    const wrapper = mountView()
    await wrapper.get('[data-canvas-open]').trigger('click')
    expect(open).toHaveBeenCalledWith('https://canvas.oreniva.com/canvas', '_blank')
  })

  it('reloads the iframe after a retry', async () => {
    const wrapper = mountView()
    const before = wrapper.get('[data-canvas-iframe]').attributes('src')
    await wrapper.get('[data-canvas-reload]').trigger('click')
    expect(wrapper.get('[data-canvas-iframe]').attributes('src')).toBe(before)
    expect(wrapper.find('[data-canvas-iframe]').exists()).toBe(true)
  })
})
