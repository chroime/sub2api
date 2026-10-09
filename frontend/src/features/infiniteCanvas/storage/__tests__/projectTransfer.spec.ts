import { describe, expect, it } from 'vitest'
import { unzipSync, strFromU8, strToU8, zipSync } from 'fflate'
import type { CanvasAsset, CanvasProject, CanvasRepository } from '../../types'
import { exportProject, importProject, sanitizeExportProject } from '../projectTransfer'
import { useInfiniteCanvasStore } from '../../stores/useInfiniteCanvasStore'

function project(): CanvasProject {
  const now = new Date('2026-10-04T00:00:00.000Z')
  return {
    id: 'project-original', title: 'Secret export', createdAt: now, updatedAt: now,
    viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid', activeKeyId: 7,
    nodes: [{ id: 'image-node', type: 'image', position: { x: 1, y: 2 }, metadata: { assetKey: 'asset-original', storageKey: 'asset-original', apiKey: 'do-not-export' } }],
    edges: [], assetKeys: ['asset-original'],
  }
}

function repository(initial: CanvasProject[] = [], initialAssets: Record<string, CanvasAsset> = {}): CanvasRepository & { projects: CanvasProject[]; assets: Record<string, CanvasAsset> } {
  const projects = [...initial]
  const assets = { ...initialAssets }
  return {
    projects, assets,
    async listProjects() { return projects },
    async loadProject(id) { return projects.find((item) => item.id === id) ?? null },
    async saveProject(item) { const index = projects.findIndex((entry) => entry.id === item.id); if (index >= 0) projects[index] = item; else projects.push(item) },
    async deleteProject(id) { const index = projects.findIndex((entry) => entry.id === id); if (index >= 0) projects.splice(index, 1) },
    async saveAsset(asset) { const key = asset.storageKey ?? `saved-${Object.keys(assets).length + 1}`; assets[key] = { ...asset, storageKey: key }; return key },
    async loadAsset(key) { return assets[key] },
    async deleteAsset(key) { delete assets[key] },
  }
}

describe('project transfer', () => {
  it('exports project JSON and referenced asset bytes without secrets and imports with remapped IDs', async () => {
    const source = repository([], { 'asset-original': { storageKey: 'asset-original', blob: new Blob(['asset-bytes'], { type: 'image/png' }), mimeType: 'image/png', kind: 'image', projectId: 'project-original' } })
    const archive = await exportProject(project(), source)
    const archiveBytes = await new Promise<ArrayBuffer>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result as ArrayBuffer); reader.onerror = () => reject(reader.error); reader.readAsArrayBuffer(archive) })
    const files = unzipSync(new Uint8Array(archiveBytes))
    const exported = JSON.parse(strFromU8(files['project.json'])) as CanvasProject
    expect(exported.id).toBe('project-original')
    expect(exported.activeKeyId).toBe(7)
    expect(JSON.stringify(exported)).not.toContain('do-not-export')
    expect(files['assets/asset-original']).toBeTruthy()
    expect(strFromU8(files['assets/asset-original'])).toBe('asset-bytes')

    const target = repository()
    const imported = await importProject(archive, target)
    expect(imported.schemaVersion).toBe(1)
    expect(imported.id).not.toBe(project().id)
    expect(imported.nodes[0].metadata.assetKey).not.toBe('asset-original')
    expect(imported.nodes[0].metadata.assetKey).toBe(imported.assetKeys?.[0])
    expect(Object.keys(target.assets)).toContain(imported.assetKeys?.[0] as string)
    expect(await target.loadAsset(imported.assetKeys?.[0] as string)).toBeTruthy()
    const store = useInfiniteCanvasStore(target)
    await store.ready
    expect(store.activeProject.value?.id).toBe(imported.id)
    expect((await target.loadProject(imported.id))?.schemaVersion).toBe(1)
  })

  it('sanitizes secret fields recursively while preserving active key id', () => {
    const sanitized = sanitizeExportProject(project()) as Record<string, unknown>
    expect(sanitized.activeKeyId).toBe(7)
    expect(JSON.stringify(sanitized)).not.toMatch(/apiKey|do-not-export|activeKeySecret/)
  })

  it('preserves prompt reference image data URLs across export and import', async () => {
    const reference = { dataUrl: 'data:image/png;base64,YQ==', mimeType: 'image/png', name: 'reference.png' }
    const sourceProject: CanvasProject = {
      ...project(),
      id: 'prompt-project',
      nodes: [{ id: 'prompt-node', type: 'prompt', position: { x: 0, y: 0 }, metadata: { prompt: 'Use [参考图1]', referenceImages: [reference] } }],
      edges: [],
      assetKeys: [],
    }
    const archive = await exportProject(sourceProject, repository())
    const target = repository()
    const imported = await importProject(archive, target)
    expect(imported.nodes[0].metadata.referenceImages).toEqual([reference])
  })

  it('rejects an export when a referenced asset is missing', async () => {
    await expect(exportProject(project(), repository())).rejects.toThrow('Missing asset')
  })

  it('rolls back assets if saving the imported project fails', async () => {
    const source = repository([], { 'asset-original': { storageKey: 'asset-original', blob: new Blob(['asset-bytes'], { type: 'image/png' }), mimeType: 'image/png', kind: 'image', projectId: 'project-original' } })
    const archive = await exportProject(project(), source)
    const target = repository()
    target.saveProject = async () => { throw new Error('project write failed') }
    await expect(importProject(archive, target)).rejects.toThrow('project write failed')
    expect(Object.keys(target.assets)).toHaveLength(0)
    expect(target.projects).toHaveLength(0)
  })

  it('rejects unsafe extra archive entries before repository writes', async () => {
    const source = repository([], { 'asset-original': { storageKey: 'asset-original', blob: new Blob(['asset-bytes'], { type: 'image/png' }), mimeType: 'image/png', kind: 'image', projectId: 'project-original' } })
    const archive = await exportProject(project(), source)
    const bytes = await new Promise<ArrayBuffer>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(reader.result as ArrayBuffer); reader.onerror = () => reject(reader.error); reader.readAsArrayBuffer(archive) })
    const original = unzipSync(new Uint8Array(bytes)); original['../extra'] = strToU8('bad')
    const malformed = new Blob([zipSync(original)], { type: 'application/zip' })
    const target = repository()
    await expect(importProject(malformed, target)).rejects.toThrow()
    expect(Object.keys(target.assets)).toHaveLength(0)
  })
})
