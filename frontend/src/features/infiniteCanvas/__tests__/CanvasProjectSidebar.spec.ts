import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CanvasProjectSidebar from '../components/CanvasProjectSidebar.vue'
import type { CanvasProject } from '../types'

function project(): CanvasProject {
  const now = new Date('2026-01-01T00:00:00.000Z')
  return { id: 'project-1', title: 'Original name', createdAt: now, updatedAt: now, viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid', nodes: [], edges: [] }
}

describe('CanvasProjectSidebar rename dialog', () => {
  it('uses the built-in dialog and emits a trimmed title', async () => {
    const prompt = vi.spyOn(window, 'prompt')
    const wrapper = mount(CanvasProjectSidebar, {
      props: { projects: [project()], activeProjectId: 'project-1' },
      global: {
        stubs: {
          BaseDialog: { template: '<div v-if="show"><slot /><slot name="footer" /></div>', props: ['show'] },
        },
      },
    })

    await wrapper.find('[data-rename-project="project-1"]').trigger('click')
    expect(prompt).not.toHaveBeenCalled()
    const input = wrapper.find('[data-rename-input]')
    expect(input.exists()).toBe(true)
    await input.setValue('  Renamed project  ')
    await wrapper.find('[data-rename-submit]').trigger('click')
    expect(wrapper.emitted('rename')).toEqual([['project-1', 'Renamed project']])
    prompt.mockRestore()
  })
})
