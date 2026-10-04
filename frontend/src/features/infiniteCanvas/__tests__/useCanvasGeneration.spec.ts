import { describe, expect, it, vi } from 'vitest'
import { ImageGenerationError, type GeneratedImage } from '@/api/imageGeneration'
import type { CanvasProject, CanvasRepository } from '../types'
import { useInfiniteCanvasStore } from '../stores/useInfiniteCanvasStore'
import { useCanvasGeneration } from '../composables/useCanvasGeneration'

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
})
