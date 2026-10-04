import { buildGatewayUrl } from './client'

export interface ImageGenerationRequest {
  model: string
  prompt: string
  count?: number
  size?: string
  quality?: string
  background?: string
}

export interface GeneratedImage {
  blob: Blob
  mimeType: string
  width?: number
  height?: number
}

export class ImageGenerationError extends Error {
  readonly status: number
  readonly code?: string

  constructor(status: number, code: string | undefined, message: string) {
    super(message)
    this.name = 'ImageGenerationError'
    this.status = status
    this.code = code
  }
}

function authHeaders(apiKey: string, extra: Record<string, string> = {}): Record<string, string> {
  return { Authorization: `Bearer ${apiKey}`, ...extra }
}

async function throwImageError(response: Response): Promise<never> {
  let body: any = undefined
  try { body = await response.json() } catch { /* non-JSON error */ }
  const error = body?.error ?? body
  const code = error?.code != null ? String(error.code) : undefined
  const message = error?.message || response.statusText || 'Image generation request failed'
  throw new ImageGenerationError(response.status, code, message)
}

export interface ImageModel { id: string; [key: string]: unknown }

export async function listImageModels(apiKey: string): Promise<ImageModel[]> {
  const response = await fetch(buildGatewayUrl('/v1/models'), { headers: authHeaders(apiKey) })
  if (!response.ok) await throwImageError(response)
  const payload = await response.json()
  const models = Array.isArray(payload) ? payload : payload?.data
  if (!Array.isArray(models)) return []
  return models.filter((model) => {
    const id = String(model?.id || '').toLowerCase()
    if (id.includes('image')) return true
    const metadata = model?.metadata ?? model?.capabilities
    if (Array.isArray(metadata)) return metadata.some((value) => String(value).toLowerCase().includes('image'))
    if (metadata && typeof metadata === 'object') {
      return Object.entries(metadata).some(([key, value]) => key.toLowerCase().includes('image') && value !== false && value != null)
    }
    return false
  })
}

function decodeBase64(value: string, mimeType = 'image/png'): Blob {
  const binary = typeof atob === 'function' ? atob(value) : Buffer.from(value, 'base64').toString('binary')
  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0))
  return new Blob([bytes], { type: mimeType })
}

async function normalizeResult(item: any): Promise<GeneratedImage> {
  if (item?.b64_json) {
    const mimeType = item.mime_type || item.mimeType || 'image/png'
    return { blob: decodeBase64(item.b64_json, mimeType), mimeType, width: item.width, height: item.height }
  }
  if (item?.url) {
    const response = await fetch(item.url)
    if (!response.ok) await throwImageError(response)
    const blob = await response.blob()
    return { blob, mimeType: blob.type || item.mime_type || 'image/png', width: item.width, height: item.height }
  }
  throw new ImageGenerationError(200, 'invalid_response', 'Image response did not contain image data')
}

export async function generateImage(apiKey: string, request: ImageGenerationRequest): Promise<GeneratedImage[]> {
  const body: Record<string, unknown> = { model: request.model, prompt: request.prompt, n: request.count ?? 1 }
  if (request.size) body.size = request.size
  if (request.quality) body.quality = request.quality
  if (request.background) body.background = request.background
  const response = await fetch(buildGatewayUrl('/v1/images/generations'), {
    method: 'POST', headers: authHeaders(apiKey, { 'Content-Type': 'application/json' }), body: JSON.stringify(body)
  })
  if (!response.ok) await throwImageError(response)
  const payload = await response.json()
  return Promise.all((payload?.data ?? []).map(normalizeResult))
}

export interface ImageEditRequest extends ImageGenerationRequest { image: Blob }

export async function editImage(apiKey: string, request: ImageEditRequest): Promise<GeneratedImage[]> {
  const form = new FormData()
  form.append('model', request.model)
  form.append('prompt', request.prompt)
  form.append('image', request.image, 'reference.png')
  if (request.size) form.append('size', request.size)
  if (request.quality) form.append('quality', request.quality)
  const response = await fetch(buildGatewayUrl('/v1/images/edits'), { method: 'POST', headers: authHeaders(apiKey), body: form })
  if (!response.ok) await throwImageError(response)
  const payload = await response.json()
  return Promise.all((payload?.data ?? []).map(normalizeResult))
}

export function buildGeminiRequest(request: ImageGenerationRequest): Record<string, unknown> {
  const generationConfig: Record<string, unknown> = { responseModalities: ['TEXT', 'IMAGE'] }
  if (request.size) generationConfig.imageConfig = { imageSize: request.size }
  return { contents: [{ parts: [{ text: request.prompt }] }], generationConfig }
}

