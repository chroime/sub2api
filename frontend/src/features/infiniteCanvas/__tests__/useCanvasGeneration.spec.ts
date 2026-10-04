import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ImageGenerationError, type GeneratedImage } from '@/api/imageGeneration'
import type { CanvasProject, CanvasRepository } from '../types'
import { useInfiniteCanvasStore } from '../stores/useInfiniteCanvasStore'
import { useCanvasGeneration } from '../composables/useCanvasGeneration'
import ConfigNode from '../components/nodes/ConfigNode.vue'

function repository(projects: CanvasProject[] = []) {
  const assets: { key: string; blob: Blob }[] = []
  let sequence = 0
  const value: CanvasRepository = {
    async listProjects() { return structuredClone(projects) }, async loadProject() { return null }, async saveProject() {}, async deleteProject() {},
    async saveAsset(asset) { const key = asset.storageKey ?? `asset-${++sequence}`; assets.push({ key, blob: asset.blob }); return key },
    async loadAsset(key) { const item = assets.find((asset) => asset.key === key); return item ? { blob: item.blob, mimeType: item.blob.type, kind: 'image', storageKey: key } : undefined },
    async deleteAsset(key) { const index = assets.findIndex((asset) => asset.key === key); if (index >= 0) assets.splice(index, 1) },
  }
  return { value, assets }
}

