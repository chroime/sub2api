import { ref, type Ref } from 'vue'
import { editImage, generateGeminiImage, generateImage, ImageGenerationError, type GeneratedImage, type ImageGenerationRequest } from '@/api/imageGeneration'
import { MAX_PROMPT_REFERENCE_IMAGES, MAX_PROMPT_REFERENCE_IMAGE_BYTES, MAX_PROMPT_REFERENCE_IMAGE_DATA_URL_LENGTH, type CanvasEdge, type CanvasNode, type CanvasProject, type CanvasRepository } from '../types'

type Adapter = (apiKey: string, request: ImageGenerationRequest) => Promise<GeneratedImage[]>

interface CanvasStoreContract {
  activeProject: Ref<CanvasProject | null>
  activeKeyId?: Ref<number | undefined>
  addNode(node: Omit<CanvasNode, 'id'> & { id?: string }): CanvasNode
  updateNode(nodeId: string, patch: Partial<CanvasNode>): void
  removeNode(nodeId: string): void
  connectNodes(sourceNodeId: string, targetNodeId: string, kind?: CanvasEdge['kind']): CanvasEdge
}

interface GenerationRun {
  cancelled: boolean
  superseded: boolean
  targetIds: string[]
  savedKeys: string[]
  replacedOldKeys: string[]
}

export interface CanvasGenerationOptions {
  store: CanvasStoreContract
  repository: CanvasRepository
  /** Returns the full credential from the page-scoped memory map. */
  getKeySecret?: (keyId: number | undefined) => string | undefined | Promise<string | undefined>
  /** Alias accepted for callers that name the credential an API key. */
  getApiKey?: (keyId: number | undefined) => string | undefined | Promise<string | undefined>
  getKeyPlatform?: (keyId: number | undefined) => string | undefined
  generate?: Adapter
  generateImage?: Adapter
  editImage?: Adapter
  generateGeminiImage?: Adapter
}

const generationError = (error: unknown): string => {
  const status = error instanceof ImageGenerationError ? error.status : typeof (error as { status?: unknown })?.status === 'number' ? (error as { status: number }).status : undefined
  if (status === 401) return 'API Key 已失效，请重新选择密钥。'
  if (status === 409 || (error instanceof ImageGenerationError && error.code === 'stale_key_selection')) return '密钥选择已变更，请重新发起生成。'
  if (status === 403) return '当前密钥所属分组或模型无权生成图片。'
  if (status === 402) return '余额不足或已达到配额，请检查账户额度。'
  if (status === 429) return '请求过于频繁，请稍后重试。'
  if (status !== undefined && status >= 500) return '图片生成服务暂时不可用，请稍后重试。'
  if (error instanceof TypeError || /network|fetch|failed to fetch/i.test(String((error as Error)?.message ?? error))) return '网络连接失败，请稍后重试。'
  if (error instanceof ImageGenerationError && error.code === 'missing_reference_image') return '参考图已失效或无法读取，请重新关联图片。'
  if (error instanceof ImageGenerationError && error.code === 'reference_image_too_large') return '参考图过大，请使用 5 MB 以内的图片。'
  if (error instanceof ImageGenerationError && error.code === 'invalid_reference_image') return '参考图格式不受支持，请使用 PNG、JPEG 或 WebP。'
  if (error instanceof ImageGenerationError && ['invalid_response', 'invalid_image_url', 'external_image_url'].includes(error.code ?? '')) return '图片数据解析失败，请重试。'
  return '图片生成失败，请稍后重试。'
}

