import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import PromptNode from '../components/nodes/PromptNode.vue'

describe('PromptNode reference images', () => {
  it('uploads a reference image, inserts a readable prompt token, and renders a thumbnail', async () => {
    const wrapper = mount(PromptNode, { props: {
      node: { id: 'prompt', type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'A cat' } },
    } })
    const file = new File(['png'], 'cat.png', { type: 'image/png' })
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [file] })
    await input.trigger('change')
    await vi.waitFor(() => expect(wrapper.emitted('update')).toHaveLength(1))
    const update = wrapper.emitted('update')?.at(-1)?.[0] as { metadata: Record<string, unknown> }
    expect(update.metadata.prompt).toBe('A cat [参考图1]')
    expect(update.metadata.text).toBe('A cat [参考图1]')
    expect(update.metadata.referenceImages).toEqual([expect.objectContaining({ name: 'cat.png', mimeType: 'image/png', dataUrl: 'data:image/png;base64,cG5n' })])
    await wrapper.setProps({ node: { id: 'prompt', type: 'prompt', position: { x: 0, y: 0 }, metadata: update.metadata } })
    expect(wrapper.find('img').exists()).toBe(true)
    expect(wrapper.find('[data-reference-image-remove]').exists()).toBe(true)
  })

  it('removes the reference and its generated token without dropping other prompt text', async () => {
    const wrapper = mount(PromptNode, { props: {
      node: { id: 'prompt', type: 'prompt', position: { x: 0, y: 0 }, metadata: {
        prompt: 'A cat [参考图1] in a room',
        referenceImages: [{ name: 'cat.png', mimeType: 'image/png', dataUrl: 'data:image/png;base64,cG5n' }],
      } },
    } })
    await wrapper.get('[data-reference-image-remove]').trigger('click')
    const update = wrapper.emitted('update')?.at(-1)?.[0] as { metadata: Record<string, unknown> }
    expect(update.metadata.prompt).toBe('A cat in a room')
    expect(update.metadata.referenceImages).toEqual([])
  })

  it('renumbers later reference tokens when a middle thumbnail is removed', async () => {
    const wrapper = mount(PromptNode, { props: {
      node: { id: 'prompt', type: 'prompt', position: { x: 0, y: 0 }, metadata: {
        prompt: '[参考图1] then [参考图2] then [参考图3]',
        referenceImages: [
          { name: 'one.png', mimeType: 'image/png', dataUrl: 'data:image/png;base64,YQ==' },
          { name: 'two.png', mimeType: 'image/png', dataUrl: 'data:image/png;base64,Yg==' },
          { name: 'three.png', mimeType: 'image/png', dataUrl: 'data:image/png;base64,Yw==' },
        ],
      } },
    } })
    await wrapper.findAll('[data-reference-image-remove]')[1].trigger('click')
    const update = wrapper.emitted('update')?.at(-1)?.[0] as { metadata: Record<string, unknown> }
    expect(update.metadata.prompt).toBe('[参考图1] then then [参考图2]')
    expect(update.metadata.referenceImages).toHaveLength(2)
  })
})
