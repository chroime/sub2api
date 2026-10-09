import { ref, toRaw } from 'vue'
import { CANVAS_SCHEMA_VERSION, type CanvasBackgroundMode, type CanvasEdge, type CanvasNode, type CanvasProject, type CanvasRepository } from '../types'
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

const IMPORT_MAX_NODES = 500
const IMPORT_MAX_EDGES = 1000
const IMPORT_MAX_TITLE_LENGTH = 200
const SECRET_FIELDS = new Set(['key', 'apiKey', 'secret', 'token', 'access_token', 'activeKeySecret'])

function containsSecret(value: unknown): boolean {
  if (Array.isArray(value)) return value.some(containsSecret)
  if (!value || typeof value !== 'object') return false
  return Object.entries(value).some(([key, child]) => SECRET_FIELDS.has(key) || containsSecret(child))
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function parseDate(value: unknown): Date | undefined {
  if (value instanceof Date && !Number.isNaN(value.getTime())) return new Date(value.getTime())
  if (typeof value === 'string' && !Number.isNaN(new Date(value).getTime())) return new Date(value)
  return undefined
}

function isCanvasEdgeAllowed(sourceType: CanvasNode['type'], targetType: CanvasNode['type'], kind: CanvasEdge['kind']): boolean {
  if (kind === 'prompt') return sourceType === 'prompt' && (targetType === 'config' || targetType === 'image')
  if (kind === 'config') return sourceType === 'config' && (targetType === 'image' || targetType === 'prompt')
  return sourceType === 'image' && (targetType === 'prompt' || targetType === 'image')
}

export function validateCanvasProjectImport(input: unknown): CanvasProject | undefined {
  if (!isRecord(input) || input.schemaVersion !== CANVAS_SCHEMA_VERSION || containsSecret(input)) return undefined
  const idValue = input.id
  const title = input.title
  const createdAt = parseDate(input.createdAt)
  const updatedAt = parseDate(input.updatedAt)
  const viewport = input.viewport
  const nodes = input.nodes
  const edges = input.edges
  const backgroundMode = input.backgroundMode
  const activeKeyId = input.activeKeyId
  const assetKeys = input.assetKeys
  if (typeof idValue !== 'string' || !idValue.trim() || idValue.length > 200 || typeof title !== 'string' || !title.trim() || title.length > IMPORT_MAX_TITLE_LENGTH || !createdAt || !updatedAt || !isRecord(viewport) || !isFiniteNumber(viewport.x) || !isFiniteNumber(viewport.y) || !isFiniteNumber(viewport.zoom) || viewport.zoom <= 0 || viewport.zoom > 10 || !Array.isArray(nodes) || nodes.length > IMPORT_MAX_NODES || !Array.isArray(edges) || edges.length > IMPORT_MAX_EDGES || !['grid', 'dots', 'plain'].includes(String(backgroundMode)) || (activeKeyId !== undefined && (!isFiniteNumber(activeKeyId) || !Number.isInteger(activeKeyId) || activeKeyId < 0)) || (assetKeys !== undefined && (!Array.isArray(assetKeys) || assetKeys.some((key) => typeof key !== 'string' || key.length > 500)))) return undefined
  const nodeIds = new Set<string>()
  const normalizedNodes: CanvasNode[] = []
  for (const candidate of nodes) {
    if (!isRecord(candidate) || typeof candidate.id !== 'string' || !candidate.id || nodeIds.has(candidate.id) || !['prompt', 'config', 'image'].includes(String(candidate.type)) || !isRecord(candidate.position) || !isFiniteNumber(candidate.position.x) || !isFiniteNumber(candidate.position.y) || !isRecord(candidate.metadata) || (candidate.size !== undefined && (!isRecord(candidate.size) || !isFiniteNumber(candidate.size.width) || !isFiniteNumber(candidate.size.height) || candidate.size.width <= 0 || candidate.size.height <= 0))) return undefined
    nodeIds.add(candidate.id)
    normalizedNodes.push({ id: candidate.id, type: candidate.type as CanvasNode['type'], position: { x: candidate.position.x, y: candidate.position.y }, ...(isRecord(candidate.size) ? { size: { width: candidate.size.width as number, height: candidate.size.height as number } } : {}), metadata: clone(candidate.metadata) })
  }
  const edgeIds = new Set<string>()
  const normalizedEdges: CanvasEdge[] = []
  for (const candidate of edges) {
    if (!isRecord(candidate) || typeof candidate.id !== 'string' || !candidate.id || edgeIds.has(candidate.id) || typeof candidate.sourceNodeId !== 'string' || typeof candidate.targetNodeId !== 'string' || !nodeIds.has(candidate.sourceNodeId) || !nodeIds.has(candidate.targetNodeId) || !['prompt', 'config', 'reference'].includes(String(candidate.kind)) || (candidate.metadata !== undefined && !isRecord(candidate.metadata))) return undefined
    edgeIds.add(candidate.id)
    normalizedEdges.push({ id: candidate.id, sourceNodeId: candidate.sourceNodeId, targetNodeId: candidate.targetNodeId, kind: candidate.kind as CanvasEdge['kind'], ...(isRecord(candidate.metadata) ? { metadata: clone(candidate.metadata) } : {}) })
  }
  return { schemaVersion: CANVAS_SCHEMA_VERSION, id: idValue, title, createdAt, updatedAt, viewport: { x: viewport.x, y: viewport.y, zoom: viewport.zoom }, backgroundMode: backgroundMode as CanvasProject['backgroundMode'], nodes: normalizedNodes, edges: normalizedEdges, ...(typeof activeKeyId === 'number' ? { activeKeyId } : {}), ...(Array.isArray(assetKeys) ? { assetKeys: [...assetKeys] } : {}) }
}

export function useInfiniteCanvasStore(repository: CanvasRepository) {
  let currentRepository = repository
  const projects = ref<CanvasProject[]>([])
  const activeProject = ref<CanvasProject | null>(null)
  const selectedNodeIds = ref<string[]>([])
  const activeKeyId = ref<number | undefined>(undefined)
  const history = useCanvasHistory<CanvasProject>(50)
  const saveTimers = new Map<string, ReturnType<typeof setTimeout>>()
  let repositoryGeneration = 0

  const cancelPersist = (projectId: string) => {
    const timer = saveTimers.get(projectId)
    if (timer) { clearTimeout(timer); saveTimers.delete(projectId) }
  }

  const persist = (project = activeProject.value) => {
    if (!project) return
    const snapshot = clone(project)
    cancelPersist(project.id)
    saveTimers.set(project.id, setTimeout(() => {
      saveTimers.delete(project.id)
      void currentRepository.saveProject(snapshot)
    }, 25)
    )
  }

  const saveProject = async (project = activeProject.value): Promise<void> => {
    if (!project) return
    cancelPersist(project.id)
    await currentRepository.saveProject(clone(project))
  }

  const hydrate = async (generation = repositoryGeneration): Promise<boolean> => {
    const repositoryForLoad = currentRepository
    const loaded = await repositoryForLoad.listProjects()
    if (generation !== repositoryGeneration || repositoryForLoad !== currentRepository) return false
    projects.value = loaded.map(clone)
    activeProject.value = projects.value[0] ?? null
    activeKeyId.value = activeProject.value?.activeKeyId
    return true
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
    const project: CanvasProject = { schemaVersion: CANVAS_SCHEMA_VERSION, id: id('project'), title, createdAt: now, updatedAt: now, viewport: { x: 0, y: 0, zoom: 1 }, backgroundMode: 'grid', nodes: [], edges: [] }
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
      cancelPersist(project.id)
      void currentRepository.saveProject(clone(project))
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
    cancelPersist(projectId)
    await currentRepository.deleteProject(projectId)
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
    const project = activeProject.value
    const source = project?.nodes.find((node) => node.id === sourceNodeId)
    const destination = project?.nodes.find((node) => node.id === target)
    if (!project || !source || !destination || sourceNodeId === target || !isCanvasEdgeAllowed(source.type, destination.type, edgeKind)) return edge
    const existing = project.edges.find((item) => item.sourceNodeId === sourceNodeId && item.targetNodeId === target)
    if (existing) return existing
    mutate((current) => { current.edges.push(edge) })
    return edge
  }

  const setActiveKey = (keyId: number | undefined) => mutate((project) => { project.activeKeyId = keyId })

  const updateViewport = (viewport: CanvasProject['viewport']) => mutate((project) => { project.viewport = { ...viewport } })
  const setBackgroundMode = (backgroundMode: CanvasBackgroundMode) => mutate((project) => { project.backgroundMode = backgroundMode })

  const importProject = (input: unknown): CanvasProject | undefined => {
    const imported = validateCanvasProjectImport(input)
    if (!imported || projects.value.some((project) => project.id === imported.id)) return undefined
    projects.value.push(imported)
    replaceActive(imported)
    selectedNodeIds.value = []
    activeKeyId.value = imported.activeKeyId
    history.clear()
    persist(imported)
    return imported
  }

  const switchRepository = async (nextRepository: CanvasRepository): Promise<boolean> => {
    const generation = ++repositoryGeneration
    for (const projectId of saveTimers.keys()) cancelPersist(projectId)
    currentRepository = nextRepository
    projects.value = []
    activeProject.value = null
    activeKeyId.value = undefined
    selectedNodeIds.value = []
    history.clear()
    return hydrate(generation)
  }

  const restoreWithFreshTimestamp = (snapshot: CanvasProject, previous: CanvasProject): CanvasProject => {
    const restored = clone(snapshot)
    restored.updatedAt = new Date(Math.max(Date.now(), previous.updatedAt.getTime() + 1))
    return restored
  }

  const undo = () => {
    const project = activeProject.value
    if (!project) return
    const snapshot = history.undo(clone(project))
    if (snapshot) { const restored = restoreWithFreshTimestamp(snapshot, project); replaceActive(restored); persist(restored) }
  }
  const redo = () => {
    const project = activeProject.value
    if (!project) return
    const snapshot = history.redo(clone(project))
    if (snapshot) { const restored = restoreWithFreshTimestamp(snapshot, project); replaceActive(restored); persist(restored) }
  }

  return { projects, activeProject, selectedNodeIds, activeKeyId, ready, switchRepository, createProject, renameProject, duplicateProject, deleteProject, setActiveProject, addNode, updateNode, removeNode, connectNodes, setActiveKey, updateViewport, setBackgroundMode, importProject, saveProject, undo, redo, canUndo: history.canUndo, canRedo: history.canRedo }
}
