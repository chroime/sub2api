import { ref, type Ref } from 'vue'
import { generateGeminiImage, generateImage, ImageGenerationError, type GeneratedImage, type ImageGenerationRequest } from '@/api/imageGeneration'
import type { CanvasEdge, CanvasNode, CanvasProject, CanvasRepository } from '../types'

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
  targetIds: string[]
  savedKeys: string[]
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
  generateGeminiImage?: Adapter
}

const generationError = (error: unknown): string => {
  const status = error instanceof ImageGenerationError ? error.status : typeof (error as { status?: unknown })?.status === 'number' ? (error as { status: number }).status : undefined
  if (status === 401) return 'API Key 已失效，请重新选择密钥。'
  if (status === 403) return '当前密钥所属分组或模型无权生成图片。'
  if (status === 402) return '余额不足或已达到配额，请检查账户额度。'
  if (status === 429) return '请求过于频繁，请稍后重试。'
  if (status !== undefined && status >= 500) return '图片生成服务暂时不可用，请稍后重试。'
  if (error instanceof TypeError || /network|fetch|failed to fetch/i.test(String((error as Error)?.message ?? error))) return '网络连接失败，请稍后重试。'
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

  async function hasAssetReference(storageKey: string, excludingNodeId?: string): Promise<boolean> {
    const projects = await options.repository.listProjects().catch(() => [])
    const candidates = [options.store.activeProject.value, ...projects.filter((project) => project.id !== options.store.activeProject.value?.id)]
    return candidates.some((project) => project?.nodes.some((node) => node.id !== excludingNodeId && node.type === 'image' && (metadata(node).storageKey === storageKey || metadata(node).assetKey === storageKey)))
  }

  async function cleanupAsset(storageKey: string, excludingNodeId?: string): Promise<void> {
    if (!(await hasAssetReference(storageKey, excludingNodeId))) await options.repository.deleteAsset(storageKey)
  }

  async function removeImageNode(nodeId: string): Promise<void> {
    const project = options.store.activeProject.value
    const node = project?.nodes.find((candidate) => candidate.id === nodeId && candidate.type === 'image')
    const storageKey = node ? stringValue(metadata(node).storageKey) ?? stringValue(metadata(node).assetKey) : undefined
    options.store.removeNode(nodeId)
    if (storageKey) await cleanupAsset(storageKey, nodeId)
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

  async function getSecret(): Promise<string | undefined> {
    const keyId = options.store.activeKeyId?.value ?? options.store.activeProject.value?.activeKeyId
    const getter = options.getKeySecret ?? options.getApiKey
    return getter ? getter(keyId) : undefined
  }

  function adapterForKey(): Adapter {
    const keyId = options.store.activeKeyId?.value ?? options.store.activeProject.value?.activeKeyId
    if (options.getKeyPlatform?.(keyId)?.toLowerCase() === 'gemini') return options.generateGeminiImage ?? generateGeminiImage
    return options.generate ?? options.generateImage ?? generateImage
  }

  async function saveImage(run: GenerationRun, nodeId: string, result: GeneratedImage, request: ImageGenerationRequest, promptNodeId: string, configNodeId: string, oldStorageKey?: string): Promise<string> {
    if (run.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled')
    const projectId = options.store.activeProject.value?.id
    const storageKey = await options.repository.saveAsset({ blob: result.blob, mimeType: result.mimeType || result.blob.type || 'image/png', width: result.width, height: result.height, kind: 'image', projectId })
    run.savedKeys.push(storageKey)
    if (run.cancelled) { await cleanupAsset(storageKey); throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled') }
    options.store.updateNode(nodeId, { metadata: { status: 'completed', prompt: request.prompt, model: request.model, size: request.size, quality: request.quality, count: request.count, background: request.background, mimeType: result.mimeType, width: result.width, height: result.height, storageKey, assetKey: storageKey, promptNodeId, configNodeId, requestSnapshot: { ...request }, error: undefined } })
    if (oldStorageKey && oldStorageKey !== storageKey) await cleanupAsset(oldStorageKey, nodeId)
    return storageKey
  }

  async function run(nodeId: string, promptNode: CanvasNode, configNode: CanvasNode, promptNodeId: string, configNodeId: string, snapshot?: ImageGenerationRequest): Promise<CanvasNode[]> {
    const project = options.store.activeProject.value
    if (!project) return []
    const request = snapshot ? { ...snapshot } : configRequest(configNode, stringValue(metadata(promptNode).prompt) ?? stringValue(metadata(promptNode).text) ?? '')
    const target = project.nodes.find((node) => node.id === nodeId)
    if (!target) return []
    const targetMetadata = metadata(target)
    const oldStorageKey = stringValue(targetMetadata.storageKey) ?? stringValue(targetMetadata.assetKey)
    options.store.updateNode(nodeId, { metadata: { status: 'pending', prompt: request.prompt, model: request.model, size: request.size, quality: request.quality, count: request.count, background: request.background, promptNodeId, configNodeId, requestSnapshot: { ...request }, error: undefined } })
    const runState: GenerationRun = { cancelled: false, targetIds: [nodeId], savedKeys: [] }
    runs.set(nodeId, runState); setActive(nodeId, true)
    try {
      const secret = await getSecret()
      if (runState.cancelled) throw new ImageGenerationError(499, 'cancelled', 'Generation cancelled')
      if (!secret) throw new ImageGenerationError(401, 'missing_key', 'No image API key selected')
      const adapter = adapterForKey()
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
      return nodes
    } catch (error) {
      const message = runState.cancelled || (error instanceof ImageGenerationError && error.code === 'cancelled') ? '生成已取消。' : generationError(error)
      for (const targetId of runState.targetIds) options.store.updateNode(targetId, { metadata: { status: 'failed', prompt: request.prompt, model: request.model, promptNodeId, configNodeId, requestSnapshot: { ...request }, error: message, storageKey: undefined, assetKey: undefined } })
      for (const key of runState.savedKeys) await cleanupAsset(key)
      return []
    } finally {
      setActive(nodeId, false); runs.delete(nodeId)
    }
  }

  async function generateFromNodes(promptNodeId: string, configNodeId: string): Promise<CanvasNode[]> {
    const project = options.store.activeProject.value
    const promptNode = resolveNode(promptNodeId, 'prompt')
    const configNode = resolveNode(configNodeId, 'config')
    if (!project || !promptNode || !configNode) return []
    const request = configRequest(configNode, stringValue(metadata(promptNode).prompt) ?? stringValue(metadata(promptNode).text) ?? '')
    const imageNode = options.store.addNode({ type: 'image', position: { x: configNode.position.x + 320, y: configNode.position.y }, metadata: { status: 'pending', prompt: request.prompt, model: request.model, promptNodeId: promptNode.id, configNodeId: configNode.id, requestSnapshot: { ...request } } })
    options.store.connectNodes(promptNode.id, imageNode.id, 'prompt'); options.store.connectNodes(configNode.id, imageNode.id, 'config')
    return run(imageNode.id, promptNode, configNode, promptNode.id, configNode.id)
  }

  async function retryImageNode(nodeId: string): Promise<CanvasNode[]> {
    const project = options.store.activeProject.value
    const imageNode = project?.nodes.find((node) => node.id === nodeId && node.type === 'image')
    if (!imageNode) return []
    const imageMetadata = metadata(imageNode)
    const promptNode = resolveNode(stringValue(imageMetadata.promptNodeId), 'prompt')
    const configNode = resolveNode(stringValue(imageMetadata.configNodeId), 'config')
    if (!promptNode || !configNode) return []
    const snapshot = imageMetadata.requestSnapshot && typeof imageMetadata.requestSnapshot === 'object' ? imageMetadata.requestSnapshot as ImageGenerationRequest : undefined
    return run(nodeId, promptNode, configNode, promptNode.id, configNode.id, snapshot)
  }

  function cancelGeneration(nodeId: string): void { const run = runs.get(nodeId); if (run) run.cancelled = true }

  return { generateFromNodes, retryImageNode, cancelGeneration, isGenerating, removeImageNode, cleanupAsset, activeIds }
}

export { generationError }
