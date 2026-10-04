import { ref, toRaw } from 'vue'
import type { CanvasEdge, CanvasNode, CanvasProject, CanvasRepository } from '../types'
import { useCanvasHistory } from '../composables/useCanvasHistory'

function clone<T>(value: T): T {
  const raw = toRaw(value)
  if (raw instanceof Date) return new Date(raw.getTime()) as T
  if (Array.isArray(raw)) return raw.map((item) => clone(item)) as T
  if (raw && typeof raw === 'object') return Object.fromEntries(Object.entries(raw).map(([key, item]) => [key, clone(item)])) as T
  return raw
}

function id(prefix: string): string {
  return typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function' ? `${prefix}-${crypto.randomUUID()}` : `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export function useInfiniteCanvasStore(repository: CanvasRepository) {
  const projects = ref<CanvasProject[]>([])
  const activeProject = ref<CanvasProject | null>(null)
  const selectedNodeIds = ref<string[]>([])
  const activeKeyId = ref<number | undefined>(undefined)
  const history = useCanvasHistory<CanvasProject>(50)
  let saveTimer: ReturnType<typeof setTimeout> | undefined

  const persist = () => {
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      const project = activeProject.value
      if (project) void repository.saveProject(clone(project))
    }, 25)
  }

  const hydrate = async () => {
    const loaded = await repository.listProjects()
    projects.value = loaded.map(clone)
    activeProject.value = projects.value[0] ?? null
    activeKeyId.value = activeProject.value?.activeKeyId
  }
  const ready = hydrate()

  const replaceActive = (project: CanvasProject | null) => {
    activeProject.value = project
    if (project) {
      const index = projects.value.findIndex((item) => item.id === project.id)
      if (index >= 0) projects.value[index] = project
    }
    activeKeyId.value = project?.activeKeyId
  }

  const mutate = (change: (project: CanvasProject) => void) => {
    const project = activeProject.value
    if (!project) return
    history.push(clone(project))
    change(project)
    project.updatedAt = new Date()
    replaceActive(project)
    persist()
  }

  const createProject = (title = 'Untitled canvas'): CanvasProject => {
    const now = new Date()
    const project: CanvasProject = { id: id('project'), title, createdAt: now, updatedAt: now, viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid', nodes: [], edges: [] }
    projects.value.push(project)
    replaceActive(project)
    history.clear()
    persist()
    return project
  }

  const renameProject = (titleOrId: string, maybeTitle?: string) => {
    const project = maybeTitle ? projects.value.find((item) => item.id === titleOrId) : activeProject.value
    if (!project) return
    const title = maybeTitle ?? titleOrId
    if (project.id === activeProject.value?.id) mutate((item) => { item.title = title })
    else {
      project.title = title
      project.updatedAt = new Date()
      void repository.saveProject(clone(project))
    }
  }

  const duplicateProject = (projectId = activeProject.value?.id): CanvasProject | undefined => {
    const source = projects.value.find((item) => item.id === projectId)
    if (!source) return undefined
    const copy = clone(source)
    copy.id = id('project')
    copy.title = `${source.title} copy`
    copy.createdAt = new Date()
    copy.updatedAt = new Date()
    projects.value.push(copy)
    replaceActive(copy)
    history.clear()
    persist()
    return copy
  }

  const deleteProject = async (projectId = activeProject.value?.id) => {
    if (!projectId) return
    const index = projects.value.findIndex((item) => item.id === projectId)
    if (index < 0) return
    projects.value.splice(index, 1)
    await repository.deleteProject(projectId)
    if (activeProject.value?.id === projectId) replaceActive(projects.value[index] ?? projects.value[index - 1] ?? null)
    selectedNodeIds.value = []
    history.clear()
  }

  const setActiveProject = (projectId: string) => {
    const project = projects.value.find((item) => item.id === projectId)
    if (project) { replaceActive(project); selectedNodeIds.value = []; history.clear() }
  }

  const addNode = (node: Omit<CanvasNode, 'id'> & { id?: string }): CanvasNode => {
    const created = { ...clone(node), id: node.id ?? id('node') } as CanvasNode
    mutate((project) => { project.nodes.push(created) })
    return created
  }

  const updateNode = (nodeId: string, patch: Partial<CanvasNode>) => mutate((project) => {
    const node = project.nodes.find((item) => item.id === nodeId)
    if (!node) return
    const { metadata, ...rest } = clone(patch)
    Object.assign(node, rest)
    if (metadata) node.metadata = { ...node.metadata, ...metadata }
  })

  const removeNode = (nodeId: string) => mutate((project) => {
    project.nodes = project.nodes.filter((node) => node.id !== nodeId)
    project.edges = project.edges.filter((edge) => edge.sourceNodeId !== nodeId && edge.targetNodeId !== nodeId)
    selectedNodeIds.value = selectedNodeIds.value.filter((idValue) => idValue !== nodeId)
  })

  const connectNodes = (sourceNodeIdOrEdge: string | Pick<CanvasEdge, 'sourceNodeId' | 'targetNodeId' | 'kind'>, targetNodeId?: string, kind: CanvasEdge['kind'] = 'reference', metadata?: Record<string, unknown>): CanvasEdge => {
    const sourceNodeId = typeof sourceNodeIdOrEdge === 'string' ? sourceNodeIdOrEdge : sourceNodeIdOrEdge.sourceNodeId
    const target = typeof sourceNodeIdOrEdge === 'string' ? targetNodeId : sourceNodeIdOrEdge.targetNodeId
    const edgeKind = typeof sourceNodeIdOrEdge === 'string' ? kind : sourceNodeIdOrEdge.kind
    const edge: CanvasEdge = { id: id('edge'), sourceNodeId, targetNodeId: target!, kind: edgeKind, ...(metadata ? { metadata: clone(metadata) } : {}) }
    mutate((project) => { if (sourceNodeId !== target && !project.edges.some((item) => item.sourceNodeId === sourceNodeId && item.targetNodeId === target)) project.edges.push(edge) })
    return edge
  }

  const setActiveKey = (keyId: number | undefined) => mutate((project) => { project.activeKeyId = keyId })

  const undo = () => {
    const project = activeProject.value
    if (!project) return
    const snapshot = history.undo(clone(project))
    if (snapshot) { replaceActive(snapshot); persist() }
  }
  const redo = () => {
    const project = activeProject.value
    if (!project) return
    const snapshot = history.redo(clone(project))
    if (snapshot) { replaceActive(snapshot); persist() }
  }

  return { projects, activeProject, selectedNodeIds, activeKeyId, ready, createProject, renameProject, duplicateProject, deleteProject, setActiveProject, addNode, updateNode, removeNode, connectNodes, setActiveKey, undo, redo, canUndo: history.canUndo, canRedo: history.canRedo }
}
