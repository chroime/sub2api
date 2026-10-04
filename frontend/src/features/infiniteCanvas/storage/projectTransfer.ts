import { zipSync, unzipSync, strFromU8, strToU8 } from 'fflate'
import type { CanvasNode, CanvasProject, CanvasRepository } from '../types'
import { CANVAS_SCHEMA_VERSION } from '../types'

const MAX_ARCHIVE_BYTES = 100 * 1024 * 1024
const MAX_ENTRIES = 2000
const MAX_TITLE = 200
const MAX_STRING = 500
const SECRET_FIELDS = new Set(['key', 'apiKey', 'api_key', 'secret', 'token', 'access_token', 'activeKeySecret', 'authorization', 'accessToken', 'bearerToken'])
const NODE_TYPES = new Set(['prompt', 'config', 'image'])
const EDGE_KINDS = new Set(['prompt', 'config', 'reference'])
const IMAGE_MIMES = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/avif', 'image/svg+xml'])

function cloneAndSanitize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(cloneAndSanitize)
  if (!value || typeof value !== 'object' || value instanceof Blob || value instanceof Date) return value
  return Object.fromEntries(Object.entries(value).filter(([key]) => !SECRET_FIELDS.has(key)).map(([key, child]) => [key, cloneAndSanitize(child)]))
}
export function sanitizeExportProject(project: CanvasProject): CanvasProject { return cloneAndSanitize(project) as CanvasProject }
function id(prefix: string): string { return typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function' ? `${prefix}-${crypto.randomUUID()}` : `${prefix}-${Math.random().toString(36).slice(2)}-${Date.now()}` }
function finite(value: unknown): value is number { return typeof value === 'number' && Number.isFinite(value) }
function safeKey(key: unknown): key is string { return typeof key === 'string' && key.length > 0 && key.length <= MAX_STRING && !/[\\/\0]/.test(key) && key !== '.' && key !== '..' }
function hasSecret(value: unknown): boolean {
  if (Array.isArray(value)) return value.some(hasSecret)
  if (!value || typeof value !== 'object' || value instanceof Blob || value instanceof Date) return false
  return Object.entries(value).some(([key, child]) => SECRET_FIELDS.has(key) || (typeof child === 'string' && /^Bearer\s+/i.test(child)) || hasSecret(child))
}
function referencedKeys(project: CanvasProject): string[] { return [...new Set([...(project.assetKeys ?? []), ...project.nodes.flatMap((node) => node.type === 'image' && typeof node.metadata.assetKey === 'string' ? [node.metadata.assetKey] : []), ...project.nodes.flatMap((node) => node.type === 'image' && typeof node.metadata.storageKey === 'string' ? [node.metadata.storageKey] : [])])] }
function blobBytes(blob: Blob): Promise<Uint8Array> {
  if (typeof blob.arrayBuffer === 'function') return blob.arrayBuffer().then((buffer) => new Uint8Array(buffer))
  return new Promise((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(new Uint8Array(reader.result as ArrayBuffer)); reader.onerror = () => reject(reader.error); reader.readAsArrayBuffer(blob) })
}
function validAssetMeta(value: unknown): value is { storageKey: string; mimeType: string; kind: string; width?: number; height?: number } {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  const item = value as Record<string, unknown>
  return safeKey(item.storageKey) && typeof item.mimeType === 'string' && IMAGE_MIMES.has(item.mimeType) && typeof item.kind === 'string' && item.kind.length > 0 && item.kind.length <= 100 && (item.width === undefined || (finite(item.width) && item.width >= 0 && item.width <= 100000)) && (item.height === undefined || (finite(item.height) && item.height >= 0 && item.height <= 100000))
}
function normalizeProject(value: unknown): CanvasProject {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid canvas project')
  const input = value as Record<string, unknown>
  if (input.schemaVersion !== CANVAS_SCHEMA_VERSION || typeof input.id !== 'string' || !input.id || input.id.length > MAX_STRING || typeof input.title !== 'string' || !input.title.trim() || input.title.length > MAX_TITLE || hasSecret(input)) throw new Error('Invalid canvas project')
  const iso = (value: unknown) => typeof value === 'string' && /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{3})?Z$/.test(value)
  const createdAt = new Date(String(input.createdAt)); const updatedAt = new Date(String(input.updatedAt)); const viewport = input.viewport
  if (!iso(input.createdAt) || !iso(input.updatedAt) || Number.isNaN(createdAt.getTime()) || Number.isNaN(updatedAt.getTime()) || !viewport || typeof viewport !== 'object' || viewport === null || !finite((viewport as Record<string, unknown>).x) || !finite((viewport as Record<string, unknown>).y) || !finite((viewport as Record<string, unknown>).zoom) || (viewport as Record<string, number>).zoom <= 0 || (viewport as Record<string, number>).zoom > 10 || !['grid', 'dots', 'plain'].includes(String(input.backgroundMode))) throw new Error('Invalid canvas project')
  if (!Array.isArray(input.nodes) || !Array.isArray(input.edges) || input.nodes.length > 10000 || input.edges.length > 20000) throw new Error('Invalid canvas project')
  const nodeIds = new Set<string>(); const nodes: CanvasNode[] = []
  for (const raw of input.nodes) {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('Invalid canvas node')
    const node = raw as Record<string, unknown>; const position = node.position; const size = node.size; const metadata = node.metadata
    if (typeof node.id !== 'string' || !node.id.trim() || node.id.length > MAX_STRING || nodeIds.has(node.id) || !NODE_TYPES.has(String(node.type)) || !position || typeof position !== 'object' || position === null || !finite((position as Record<string, unknown>).x) || !finite((position as Record<string, unknown>).y) || !metadata || typeof metadata !== 'object' || metadata === null || Array.isArray(metadata) || (size !== undefined && (!size || typeof size !== 'object' || size === null || !finite((size as Record<string, unknown>).width) || !finite((size as Record<string, unknown>).height) || (size as Record<string, number>).width <= 0 || (size as Record<string, number>).height <= 0 || (size as Record<string, number>).width > 100000 || (size as Record<string, number>).height > 100000))) throw new Error('Invalid canvas node')
    nodeIds.add(node.id); nodes.push({ id: node.id, type: node.type as CanvasNode['type'], position: { x: (position as Record<string, number>).x, y: (position as Record<string, number>).y }, ...(size ? { size: { width: (size as Record<string, number>).width, height: (size as Record<string, number>).height } } : {}), metadata: metadata as CanvasNode['metadata'] })
  }
  const edgeIds = new Set<string>(); const edges = input.edges.map((raw) => {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) throw new Error('Invalid canvas edge')
    const edge = raw as Record<string, unknown>; const metadata = edge.metadata
    if (typeof edge.id !== 'string' || !edge.id.trim() || edge.id.length > MAX_STRING || edgeIds.has(edge.id) || typeof edge.sourceNodeId !== 'string' || typeof edge.targetNodeId !== 'string' || !nodeIds.has(edge.sourceNodeId) || !nodeIds.has(edge.targetNodeId) || !EDGE_KINDS.has(String(edge.kind)) || (metadata !== undefined && (!metadata || typeof metadata !== 'object' || metadata === null || Array.isArray(metadata)))) throw new Error('Invalid canvas edge')
    edgeIds.add(edge.id); return { id: edge.id, sourceNodeId: edge.sourceNodeId, targetNodeId: edge.targetNodeId, kind: edge.kind as 'prompt' | 'config' | 'reference', ...(metadata ? { metadata: metadata as Record<string, unknown> } : {}) }
  })
  const assetKeys = input.assetKeys
  if (assetKeys !== undefined && (!Array.isArray(assetKeys) || new Set(assetKeys).size !== assetKeys.length || assetKeys.some((key) => !safeKey(key)))) throw new Error('Invalid asset keys')
  if (input.activeKeyId !== undefined && (!finite(input.activeKeyId) || !Number.isInteger(input.activeKeyId) || input.activeKeyId < 0)) throw new Error('Invalid active key id')
  return { id: input.id, title: input.title, createdAt, updatedAt, viewport: { x: (viewport as Record<string, number>).x, y: (viewport as Record<string, number>).y, zoom: (viewport as Record<string, number>).zoom }, backgroundMode: input.backgroundMode as CanvasProject['backgroundMode'], ...(input.activeKeyId !== undefined ? { activeKeyId: input.activeKeyId as number } : {}), nodes, edges, ...(assetKeys ? { assetKeys: [...assetKeys] } : {}) }
}
function zipEntries(bytes: Uint8Array): string[] {
  const names: string[] = []; const seen = new Set<string>(); let count = 0; let total = 0; let compressedTotal = 0
  let eocd = -1
  for (let i = Math.max(0, bytes.length - 65557); i + 22 <= bytes.length; i += 1) if (bytes[i] === 0x50 && bytes[i + 1] === 0x4b && bytes[i + 2] === 0x05 && bytes[i + 3] === 0x06) eocd = i
  if (eocd < 0) throw new Error('Missing ZIP directory')
  const end = new DataView(bytes.buffer, bytes.byteOffset + eocd, bytes.byteLength - eocd); const countExpected = end.getUint16(10, true); const directorySize = end.getUint32(12, true); const directoryOffset = end.getUint32(16, true); if (directorySize === 0xffffffff || directoryOffset === 0xffffffff || directoryOffset + directorySize > eocd) throw new Error('Invalid ZIP directory')
  let offset = directoryOffset; const directoryEnd = directoryOffset + directorySize
  while (offset < directoryEnd) {
    if (offset + 46 > directoryEnd || bytes[offset] !== 0x50 || bytes[offset + 1] !== 0x4b || bytes[offset + 2] !== 0x01 || bytes[offset + 3] !== 0x02) throw new Error('Invalid ZIP directory entry')
    count += 1; if (count > MAX_ENTRIES) throw new Error('Too many archive entries')
    const view = new DataView(bytes.buffer, bytes.byteOffset + offset, bytes.byteLength - offset); const compressed = view.getUint32(20, true); const uncompressed = view.getUint32(24, true); const nameLength = view.getUint16(28, true); const extraLength = view.getUint16(30, true); const commentLength = view.getUint16(32, true)
    if (uncompressed === 0xffffffff || compressed === 0xffffffff) throw new Error('Unsupported ZIP entry')
    total += uncompressed; compressedTotal += compressed; if (total > MAX_ARCHIVE_BYTES || compressedTotal > MAX_ARCHIVE_BYTES) throw new Error('Canvas archive exceeds 100 MiB')
    const name = new TextDecoder().decode(bytes.subarray(offset + 46, offset + 46 + nameLength)); if (seen.has(name) || !name || name.includes('..') || name.startsWith('/') || name.includes('\\') || name.includes('\0')) throw new Error('Invalid archive path')
    if (name !== 'project.json' && !/^assets\/[^/]+$/.test(name)) throw new Error('Unexpected archive entry')
    seen.add(name); names.push(name); offset += 46 + nameLength + extraLength + commentLength
  }
  if (count !== countExpected || offset !== directoryEnd || !seen.has('project.json')) throw new Error('Invalid ZIP directory')
  return names
}
export async function exportProject(project: CanvasProject, repository: CanvasRepository): Promise<Blob> {
  const sanitizedInput = sanitizeExportProject(project); const normalized = normalizeProject({ schemaVersion: CANVAS_SCHEMA_VERSION, ...sanitizedInput, createdAt: project.createdAt.toISOString(), updatedAt: project.updatedAt.toISOString() }); const sanitized = sanitizeExportProject(normalized); const manifest: Record<string, { storageKey: string; mimeType: string; kind: string; width?: number; height?: number }> = {}; const files: Record<string, Uint8Array> = {}
  for (const key of referencedKeys(normalized)) { if (!safeKey(key)) throw new Error('Invalid asset storage key'); const asset = await repository.loadAsset(key); if (!asset) throw new Error(`Missing asset ${key}`); if (!validAssetMeta({ storageKey: key, mimeType: asset.mimeType, kind: asset.kind, width: asset.width, height: asset.height }) || asset.mimeType !== asset.blob.type) throw new Error('Invalid asset MIME type'); files[`assets/${key}`] = await blobBytes(asset.blob); manifest[key] = { storageKey: key, mimeType: asset.mimeType, kind: asset.kind, width: asset.width, height: asset.height } }
  files['project.json'] = strToU8(JSON.stringify({ schemaVersion: CANVAS_SCHEMA_VERSION, ...sanitized, assetManifest: manifest })); return new Blob([zipSync(files)], { type: 'application/zip' })
}
export async function importProject(file: Blob, repository: CanvasRepository): Promise<CanvasProject> {
  if (file.size > MAX_ARCHIVE_BYTES) throw new Error('Canvas archive exceeds 100 MiB'); const bytes = await blobBytes(file); if (bytes.byteLength > MAX_ARCHIVE_BYTES) throw new Error('Canvas archive exceeds 100 MiB')
  const entryNames = zipEntries(bytes); const files = unzipSync(bytes); const json = files['project.json']; if (!json) throw new Error('Missing project.json'); const raw = JSON.parse(strFromU8(json)) as Record<string, unknown>; const project = normalizeProject(raw); const rawManifest = raw.assetManifest
  if (!rawManifest || typeof rawManifest !== 'object' || rawManifest === null || Array.isArray(rawManifest)) throw new Error('Invalid asset manifest')
  const manifest = rawManifest as Record<string, unknown>; const references = referencedKeys(project); const manifestKeys = Object.keys(manifest); if (manifestKeys.length !== references.length || manifestKeys.some((key) => !safeKey(key) || ['__proto__', 'constructor', 'prototype'].includes(key) || !references.includes(key))) throw new Error('Asset manifest does not match project references')
  const expectedEntries = new Set(['project.json', ...references.map((key) => `assets/${key}`)]); if (entryNames.length !== expectedEntries.size || entryNames.some((name) => !expectedEntries.has(name))) throw new Error('Archive entries do not match manifest')
  const prepared: { key: string; nextKey: string; blob: Blob; mimeType: string; kind: string; width?: number; height?: number }[] = []
  for (const key of references) { const metadata = manifest[key]; if (!validAssetMeta(metadata) || metadata.storageKey !== key) throw new Error('Invalid asset manifest entry'); const data = files[`assets/${key}`]; if (!data) throw new Error(`Missing asset ${key}`); const blob = new Blob([data], { type: metadata.mimeType }); if (blob.type !== metadata.mimeType) throw new Error('Asset MIME mismatch'); prepared.push({ key, nextKey: id('asset'), blob, ...metadata }) }
  const existingProjects = await repository.listProjects(); let newProjectId = id('project'); while (existingProjects.some((item) => item.id === newProjectId)) newProjectId = id('project')
  const generatedKeys = new Set<string>(); for (const item of prepared) { while (generatedKeys.has(item.nextKey) || await repository.loadAsset(item.nextKey)) item.nextKey = id('asset'); generatedKeys.add(item.nextKey) }
  const keyMap = new Map(prepared.map((item) => [item.key, item.nextKey])); const remapped = { ...project, id: newProjectId, updatedAt: new Date(), assetKeys: project.assetKeys?.map((key) => keyMap.get(key) as string), nodes: project.nodes.map((node) => node.type === 'image' ? { ...node, metadata: { ...node.metadata, ...(typeof node.metadata.assetKey === 'string' ? { assetKey: keyMap.get(node.metadata.assetKey) } : {}), ...(typeof node.metadata.storageKey === 'string' ? { storageKey: keyMap.get(node.metadata.storageKey) } : {}) } } : node) }
  const attempted = prepared.map((item) => item.nextKey)
  try { for (const item of prepared) await repository.saveAsset({ storageKey: item.nextKey, blob: item.blob, mimeType: item.mimeType, kind: item.kind, width: item.width, height: item.height, projectId: newProjectId }); await repository.saveProject(remapped); return remapped } catch (error) { const failures: unknown[] = []; try { await repository.deleteProject(newProjectId) } catch (cleanupError) { failures.push(cleanupError) }; for (const key of attempted) { try { await repository.deleteAsset(key) } catch (cleanupError) { failures.push(cleanupError) } } if (failures.length) throw new Error(`Canvas import rollback failed: ${String(failures[0])}`); throw error }
}
