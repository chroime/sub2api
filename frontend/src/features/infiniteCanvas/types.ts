export const CANVAS_SCHEMA_VERSION = 1 as const

export type CanvasBackgroundMode = 'grid' | 'dots' | 'plain'
export type CanvasNodeType = 'prompt' | 'config' | 'image'

export interface CanvasViewport {
  x: number
  y: number
  zoom: number
}

export interface CanvasPoint {
  x: number
  y: number
}

export interface CanvasSize {
  width: number
  height: number
}

export interface PromptNodeMetadata {
  text?: string
  [key: string]: unknown
}

export interface ConfigNodeMetadata {
  model?: string
  parameters?: Record<string, unknown>
  [key: string]: unknown
}

export interface ImageNodeMetadata {
  status?: 'pending' | 'ready' | 'error' | 'completed' | 'failed'
  url?: string
  assetKey?: string
  [key: string]: unknown
}

export type CanvasNodeMetadata = PromptNodeMetadata | ConfigNodeMetadata | ImageNodeMetadata

export interface CanvasNode {
  id: string
  type: CanvasNodeType
  position: CanvasPoint
  size?: CanvasSize
  metadata: CanvasNodeMetadata
}

export interface CanvasEdge {
  id: string
  sourceNodeId: string
  targetNodeId: string
  kind: 'prompt' | 'config' | 'reference'
  metadata?: Record<string, unknown>
}

export interface CanvasProject {
  /** Persisted schema marker. Older in-memory fixtures may omit this field. */
  schemaVersion?: typeof CANVAS_SCHEMA_VERSION
  id: string
  title: string
  createdAt: Date
  updatedAt: Date
  viewport: CanvasViewport
  backgroundMode: CanvasBackgroundMode
  activeKeyId?: number
  nodes: CanvasNode[]
  edges: CanvasEdge[]
  assetKeys?: string[]
}

export interface CanvasAsset {
  storageKey?: string
  blob: Blob
  mimeType: string
  width?: number
  height?: number
  kind: string
  projectId?: string
}

export interface CanvasRepository {
  listProjects(): Promise<CanvasProject[]>
  loadProject(id: string): Promise<CanvasProject | null>
  saveProject(project: CanvasProject): Promise<void>
  deleteProject(id: string): Promise<void>
  saveAsset(asset: CanvasAsset): Promise<string>
  loadAsset(storageKey: string): Promise<CanvasAsset | undefined>
  deleteAsset(storageKey: string): Promise<void>
}

export class CanvasSchemaError extends Error {
  readonly schemaVersion: unknown

  constructor(schemaVersion: unknown) {
    super(`Unsupported canvas schema version: ${String(schemaVersion)}`)
    this.name = 'CanvasSchemaError'
    this.schemaVersion = schemaVersion
  }
}