function metadata(node: CanvasNode): Record<string, unknown> { return node.metadata as Record<string, unknown> }
function stringValue(value: unknown): string | undefined { return typeof value === 'string' && value.trim() ? value : undefined }
function numberValue(value: unknown): number | undefined { return typeof value === 'number' && Number.isFinite(value) ? value : typeof value === 'string' && value.trim() ? Number(value) : undefined }
export function useCanvasGeneration(options: CanvasGenerationOptions) {
  const active = new Set<string>()
  const runs = new Map<string, GenerationRun>()
  const activeIds = ref<string[]>([])
  const setActive = (nodeId: string, value: boolean) => {
    if (value) active.add(nodeId); else active.delete(nodeId)
    activeIds.value = [...active]
  }

  function isGenerating(nodeId: string): boolean { return active.has(nodeId) }

  async function hasAssetReference(storageKey: string): Promise<boolean> {
    let projects: CanvasProject[]
    try { projects = await options.repository.listProjects() } catch { return true }
    const active = options.store.activeProject.value
    const candidates = [active, ...projects.filter((project) => project.id !== active?.id)]
    return candidates.some((project) => Boolean(project && (project.assetKeys?.includes(storageKey) || project.nodes.some((node) => node.type === 'image' && (metadata(node).storageKey === storageKey || metadata(node).assetKey === storageKey)))))
  }

  async function cleanupAsset(storageKey: string): Promise<void> {
    try {
      if (!(await hasAssetReference(storageKey))) await options.repository.deleteAsset(storageKey)
    } catch {
      // Asset cleanup is fail-closed: a repository failure must retain the asset.
    }
  }

  async function removeImageNode(nodeId: string): Promise<void> {
    const project = options.store.activeProject.value
    const node = project?.nodes.find((candidate) => candidate.id === nodeId && candidate.type === 'image')
    const storageKey = node ? stringValue(metadata(node).storageKey) ?? stringValue(metadata(node).assetKey) : undefined
    cancelGeneration(nodeId)
    options.store.removeNode(nodeId)
    if (storageKey) await cleanupAsset(storageKey)
  }

  function resolveNode(nodeId: string | undefined, type: CanvasNode['type']): CanvasNode | undefined {
    const project = options.store.activeProject.value
    if (!project || !nodeId) return undefined
    const direct = project.nodes.find((node) => node.id === nodeId)
    if (direct?.type === type) return direct
    if (direct) {
      const incoming = project.edges.find((edge) => edge.targetNodeId === direct.id && edge.kind === type)
      const upstream = project.nodes.find((node) => node.id === incoming?.sourceNodeId && node.type === type)
      if (upstream) return upstream
    }
    return project.nodes.find((node) => node.id === nodeId && node.type === type)
  }

  function configRequest(configNode: CanvasNode, prompt: string): ImageGenerationRequest {
    const values = metadata(configNode)
    const parameters = values.parameters && typeof values.parameters === 'object' ? values.parameters as Record<string, unknown> : {}
    const model = stringValue(values.model) ?? 'gpt-image-1'
    const request: ImageGenerationRequest = { model, prompt }
    const size = stringValue(values.size) ?? stringValue(parameters.size)
    const quality = stringValue(values.quality) ?? stringValue(parameters.quality)
    const background = stringValue(values.background) ?? stringValue(parameters.background)
    const count = numberValue(values.count) ?? numberValue(parameters.count)
    if (size) request.size = size
    if (quality) request.quality = quality
    if (background) request.background = background
    if (count) request.count = Math.max(1, Math.min(10, Math.floor(count)))
    return request
  }

  function inlineReferenceImages(promptNode: CanvasNode): ImageGenerationRequest['referenceImages'] {
    const value = metadata(promptNode).referenceImages
    if (!Array.isArray(value)) return undefined
    const references = value.filter((reference): reference is NonNullable<ImageGenerationRequest['referenceImages']>[number] => {
      if (!reference || typeof reference !== 'object') return false
      const candidate = reference as Record<string, unknown>
      return typeof candidate.dataUrl === 'string' && /^data:image\/(png|jpeg|webp);base64,/i.test(candidate.dataUrl) && candidate.dataUrl.length <= MAX_PROMPT_REFERENCE_IMAGE_DATA_URL_LENGTH && typeof candidate.mimeType === 'string' && /^(image\/png|image\/jpeg|image\/webp)$/i.test(candidate.mimeType)
    }).slice(0, MAX_PROMPT_REFERENCE_IMAGES).map((reference) => ({ dataUrl: reference.dataUrl, mimeType: reference.mimeType, ...(typeof reference.name === 'string' ? { name: reference.name } : {}) }))
    return references.length ? references : undefined
  }

  async function blobToDataUrl(blob: Blob, mimeType: string): Promise<string> {
    if (typeof blob.arrayBuffer !== 'function' && typeof FileReader !== 'undefined') {
      return new Promise((resolve, reject) => {
        const reader = new FileReader()
        reader.onload = () => {
          const value = typeof reader.result === 'string' ? reader.result : ''
          if (value) resolve(value.replace(/^data:[^;]+;/i, `data:${mimeType};`))
          else reject(new Error('Unable to read reference image'))
        }
        reader.onerror = () => reject(reader.error ?? new Error('Unable to read reference image'))
        reader.readAsDataURL(blob)
      })
    }
    const buffer = await blob.arrayBuffer()
    const bytes = new Uint8Array(buffer)
    let binary = ''
    const chunkSize = 0x8000
    for (let index = 0; index < bytes.length; index += chunkSize) binary += String.fromCharCode(...bytes.subarray(index, index + chunkSize))
    const encoded = typeof btoa === 'function' ? btoa(binary) : Buffer.from(binary, 'binary').toString('base64')
    return `data:${mimeType};base64,${encoded}`
  }

  async function referenceImagesForPrompt(promptNode: CanvasNode, targetNodeId?: string): Promise<ImageGenerationRequest['referenceImages']> {
    const references = [...(inlineReferenceImages(promptNode) ?? [])]
    const project = options.store.activeProject.value
    const referenceTargets = new Set([promptNode.id, ...(targetNodeId ? [targetNodeId] : [])])
    const linkedImages = project?.edges
      .filter((edge) => referenceTargets.has(edge.targetNodeId) && edge.kind === 'reference')
      .map((edge) => project.nodes.find((node) => node.id === edge.sourceNodeId && node.type === 'image'))
      .filter((node): node is CanvasNode => Boolean(node)) ?? []
    const seenLinkedIds = new Set<string>()
    for (const imageNode of linkedImages) {
      if (seenLinkedIds.has(imageNode.id)) continue
      seenLinkedIds.add(imageNode.id)
      if (references.length >= MAX_PROMPT_REFERENCE_IMAGES) break
      const imageMetadata = metadata(imageNode)
      const storageKey = stringValue(imageMetadata.storageKey) ?? stringValue(imageMetadata.assetKey)
      if (!storageKey) throw new ImageGenerationError(400, 'missing_reference_image', 'Reference image asset is unavailable')
      const asset = await options.repository.loadAsset(storageKey)
      if (!asset?.blob) throw new ImageGenerationError(400, 'missing_reference_image', 'Reference image asset is unavailable')
      if (asset.blob.size > MAX_PROMPT_REFERENCE_IMAGE_BYTES) throw new ImageGenerationError(400, 'reference_image_too_large', 'Reference image is too large')
      const mimeType = asset.mimeType || asset.blob.type || 'image/png'
      if (!mimeType.toLowerCase().startsWith('image/')) throw new ImageGenerationError(400, 'invalid_reference_image', 'Reference image must be an image')
      references.push({ dataUrl: await blobToDataUrl(asset.blob, mimeType), mimeType, name: stringValue(imageMetadata.fileName) ?? `${imageNode.id}.png` })
    }
    return references.length ? references : undefined
  }

  async function requestForNodes(promptNode: CanvasNode, configNode: CanvasNode, targetNodeId?: string): Promise<ImageGenerationRequest> {
    const request = configRequest(configNode, stringValue(metadata(promptNode).prompt) ?? stringValue(metadata(promptNode).text) ?? '')
    const refs = await referenceImagesForPrompt(promptNode, targetNodeId)
    if (refs) request.referenceImages = refs
    return request
  }

  function referenceMetadata(request: ImageGenerationRequest): Record<string, unknown> {
    return request.referenceImages?.length ? { referenceImages: request.referenceImages } : {}
  }

  // Keep the original reference bytes on the generated image node so retry can
  // still work after the prompt is edited, while avoiding a second full copy in
  // the request snapshot that is cloned into history and persisted repeatedly.
  function requestSnapshot(request: ImageGenerationRequest): Record<string, unknown> {
    const { referenceImages: _referenceImages, ...snapshot } = request
    return snapshot
  }

  function adapterForKey(platform?: string, hasReferences = false): Adapter {
    if (platform?.toLowerCase() === 'gemini') return options.generateGeminiImage ?? generateGeminiImage
    if (hasReferences) return options.editImage ?? options.generate ?? options.generateImage ?? editImage
    return options.generate ?? options.generateImage ?? generateImage
  }

  async function saveImage(run: GenerationRun, nodeId: string, result: GeneratedImage, request: ImageGenerationRequest, promptNodeId: string, configNodeId: string, oldStorageKey?: string): Promise<string> {
    if (run.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled')
    const projectId = options.store.activeProject.value?.id
    const storageKey = await options.repository.saveAsset({ blob: result.blob, mimeType: result.mimeType || result.blob.type || 'image/png', width: result.width, height: result.height, kind: 'image', projectId })
    run.savedKeys.push(storageKey)
    if (run.cancelled) { await cleanupAsset(storageKey); if (run.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled') }
    options.store.updateNode(nodeId, { metadata: { status: 'completed', prompt: request.prompt, model: request.model, size: request.size, quality: request.quality, count: request.count, background: request.background, ...referenceMetadata(request), mimeType: result.mimeType, width: result.width, height: result.height, storageKey, assetKey: storageKey, promptNodeId, configNodeId, requestSnapshot: requestSnapshot(request), error: undefined } })
    if (oldStorageKey && oldStorageKey !== storageKey) run.replacedOldKeys.push(oldStorageKey)
    return storageKey
  }

  async function run(nodeId: string, promptNode: CanvasNode, configNode: CanvasNode, promptNodeId: string, configNodeId: string, snapshot?: ImageGenerationRequest): Promise<CanvasNode[]> {
    const project = options.store.activeProject.value
    if (!project) return []
    const target = project.nodes.find((node) => node.id === nodeId)
    if (!target) return []
    const request = snapshot ? { ...snapshot, referenceImages: snapshot.referenceImages ? snapshot.referenceImages.map((reference) => ({ ...reference })) : undefined } : await requestForNodes(promptNode, configNode, nodeId)
    const targetMetadata = metadata(target)
    const oldStorageKey = stringValue(targetMetadata.storageKey) ?? stringValue(targetMetadata.assetKey)
    options.store.updateNode(nodeId, { metadata: { status: 'pending', prompt: request.prompt, model: request.model, size: request.size, quality: request.quality, count: request.count, background: request.background, ...referenceMetadata(request), promptNodeId, configNodeId, requestSnapshot: requestSnapshot(request), error: undefined } })
    const previousRun = runs.get(nodeId)
    if (previousRun) { previousRun.cancelled = true; previousRun.superseded = true }
    const runState: GenerationRun = { cancelled: false, superseded: false, targetIds: [nodeId], savedKeys: [], replacedOldKeys: [] }
    runs.set(nodeId, runState); setActive(nodeId, true)
    try {
      const keyId = options.store.activeKeyId?.value ?? options.store.activeProject.value?.activeKeyId
      const platform = options.getKeyPlatform?.(keyId)
      const getter = options.getKeySecret ?? options.getApiKey
      const secret = getter ? await getter(keyId) : undefined
      if (runState.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled')
      const currentKeyId = options.store.activeKeyId?.value ?? options.store.activeProject.value?.activeKeyId
      if (currentKeyId !== keyId || options.getKeyPlatform?.(currentKeyId) !== platform) throw new ImageGenerationError(409, 'stale_key_selection', 'Key selection changed')
      if (!secret) throw new ImageGenerationError(401, 'missing_key', 'No image API key selected')
      const adapter = adapterForKey(platform, Boolean(request.referenceImages?.length))
      if (runState.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled')
      const results = await adapter(secret, request)
      if (runState.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled')
      if (!results.length || results.some((result) => !(result?.blob instanceof Blob))) throw new ImageGenerationError(200, 'invalid_response', 'Image response did not contain image data')
      const nodes: CanvasNode[] = []
      for (let index = 0; index < results.length; index += 1) {
        if (runState.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled')
        const result = results[index]
        const resultNode = index === 0 ? target : options.store.addNode({ type: 'image', position: { x: configNode.position.x + 320, y: configNode.position.y + index * 190 }, metadata: { status: 'pending', prompt: request.prompt, model: request.model, promptNodeId, configNodeId } })
        if (index > 0) runState.targetIds.push(resultNode.id)
        if (index > 0) { options.store.connectNodes(promptNodeId, resultNode.id, 'prompt'); options.store.connectNodes(configNodeId, resultNode.id, 'config') }
        await saveImage(runState, resultNode.id, result, request, promptNodeId, configNodeId, index === 0 ? oldStorageKey : undefined)
        nodes.push(resultNode)
      }
      for (const oldKey of runState.replacedOldKeys) await cleanupAsset(oldKey)
      return nodes
    } catch (error) {
      const message = runState.cancelled || (error instanceof ImageGenerationError && error.code === 'cancelled') ? '生成已取消。' : generationError(error)
      if (!runState.superseded) for (const targetId of runState.targetIds) options.store.updateNode(targetId, { metadata: { status: 'failed', prompt: request.prompt, model: request.model, ...referenceMetadata(request), promptNodeId, configNodeId, requestSnapshot: requestSnapshot(request), error: message, ...(targetId === nodeId && oldStorageKey ? { storageKey: oldStorageKey, assetKey: oldStorageKey } : { storageKey: undefined, assetKey: undefined }) } })
      for (const key of runState.savedKeys) await cleanupAsset(key)
      return []
    } finally {
      if (runs.get(nodeId) === runState) { setActive(nodeId, false); runs.delete(nodeId) }
    }
  }

  async function generateFromNodes(promptNodeId: string, configNodeId: string): Promise<CanvasNode[]> {
    const project = options.store.activeProject.value
    const promptNode = resolveNode(promptNodeId, 'prompt')
    const configNode = resolveNode(configNodeId, 'config')
    if (!project || !promptNode || !configNode) return []
    const request = await requestForNodes(promptNode, configNode)
    const existing = project.nodes.find((node) => node.type === 'image' && metadata(node).status === 'pending' && metadata(node).promptNodeId === promptNode.id && metadata(node).configNodeId === configNode.id)
    if (existing) {
      if (isGenerating(existing.id)) return []
      return run(existing.id, promptNode, configNode, promptNode.id, configNode.id, request)
    }
    const imageNode = options.store.addNode({ type: 'image', position: { x: configNode.position.x + 320, y: configNode.position.y }, metadata: { status: 'pending', prompt: request.prompt, model: request.model, ...referenceMetadata(request), promptNodeId: promptNode.id, configNodeId: configNode.id, requestSnapshot: requestSnapshot(request) } })
    options.store.connectNodes(promptNode.id, imageNode.id, 'prompt'); options.store.connectNodes(configNode.id, imageNode.id, 'config')
    return run(imageNode.id, promptNode, configNode, promptNode.id, configNode.id, request)
  }

  async function retryImageNode(nodeId: string): Promise<CanvasNode[]> {
    const project = options.store.activeProject.value
    const imageNode = project?.nodes.find((node) => node.id === nodeId && node.type === 'image')
    if (!imageNode) return []
    const imageMetadata = metadata(imageNode)
    const promptNode = resolveNode(stringValue(imageMetadata.promptNodeId), 'prompt')
    const configNode = resolveNode(stringValue(imageMetadata.configNodeId), 'config')
    if (!promptNode || !configNode) return []
    const storedSnapshot = imageMetadata.requestSnapshot && typeof imageMetadata.requestSnapshot === 'object' ? imageMetadata.requestSnapshot as ImageGenerationRequest : undefined
    const storedReferences = Array.isArray(imageMetadata.referenceImages) ? imageMetadata.referenceImages as ImageGenerationRequest['referenceImages'] : undefined
    let snapshot: ImageGenerationRequest | undefined
    if (storedSnapshot) {
      snapshot = { ...storedSnapshot, ...(storedReferences?.length ? { referenceImages: storedReferences.map((reference) => ({ ...reference })) } : {}) }
      if (!storedReferences?.length) {
        const currentRequest = await requestForNodes(promptNode, configNode, imageNode.id)
        if (currentRequest.referenceImages?.length) snapshot.referenceImages = currentRequest.referenceImages
      }
    }
    return run(nodeId, promptNode, configNode, promptNode.id, configNode.id, snapshot)
  }

  function cancelGeneration(nodeId: string): void {
    const direct = runs.get(nodeId)
    const run = direct ?? [...runs.values()].find((candidate) => candidate.targetIds.includes(nodeId))
    if (run) run.cancelled = true
  }

  return { generateFromNodes, retryImageNode, cancelGeneration, isGenerating, removeImageNode, cleanupAsset, activeIds }
}

export { generationError }
