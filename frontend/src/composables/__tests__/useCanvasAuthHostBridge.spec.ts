import { defineComponent, h, nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { useCanvasAuthHostBridge } from '@/composables/useCanvasAuthHostBridge'

const authState = vi.hoisted(() => ({
  token: 'host-token',
  isAuthenticated: true,
  user: { id: 7, role: 'user' },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState,
}))

describe('useCanvasAuthHostBridge', () => {
  beforeEach(() => {
    authState.token = 'host-token'
    authState.isAuthenticated = true
    authState.user = { id: 7, role: 'user' }
  })

  it('ignores an untrusted origin and answers a trusted canvas window', async () => {
    const target = { closed: false, postMessage: vi.fn() } as unknown as Window
    const wrapper = mount(defineComponent({ setup: () => { useCanvasAuthHostBridge(); return () => h('div') } }))

    window.dispatchEvent(new MessageEvent('message', {
      origin: 'https://evil.example',
      source: target,
      data: { type: 'sub2api.canvas.ready', version: 1 },
    }))
    expect(target.postMessage).not.toHaveBeenCalled()

    window.dispatchEvent(new MessageEvent('message', {
      origin: 'https://canvas.oreniva.com',
      source: target,
      data: { type: 'sub2api.canvas.ready', version: 1 },
    }))
    await nextTick()
    expect(target.postMessage).toHaveBeenCalledWith(expect.objectContaining({ type: 'sub2api.auth.init', token: 'host-token', apiBase: expect.stringContaining('/api/v1') }), 'https://canvas.oreniva.com')
    wrapper.unmount()
  })

  it('sends clear after the host is unauthenticated', async () => {
    const target = { closed: false, postMessage: vi.fn() } as unknown as Window
    const wrapper = mount(defineComponent({ setup: () => { useCanvasAuthHostBridge(); return () => h('div') } }))
    window.dispatchEvent(new MessageEvent('message', { origin: 'https://canvas.oreniva.com', source: target, data: { type: 'sub2api.canvas.ready', version: 1 } }))
    authState.token = null
    authState.isAuthenticated = false
    window.dispatchEvent(new MessageEvent('message', { origin: 'https://canvas.oreniva.com', source: target, data: { type: 'sub2api.canvas.ready', version: 1 } }))
    await nextTick()
    expect(target.postMessage).toHaveBeenLastCalledWith(expect.objectContaining({ type: 'sub2api.auth.clear' }), 'https://canvas.oreniva.com')
    wrapper.unmount()
  })
})
