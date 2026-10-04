import { zipSync, unzipSync, strFromU8, strToU8 } from 'fflate'
import type { CanvasNode, CanvasProject, CanvasRepository } from '../types'
import { CANVAS_SCHEMA_VERSION } from '../types'

const MAX_ARCHIVE_BYTES = 100 * 1024 * 1024
const SECRET_FIELDS = new Set(['key', 'apiKey', 'api_key', 'secret', 'token', 'access_token', 'activeKeySecret'])
const NODE_TYPES = new Set(['prompt', 'config', 'image'])
const MIME_RE = /^[\w.+-]+\/[\w.+-]+$/

function cloneAndSanitize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(cloneAndSanitize)
  if (!value || typeof value !== 'object' || value instanceof Blob || value instanceof Date) return value
  return Object.fromEntries(Object.entries(value).filter(([key]) => !SECRET_FIELDS.has(key)).map(([key, child]) => [key, cloneAndSanitize(child)]))
}

export function sanitizeExportProject(project: CanvasProject): CanvasProject {
  return cloneAndSanitize(project) as CanvasProject
}

function id(prefix: string): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return `${prefix}-${crypto.randomUUID()}`
  return `${prefix}-${Math.random().toString(36).slice(2)}-${Date.now()}`
}

function referencedKeys(project: CanvasProject): string[] {
  return [...new Set([...(project.assetKeys ?? []), ...project.nodes.flatMap((node) => node.type === 'image' && typeof node.metadata.assetKey === 'string' ? [node.metadata.assetKey] : []), ...project.nodes.flatMap((node) => node.type === 'image' && typeof node.metadata.storageKey === 'string' ? [node.metadata.storageKey] : [])])]
}

function assertStorageKey(key: string) {
  if (!key || key.length > 500 || key.includes('/') || key.includes('\\') || key === '.' || key === '..') throw new Error('Invalid asset storage key')
}

function blobBytes(blob: Blob): Promise<Uint8Array> {
  if (typeof blob.arrayBuffer === 'function') return blob.arrayBuffer().then((buffer) => new Uint8Array(buffer))
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer))
    reader.onerror = () => reject(reader.error)
    reader.readAsArrayBuffer(blob)
  })
}

function asProject(value: unknown): CanvasProject {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid canvas project')
  const input = value as Record<string, unknown>
  if (input.schemaVersion !== CANVAS_SCHEMA_VERSION || typeof input.id !== 'string' || typeof input.title !== 'string' || !Array.isArray(input.nodes) || !Array.isArray(input.edges)) throw new Error('Unsupported canvas archive')
  const nodes = input.nodes as CanvasNode[]
  const nodeIds = new Set<string>()
  for (const node of nodes) {
    if (!node || typeof node.id !== 'string' || nodeIds.has(node.id) || !NODE_TYPES.has(node.type) || !node.position || typeof node.metadata !== 'object') throw new Error('Invalid canvas node')
    nodeIds.add(node.id)
  }
  for (const edge of input.edges) {
    if (!edge || typeof edge !== 'object' || typeof edge.id !== 'string' || !nodeIds.has((edge as { sourceNodeId?: string }).sourceNodeId ?? '') || !nodeIds.has((edge as { targetNodeId?: string }).targetNodeId ?? '')) throw new Error('Invalid canvas edge')
  }
  const parsed = { ...input, createdAt: new Date(String(input.createdAt)), updatedAt: new Date(String(input.updatedAt)), nodes, edges: input.edges } as CanvasProject
  if (Number.isNaN(parsed.createdAt.getTime()) || Number.isNaN(parsed.updatedAt.getTime())) throw new Error('Invalid canvas dates')
  return parsed
}

export async function exportProject(project: CanvasProject, repository: CanvasRepository): Promise<Blob> {
  const sanitized = sanitizeExportProject(project)
  const manifest: Record<string, { mimeType: string; kind: string; width?: number; height?: number }> = {}
  const files: Record<string, Uint8Array> = {}
  for (const key of referencedKeys(project)) {
    assertStorageKey(key)
    const asset = await repository.loadAsset(key)
    if (!asset) continue
    if (!MIME_RE.test(asset.mimeType || asset.blob.type)) throw new Error('Invalid asset MIME type')
    files[`assets/${key}`] = await blobBytes(asset.blob)
    manifest[key] = { mimeType: asset.mimeType || asset.blob.type, kind: asset.kind, width: asset.width, height: asset.height }
  }
  files['project.json'] = strToU8(JSON.stringify({ schemaVersion: CANVAS_SCHEMA_VERSION, ...sanitized, assetManifest: manifest }))
  return new Blob([zipSync(files)], { type: 'application/zip' })
}

export async function importProject(file: Blob, repository: CanvasRepository): Promise<CanvasProject> {
  if (file.size > MAX_ARCHIVE_BYTES) throw new Error('Canvas archive exceeds 100 MiB')
  const bytes = await blobBytes(file)
  if (bytes.byteLength > MAX_ARCHIVE_BYTES) throw new Error('Canvas archive exceeds 100 MiB')
  const files = unzipSync(bytes)
  const json = files['project.json']
  if (!json) throw new Error('Missing project.json')
  const raw = JSON.parse(strFromU8(json)) as Record<string, unknown>
  const manifest = (raw.assetManifest && typeof raw.assetManifest === 'object' ? raw.assetManifest : {}) as Record<string, { mimeType?: string; kind?: string; width?: number; height?: number }>
  const project = asProject(raw)
  const oldProjectId = project.id
  const newProjectId = id('project')
  const keyMap = new Map<string, string>()
  for (const key of referencedKeys(project)) {
    assertStorageKey(key)
    const data = files[`assets/${key}`]
    if (!data) throw new Error(`Missing asset ${key}`)
    const metadata = manifest[key]
    if (!metadata || typeof metadata.mimeType !== 'string' || !MIME_RE.test(metadata.mimeType)) throw new Error('Invalid asset MIME type')
    const nextKey = id('asset')
    keyMap.set(key, nextKey)
    await repository.saveAsset({ storageKey: nextKey, blob: new Blob([data], { type: metadata.mimeType }), mimeType: metadata.mimeType, kind: metadata.kind ?? 'image', width: metadata.width, height: metadata.height, projectId: newProjectId })
  }
  const remapped = JSON.parse(JSON.stringify(project)) as CanvasProject
  remapped.id = newProjectId
  remapped.createdAt = new Date(project.createdAt)
  remapped.updatedAt = new Date()
  remapped.assetKeys = project.assetKeys?.map((key) => keyMap.get(key) ?? key)
  remapped.nodes = project.nodes.map((node) => node.type === 'image' ? { ...node, metadata: { ...node.metadata, ...(typeof node.metadata.assetKey === 'string' ? { assetKey: keyMap.get(node.metadata.assetKey) } : {}), ...(typeof node.metadata.storageKey === 'string' ? { storageKey: keyMap.get(node.metadata.storageKey) } : {}) } } : node)
  await repository.saveProject(remapped)
  void oldProjectId
  return remapped
}

