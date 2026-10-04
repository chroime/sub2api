import { describe, expect, it, vi } from 'vitest'
import { CANVAS_SCHEMA_VERSION, type CanvasProject, type CanvasRepository } from '../types'
import { useInfiniteCanvasStore } from '../stores/useInfiniteCanvasStore'

function fixture(overrides: Partial<CanvasProject> = {}): CanvasProject {
  const date = new Date('2026-01-01T00:00:00.000Z')
  return {
    id: 'project-1', title: 'Initial', createdAt: date, updatedAt: date,
    viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid',
    nodes: [], edges: [], ...overrides,
  }
}

function repository(projects: CanvasProject[] = []): CanvasRepository & { saves: CanvasProject[]; state: CanvasProject[] } {
  const state = projects.map((project) => structuredClone(project))
  const saves: CanvasProject[] = []
  return {
    state,
    saves,
    async listProjects() { return state.map((project) => structuredClone(project)) },
    async loadProject(id) { return state.find((project) => project.id === id) ?? null },
    async saveProject(project) { saves.push(structuredClone(project)); const index = state.findIndex((item) => item.id === project.id); if (index >= 0) state[index] = structuredClone(project); else state.push(structuredClone(project)) },
    async deleteProject(id) { const index = state.findIndex((project) => project.id === id); if (index >= 0) state.splice(index, 1) },
    async saveAsset() { return 'asset' },
    async loadAsset() { return undefined },
    async deleteAsset() {},
  }
}

describe('useInfiniteCanvasStore', () => {
  it('hydrates projects and switches the active project', async () => {
    const store = useInfiniteCanvasStore(repository([fixture(), fixture({ id: 'project-2', title: 'Second' })]))
    await store.ready
    expect(store.projects.value).toHaveLength(2)
    expect(store.activeProject.value?.id).toBe('project-1')
    store.setActiveProject('project-2')
    expect(store.activeProject.value?.title).toBe('Second')
  })

  it('creates, updates, removes and connects nodes while preserving metadata', async () => {
    const store = useInfiniteCanvasStore(repository())
    await store.ready
    const project = store.createProject('Canvas')
    const first = store.addNode({ type: 'prompt', position: { x: 10, y: 20 }, metadata: { text: 'Hello', future: { value: true } } })
    const second = store.addNode({ type: 'config', position: { x: 200, y: 20 }, metadata: { model: 'gpt' } })
    store.updateNode(first.id, { position: { x: 30, y: 40 }, metadata: { text: 'Updated' } })
    expect(store.activeProject.value?.nodes[0].metadata).toEqual({ text: 'Updated', future: { value: true } })
    const edge = store.connectNodes(first.id, second.id, 'prompt')
    expect(edge.sourceNodeId).toBe(first.id)
    expect(store.activeProject.value?.edges).toHaveLength(1)
    store.removeNode(second.id)
    expect(store.activeProject.value?.nodes.map((node) => node.id)).toEqual([first.id])
    expect(store.activeProject.value?.edges).toHaveLength(0)
    expect(project.id).toBe(store.activeProject.value?.id)
  })

  it('supports undo and redo and persists updated timestamps', async () => {
    vi.useFakeTimers()
    const repo = repository([fixture()])
    const store = useInfiniteCanvasStore(repo)
    await store.ready
    const before = store.activeProject.value!.updatedAt.getTime()
    store.renameProject('Renamed')
    expect(store.activeProject.value?.title).toBe('Renamed')
    expect(store.activeProject.value!.updatedAt.getTime()).toBeGreaterThanOrEqual(before)
    const renamedAt = store.activeProject.value!.updatedAt.getTime()
    store.undo()
    expect(store.activeProject.value?.title).toBe('Initial')
    expect(store.activeProject.value!.updatedAt.getTime()).toBeGreaterThan(renamedAt)
    const undoneAt = store.activeProject.value!.updatedAt.getTime()
    store.redo()
    expect(store.activeProject.value?.title).toBe('Renamed')
    expect(store.activeProject.value!.updatedAt.getTime()).toBeGreaterThan(undoneAt)
    vi.advanceTimersByTime(100)
    await Promise.resolve()
    expect(repo.saves.length).toBeGreaterThan(0)
    vi.useRealTimers()
  })

  it('persists a mutated project even when the active project changes before debounce flush', async () => {
    vi.useFakeTimers()
    const repo = repository([fixture(), fixture({ id: 'project-2', title: 'Second' })])
    const store = useInfiniteCanvasStore(repo)
    await store.ready
    store.renameProject('First updated')
    store.setActiveProject('project-2')
    vi.advanceTimersByTime(100)
    await Promise.resolve()
    expect(repo.saves.some((project) => project.id === 'project-1' && project.title === 'First updated')).toBe(true)
    vi.useRealTimers()
  })

  it('replaces an inactive project debounce before saving its latest direct rename', async () => {
    vi.useFakeTimers()
    const repo = repository([fixture(), fixture({ id: 'project-2', title: 'Second' })])
    const store = useInfiniteCanvasStore(repo)
    await store.ready
    store.renameProject('First pending')
    store.setActiveProject('project-2')
    store.renameProject('project-1', 'First direct')
    vi.advanceTimersByTime(100)
    await Promise.resolve()
    expect(repo.saves.filter((project) => project.id === 'project-1')).toEqual([expect.objectContaining({ title: 'First direct' })])
    vi.useRealTimers()
  })

  it('persists viewport and background mutations and can flush them immediately', async () => {
    vi.useFakeTimers()
    const repo = repository([fixture()])
    const store = useInfiniteCanvasStore(repo)
    await store.ready
    store.updateViewport({ x: 10, y: 20, zoom: 1.5 })
    store.setBackgroundMode('dots')
    await store.saveProject()
    expect(repo.saves.at(-1)).toMatchObject({ viewport: { x: 10, y: 20, zoom: 1.5 }, backgroundMode: 'dots' })
    const reloaded = useInfiniteCanvasStore(repo)
    await reloaded.ready
    expect(reloaded.activeProject.value?.viewport).toEqual({ x: 10, y: 20, zoom: 1.5 })
    expect(reloaded.activeProject.value?.backgroundMode).toBe('dots')
    expect(store.canUndo.value).toBe(true)
    vi.useRealTimers()
  })

  it('validates and imports a versioned project without changing state on malformed data', async () => {
    const repo = repository([fixture()])
    const store = useInfiniteCanvasStore(repo)
    await store.ready
    const imported = store.importProject({
      schemaVersion: CANVAS_SCHEMA_VERSION,
      id: 'imported', title: 'Imported', createdAt: '2026-01-02T00:00:00.000Z', updatedAt: '2026-01-02T00:00:00.000Z',
      viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'plain', nodes: [], edges: [],
    })
    expect(imported?.id).toBe('imported')
    expect(store.activeProject.value?.id).toBe('imported')
    expect(store.activeProject.value?.createdAt).toBeInstanceOf(Date)
    const before = store.activeProject.value?.id
    expect(store.importProject({ schemaVersion: CANVAS_SCHEMA_VERSION, id: 'bad', title: 'bad', key: 'secret' })).toBeUndefined()
    expect(store.activeProject.value?.id).toBe(before)
  })
})
