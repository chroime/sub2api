import {
  CANVAS_SCHEMA_VERSION,
  CanvasSchemaError,
  type CanvasAsset,
  type CanvasProject,
  type CanvasRepository,
} from '../types'

interface ProjectRecord {
  id: string
  schemaVersion: number
  payload: CanvasProject
}

interface AssetRecord extends CanvasAsset {
  storageKey: string
  blobData: ArrayBuffer
}

const PROJECTS_STORE = 'projects'
const ASSETS_STORE = 'assets'

function clone<T>(value: T): T {
  return typeof structuredClone === 'function' ? structuredClone(value) : JSON.parse(JSON.stringify(value)) as T
}

function stripSecrets(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(stripSecrets)
  if (!value || typeof value !== 'object' || value instanceof Blob || value instanceof Date) return value
  return Object.fromEntries(Object.entries(value).filter(([key]) => !['key', 'apiKey', 'secret', 'token'].includes(key)).map(([key, child]) => [key, stripSecrets(child)]))
}

function request<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onerror = () => reject(transaction.error)
    transaction.onabort = () => reject(transaction.error ?? new Error('IndexedDB transaction aborted'))
  })
}

function blobToArrayBuffer(blob: Blob): Promise<ArrayBuffer> {
  if (typeof blob.arrayBuffer === 'function') return blob.arrayBuffer()
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.onerror = () => reject(reader.error)
    reader.readAsArrayBuffer(blob)
  })
}

export function createIndexedDbCanvasRepository(databaseName = 'sub2api-infinite-canvas'): CanvasRepository & {
  __unsafePutProjectRecord(record: ProjectRecord): Promise<void>
} {
  let databasePromise: Promise<IDBDatabase> | undefined
  const openDatabase = () => databasePromise ??= new Promise((resolve, reject) => {
    const open = indexedDB.open(databaseName, 1)
    open.onupgradeneeded = () => {
      const database = open.result
      if (!database.objectStoreNames.contains(PROJECTS_STORE)) database.createObjectStore(PROJECTS_STORE, { keyPath: 'id' })
      if (!database.objectStoreNames.contains(ASSETS_STORE)) database.createObjectStore(ASSETS_STORE, { keyPath: 'storageKey' })
    }
    open.onsuccess = () => resolve(open.result)
    open.onerror = () => reject(open.error)
  })

  return {
    async listProjects() {
      const database = await openDatabase()
      const records = await request(database.transaction(PROJECTS_STORE).objectStore(PROJECTS_STORE).getAll()) as ProjectRecord[]
      return records.map((record) => validate(record).payload).map(clone)
    },
    async loadProject(id) {
      const database = await openDatabase()
      const record = await request(database.transaction(PROJECTS_STORE).objectStore(PROJECTS_STORE).get(id)) as ProjectRecord | undefined
      return record ? clone(validate(record).payload) : undefined
    },
    async saveProject(project) {
      const database = await openDatabase()
      const transaction = database.transaction(PROJECTS_STORE, 'readwrite')
      transaction.objectStore(PROJECTS_STORE).put({ id: project.id, schemaVersion: CANVAS_SCHEMA_VERSION, payload: stripSecrets(clone(project)) as CanvasProject })
      await transactionDone(transaction)
    },
    async deleteProject(id) {
      const database = await openDatabase()
      const transaction = database.transaction([PROJECTS_STORE, ASSETS_STORE], 'readwrite')
      const project = await request(transaction.objectStore(PROJECTS_STORE).get(id)) as ProjectRecord | undefined
      transaction.objectStore(PROJECTS_STORE).delete(id)
      if (project?.payload.assetKeys) project.payload.assetKeys.forEach((key) => transaction.objectStore(ASSETS_STORE).delete(key))
      await transactionDone(transaction)
    },
    async saveAsset(asset) {
      const blobData = await blobToArrayBuffer(asset.blob)
      const saved: AssetRecord = { ...clone(asset), blobData, storageKey: asset.storageKey ?? crypto.randomUUID() }
      const database = await openDatabase()
      const transaction = database.transaction(ASSETS_STORE, 'readwrite')
      transaction.objectStore(ASSETS_STORE).put(saved)
      await transactionDone(transaction)
      return { ...saved, blob: new Blob([blobData], { type: saved.mimeType }) }
    },
    async loadAsset(storageKey) {
      const database = await openDatabase()
      const asset = await request(database.transaction(ASSETS_STORE).objectStore(ASSETS_STORE).get(storageKey)) as AssetRecord | undefined
      return asset ? { ...asset, blob: new Blob([asset.blobData], { type: asset.mimeType }) } : undefined
    },
    async deleteAsset(storageKey) {
      const database = await openDatabase()
      const transaction = database.transaction(ASSETS_STORE, 'readwrite')
      transaction.objectStore(ASSETS_STORE).delete(storageKey)
      await transactionDone(transaction)
    },
    async __unsafePutProjectRecord(record) {
      const database = await openDatabase()
      const transaction = database.transaction(PROJECTS_STORE, 'readwrite')
      transaction.objectStore(PROJECTS_STORE).put(record)
      await transactionDone(transaction)
    },
  }
}

function validate(record: ProjectRecord): ProjectRecord {
  if (record.schemaVersion !== CANVAS_SCHEMA_VERSION) throw new CanvasSchemaError(record.schemaVersion)
  return record
}
