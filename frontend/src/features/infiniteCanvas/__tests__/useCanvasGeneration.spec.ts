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

  it('aborts stale key selections before dispatching the old secret', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    store.setActiveKey(1)
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Stale key' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model' } })
    let resolveSecret: ((secret: string) => void) | undefined
    const adapter = vi.fn().mockResolvedValue([{ blob: new Blob(['unexpected']), mimeType: 'image/png' }])
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => new Promise<string>((resolve) => { resolveSecret = resolve }), getKeyPlatform: (id) => id === 1 ? 'openai' : 'gemini', generate: adapter })
    const pending = generation.generateFromNodes(prompt.id, config.id)
    await vi.waitFor(() => expect(store.activeProject.value?.nodes.some((node) => node.type === 'image')).toBe(true))
    store.setActiveKey(2); resolveSecret?.('old-secret'); await pending
    expect(adapter).not.toHaveBeenCalled()
    expect(store.activeProject.value?.nodes.find((node) => node.type === 'image')?.metadata.error).toContain('密钥选择已变更')
  })

  it('retains an old replacement asset when the new save fails', async () => {
    const repo = repository([fixture()]); repo.assets.push({ key: 'old-asset', blob: new Blob(['old']) })
    const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Replace' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model' } })
    const image = store.addNode({ type: 'image', position: { x: 500, y: 0 }, metadata: { status: 'failed', storageKey: 'old-asset', assetKey: 'old-asset', promptNodeId: prompt.id, configNodeId: config.id, requestSnapshot: { model: 'image-model', prompt: 'Replace' } } })
    const failingRepo: CanvasRepository = { ...repo.value, async saveAsset() { throw new Error('disk full') } }
    const generation = useCanvasGeneration({ store, repository: failingRepo, getKeySecret: () => 'secret', generate: async () => [{ blob: new Blob(['new']), mimeType: 'image/png' }] })
    await generation.retryImageNode(image.id)
    expect(store.activeProject.value?.nodes.find((node) => node.id === image.id)?.metadata.storageKey).toBe('old-asset')
    expect(repo.assets).toHaveLength(1)
  })

  it('fails closed for duplicate node IDs, project assetKeys, and repository scan errors', async () => {
    const second = fixture(); second.id = 'project-2'; second.assetKeys = ['shared']; second.nodes = [{ id: 'same-node', type: 'image', position: { x: 0, y: 0 }, metadata: { storageKey: 'shared' } }]
    const first = fixture(); first.nodes = [{ id: 'same-node', type: 'image', position: { x: 0, y: 0 }, metadata: { storageKey: 'shared' } }]
    const repo = repository([first, second]); repo.assets.push({ key: 'shared', blob: new Blob(['shared']) })
    const store = useInfiniteCanvasStore(repo.value); await store.ready
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret' })
    await generation.removeImageNode('same-node'); expect(repo.assets).toHaveLength(1)
    repo.value.listProjects = async () => { throw new Error('offline') }
    await generation.cleanupAsset('shared'); expect(repo.assets).toHaveLength(1)
  })

  it('does not let an older overlapping run clear the newer retry state', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Overlap' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model' } })
    const deferred: Array<(value: GeneratedImage[]) => void> = []
    const adapter = vi.fn(() => new Promise<GeneratedImage[]>((resolve) => deferred.push(resolve)))
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret', generate: adapter })
    const first = generation.generateFromNodes(prompt.id, config.id)
    await vi.waitFor(() => expect(adapter).toHaveBeenCalledTimes(1))
    const image = store.activeProject.value?.nodes.find((node) => node.type === 'image')!
    const second = generation.retryImageNode(image.id)
    await vi.waitFor(() => expect(adapter).toHaveBeenCalledTimes(2))
    deferred[0]([{ blob: new Blob(['old']), mimeType: 'image/png' }]); deferred[1]([{ blob: new Blob(['new']), mimeType: 'image/png' }])
    await Promise.all([first, second])
    expect(store.activeProject.value?.nodes.find((node) => node.id === image.id)?.metadata.status).toBe('completed')
  })

  it('cancels an in-flight result before deleting its image node and leaves no orphan asset', async () => {
    const repo = repository([fixture()]); const store = useInfiniteCanvasStore(repo.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Delete during generation' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model' } })
    let resolveResult: ((result: GeneratedImage[]) => void) | undefined
    const adapter = vi.fn(() => new Promise<GeneratedImage[]>((resolve) => { resolveResult = resolve }))
    const generation = useCanvasGeneration({ store, repository: repo.value, getKeySecret: () => 'secret', generate: adapter })
    const pending = generation.generateFromNodes(prompt.id, config.id)
    await vi.waitFor(() => expect(adapter).toHaveBeenCalledTimes(1))
    const image = store.activeProject.value?.nodes.find((node) => node.type === 'image')!
    await generation.removeImageNode(image.id)
    resolveResult?.([{ blob: new Blob(['late']), mimeType: 'image/png' }]); await pending
    expect(store.activeProject.value?.nodes.some((node) => node.id === image.id)).toBe(false)
    expect(repo.assets).toHaveLength(0)
  })

  it('cancels the whole run when a later multi-result node is deleted during save', async () => {
    const base = repository([fixture()]); const store = useInfiniteCanvasStore(base.value); await store.ready
    const prompt = store.addNode({ type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Delete second' } })
    const config = store.addNode({ type: 'config', position: { x: 200, y: 0 }, metadata: { model: 'image-model', count: 2 } })
    let saveCount = 0; let releaseSecond: (() => void) | undefined
    const repo: CanvasRepository = { ...base.value, async saveAsset(asset) { saveCount += 1; if (saveCount === 2) await new Promise<void>((resolve) => { releaseSecond = resolve }); return base.value.saveAsset(asset) } }
    const generation = useCanvasGeneration({ store, repository: repo, getKeySecret: () => 'secret', generate: async () => [{ blob: new Blob(['one']), mimeType: 'image/png' }, { blob: new Blob(['two']), mimeType: 'image/png' }] })
    const pending = generation.generateFromNodes(prompt.id, config.id)
    await vi.waitFor(() => expect(saveCount).toBe(2))
    const images = store.activeProject.value?.nodes.filter((node) => node.type === 'image') ?? []
    expect(images).toHaveLength(2)
    await generation.removeImageNode(images[1].id); releaseSecond?.(); await pending
    expect(store.activeProject.value?.nodes.some((node) => node.id === images[1].id)).toBe(false)
    expect(base.assets).toHaveLength(0)
    expect(store.activeProject.value?.nodes.find((node) => node.id === images[0].id)?.metadata.status).toBe('failed')
  })
})