function fixture() {
  const now = new Date()
  return { id: 'project-1', title: 'Canvas', createdAt: now, updatedAt: now, viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid' as const, nodes: [], edges: [] }
}

describe('useCanvasGeneration', () => {
  it('exposes a user-facing generate action from the config node', async () => {
    const wrapper = mount(ConfigNode, { props: { node: { id: 'config', type: 'config', position: { x: 0, y: 0 }, metadata: { model: 'image-model' } } } })
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('generate')).toHaveLength(1)
  })

  it('persists generated blobs and creates completed image metadata', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    store.setActiveProject('project-1')
    const prompt = store.addNode({ type: 'prompt', position: { x: 10, y: 20 }, metadata: { prompt: 'A mountain' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 20 }, metadata: { model: 'image-model', size: '1024x1024', count: 1 } })
    const adapter = vi.fn(async (secret: string): Promise<GeneratedImage[]> => { expect(secret).toBe('memory-secret'); return [{ blob: new Blob(['png'], { type: 'image/png' }), mimeType: 'image/png', width: 10, height: 20 }] })
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'memory-secret', generate: adapter })
    const result = await generation.generateFromNodes(prompt.id, config.id)
    expect(adapter).toHaveBeenCalledWith('memory-secret', expect.objectContaining({ model: 'image-model', prompt: 'A mountain', count: 1 }))
    expect(repo.assets).toHaveLength(1)
    expect(result[0].metadata).toMatchObject({ status: 'completed', storageKey: 'asset-1', prompt: 'A mountain', model: 'image-model' })
    expect(JSON.stringify(store.activeProject.value)).not.toContain('memory-secret')
  })

  it('retains prompt/config metadata and maps forbidden responses to Chinese feedback', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Keep this prompt' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'restricted-model', quality: 'high' } })
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret', generate: vi.fn(async () => { throw new ImageGenerationError(403, 'forbidden', 'no permission') }) })
    const result = await generation.generateFromNodes(prompt.id, config.id)
    expect(result).toEqual([])
    const image = store.activeProject.value?.nodes.find((node) => node.type === 'image')
    expect(image?.metadata).toMatchObject({ status: 'failed', prompt: 'Keep this prompt', model: 'restricted-model', configNodeId: config.id, promptNodeId: prompt.id })
    expect(String(image?.metadata.error)).toContain('无权')
  })

  it('retries a failed image with its stored prompt and configuration', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Retry me' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'retry-model', size: '1K' } })
    const adapter = vi.fn().mockRejectedValueOnce(new ImageGenerationError(403, 'forbidden', 'no')).mockResolvedValueOnce([{ blob: new Blob(['ok'], { type: 'image/png' }), mimeType: 'image/png' }])
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret', generate: adapter })
    await generation.generateFromNodes(prompt.id, config.id)
    const failed = store.activeProject.value?.nodes.find((node) => node.type === 'image')!
    await generation.retryImageNode(failed.id)
    expect(adapter.mock.calls[1][1]).toMatchObject({ prompt: 'Retry me', model: 'retry-model', size: '1K' })
    expect(store.activeProject.value?.nodes.find((node) => node.id === failed.id)?.metadata.status).toBe('completed')
  })

  it('retries from the credential-free request snapshot after source nodes change', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Original prompt' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'original-model', quality: 'high' } })
    const adapter = vi.fn().mockRejectedValueOnce(new ImageGenerationError(403, 'forbidden', 'no')).mockResolvedValueOnce([{ blob: new Blob(['ok'], { type: 'image/png' }), mimeType: 'image/png' }])
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret', generate: adapter })
    await generation.generateFromNodes(prompt.id, config.id)
    const failed = store.activeProject.value?.nodes.find((node) => node.type === 'image')!
    store.updateNode(prompt.id, { metadata: { prompt: 'Edited prompt' } }); store.updateNode(config.id, { metadata: { model: 'edited-model', quality: 'low' } })
    await generation.retryImageNode(failed.id)
    expect(adapter.mock.calls[1][1]).toMatchObject({ prompt: 'Original prompt', model: 'original-model', quality: 'high' })
  })

  it('does not send a key when cancellation wins while key lookup is pending', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Cancel me' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model' } })
    let resolveSecret: ((secret: string) => void) | undefined
    const adapter = vi.fn().mockResolvedValue([{ blob: new Blob(['unexpected']), mimeType: 'image/png' }])
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => new Promise<string>((resolve) => { resolveSecret = resolve }), generate: adapter })
    const pending = generation.generateFromNodes(prompt.id, config.id)
    await vi.waitFor(() => expect(store.activeProject.value?.nodes.some((node) => node.type === 'image')).toBe(true))
    const image = store.activeProject.value?.nodes.find((node) => node.type === 'image')!
    generation.cancelGeneration(image.id); resolveSecret?.('secret'); await pending
    expect(adapter).not.toHaveBeenCalled()
    expect(image.metadata.status).toBe('failed')
  })

  it('cleans shared assets only after the final image reference is removed', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Shared' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model' } })
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret', generate: async () => [{ blob: new Blob(['ok']), mimeType: 'image/png' }] })
    const [created] = await generation.generateFromNodes(prompt.id, config.id)
    const shared = store.addNode({ type: 'image', position: { x: 500, y: 0 }, metadata: { status: 'completed', storageKey: created.metadata.storageKey, assetKey: created.metadata.assetKey } })
    await generation.removeImageNode(created.id); expect(repo.assets).toHaveLength(1)
    await generation.removeImageNode(shared.id); expect(repo.assets).toHaveLength(0)
  })

  it('marks every target failed and removes saved assets when a later save fails', async () => {
    const base = repository([fixture()]); let saves = 0
    const repo: CanvasRepository = { ...base.value, async saveAsset(asset) { saves += 1; if (saves === 2) throw new Error('disk full'); return base.value.saveAsset(asset) } }
    const store = useInfiniteCanvasStore(repo); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Two' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model', count: 2 } })
    const generation = useCanvasGeneration({ store, repository: repo, getKeySecret: () => 'secret', generate: async () => [{ blob: new Blob(['one']), mimeType: 'image/png' }, { blob: new Blob(['two']), mimeType: 'image/png' }] })
    await generation.generateFromNodes(prompt.id, config.id)
    expect(store.activeProject.value?.nodes.filter((node) => node.type === 'image').every((node) => node.metadata.status === 'failed')).toBe(true)
    expect(base.assets).toHaveLength(0)
  })

  it('dispatches Gemini keys to the Gemini adapter', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready; store.setActiveKey(7)
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Gemini' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'gemini-image' } })
    const gemini = vi.fn().mockResolvedValue([{ blob: new Blob(['ok']), mimeType: 'image/png' }])
    const generic = vi.fn()
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret', getKeyPlatform: () => 'gemini', generate: generic, generateGeminiImage: gemini })
    await generation.generateFromNodes(prompt.id, config.id)
    expect(gemini).toHaveBeenCalled(); expect(generic).not.toHaveBeenCalled()
  })
})
