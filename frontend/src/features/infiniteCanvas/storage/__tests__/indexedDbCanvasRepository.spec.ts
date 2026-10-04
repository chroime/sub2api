import { describe, expect, it } from 'vitest'
import 'fake-indexeddb/auto'
import { canvasDatabaseNameForUser, createIndexedDbCanvasRepository, createIndexedDbCanvasRepositoryForUser } from '../indexedDbCanvasRepository'
import type { CanvasProject } from '../../types'

function makeFixtureProject(overrides: Partial<CanvasProject> = {}): CanvasProject {
  const now = new Date('2026-01-02T03:04:05.000Z')
  return {
    id: 'project-1', title: 'Canvas', createdAt: now, updatedAt: now,
    viewport: { x: 12, y: -8, zoom: 1.25 }, backgroundMode: 'grid', activeKeyId: undefined,
    nodes: [
      { id: 'prompt-1', type: 'prompt', position: { x: 1, y: 2 }, size: { width: 240, height: 120 }, metadata: { text: 'Hello' } },
      { id: 'config-1', type: 'config', position: { x: 3, y: 4 }, metadata: { model: 'gpt-4o' } },
      { id: 'image-1', type: 'image', position: { x: 5, y: 6 }, metadata: { status: 'ready', url: 'blob:one' } },
    ],
    edges: [{ id: 'edge-1', sourceNodeId: 'prompt-1', targetNodeId: 'config-1', kind: 'prompt' }],
    ...overrides,
  }
}

describe('indexed db canvas repository', () => {
  it('namespaces databases by user without persisting an email', () => {
    const first = canvasDatabaseNameForUser(101)
    const second = canvasDatabaseNameForUser(202)
    expect(first).not.toBe(second)
    expect(first).not.toContain('@')
    expect(createIndexedDbCanvasRepositoryForUser(101)).toBeTruthy()
  })
  it('round trips project metadata without an API key secret', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    const project = makeFixtureProject({ activeKeyId: 7 })
    const polluted = makeFixtureProject({ activeKeyId: 7 })
    polluted.nodes[0].metadata = {
      text: 'Hello', key: 'k', apiKey: 'api', secret: 's', token: 't', access_token: 'at', activeKeySecret: 'aks',
      nested: { key: 'nested-k', apiKey: 'nested-api', secret: 'nested-s', token: 'nested-t', access_token: 'nested-at', activeKeySecret: 'nested-aks' },
    }
    await repo.saveProject(polluted)
    const loaded = await repo.loadProject(project.id)
    expect(loaded).toMatchObject({ ...project, nodes: [{ ...project.nodes[0], metadata: { text: 'Hello' } }, ...project.nodes.slice(1)] })
    expect(JSON.stringify(loaded)).not.toMatch(/key|apiKey|secret|token|access_token|activeKeySecret/)
    expect(loaded?.createdAt).toBeInstanceOf(Date)
    expect(loaded?.viewport).toEqual(project.viewport)
    expect(loaded?.edges[0].kind).toBe('prompt')
  })

  it('saves, reloads, and deletes asset bytes', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    const bytes = new Uint8Array([137, 80, 78, 71])
    const storageKey = await repo.saveAsset({ blob: new Blob([bytes], { type: 'image/png' }), mimeType: 'image/png', kind: 'image' })
    expect(storageKey).toBeTruthy()
    const loaded = await repo.loadAsset(storageKey)
    expect(loaded?.mimeType).toBe('image/png')
    const result = await new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as ArrayBuffer)
      reader.onerror = () => reject(reader.error)
      reader.readAsArrayBuffer(loaded!.blob)
    })
    expect(new Uint8Array(result)).toEqual(bytes)
    await repo.deleteAsset(storageKey)
    expect(await repo.loadAsset(storageKey)).toBeUndefined()
  })

  it('deletes owned assets while retaining assets referenced by another project', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    const ownedKey = await repo.saveAsset({ blob: new Blob(['owned']), mimeType: 'text/plain', kind: 'text', projectId: 'project-1' })
    const sharedKey = await repo.saveAsset({ blob: new Blob(['shared']), mimeType: 'text/plain', kind: 'text', projectId: 'project-1' })
    await repo.saveProject(makeFixtureProject({ assetKeys: [ownedKey, sharedKey] }))
    await repo.saveProject(makeFixtureProject({ id: 'project-2', assetKeys: [sharedKey] }))
    await repo.deleteProject('project-1')
    expect(await repo.loadAsset(ownedKey)).toBeUndefined()
    expect(await repo.loadAsset(sharedKey)).toBeTruthy()
  })

  it('retains an asset owned by another project even when its key is only listed by the deleted project', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    const key = await repo.saveAsset({ blob: new Blob(['owned-by-two']), mimeType: 'text/plain', kind: 'text', projectId: 'project-2' })
    await repo.saveProject(makeFixtureProject({ assetKeys: [key] }))
    await repo.saveProject(makeFixtureProject({ id: 'project-2' }))
    await repo.deleteProject('project-1')
    expect(await repo.loadAsset(key)).toBeTruthy()
  })

  it('deletes an asset whose project owner no longer exists', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    const key = await repo.saveAsset({ blob: new Blob(['orphan']), mimeType: 'text/plain', kind: 'text', projectId: 'missing-project' })
    await repo.saveProject(makeFixtureProject({ assetKeys: [key] }))
    await repo.deleteProject('project-1')
    expect(await repo.loadAsset(key)).toBeUndefined()
  })

  it('rejects an unknown schema version with a typed error', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    await repo.__unsafePutProjectRecord({ id: 'bad', schemaVersion: 999, payload: {} as CanvasProject })
    await expect(repo.loadProject('bad')).rejects.toMatchObject({ name: 'CanvasSchemaError', schemaVersion: 999 })
  })
})
