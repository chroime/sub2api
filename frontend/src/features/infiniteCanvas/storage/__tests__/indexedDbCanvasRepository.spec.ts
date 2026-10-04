import { describe, expect, it } from 'vitest'
import 'fake-indexeddb/auto'
import { createIndexedDbCanvasRepository } from '../indexedDbCanvasRepository'
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
    edges: [{ id: 'edge-1', source: 'prompt-1', target: 'config-1', kind: 'data' }],
    ...overrides,
  }
}

describe('indexed db canvas repository', () => {
  it('round trips project metadata without an API key secret', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    const project = makeFixtureProject({ activeKeyId: 7 })
    await repo.saveProject(project)
    const loaded = await repo.loadProject(project.id)
    expect(loaded).toMatchObject(project)
    expect(JSON.stringify(loaded)).not.toContain('sk-secret')
    expect(loaded?.createdAt).toBeInstanceOf(Date)
    expect(loaded?.viewport).toEqual(project.viewport)
    expect(loaded?.edges[0].kind).toBe('data')
  })

  it('saves, reloads, and deletes asset bytes', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    const bytes = new Uint8Array([137, 80, 78, 71])
    const saved = await repo.saveAsset({ blob: new Blob([bytes], { type: 'image/png' }), mimeType: 'image/png', kind: 'image' })
    expect(saved.storageKey).toBeTruthy()
    const loaded = await repo.loadAsset(saved.storageKey!)
    expect(loaded?.mimeType).toBe('image/png')
    const result = await new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as ArrayBuffer)
      reader.onerror = () => reject(reader.error)
      reader.readAsArrayBuffer(loaded!.blob)
    })
    expect(new Uint8Array(result)).toEqual(bytes)
    await repo.deleteAsset(saved.storageKey!)
    expect(await repo.loadAsset(saved.storageKey!)).toBeUndefined()
  })

  it('rejects an unknown schema version with a typed error', async () => {
    const repo = createIndexedDbCanvasRepository(`test-${crypto.randomUUID()}`)
    await repo.__unsafePutProjectRecord({ id: 'bad', schemaVersion: 999, payload: {} as CanvasProject })
    await expect(repo.loadProject('bad')).rejects.toBeInstanceOf(Error)
  })
})
