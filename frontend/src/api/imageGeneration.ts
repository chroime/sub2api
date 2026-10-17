import { buildGatewayUrl } from './client'

export interface ImageGenerationRequest {
  model: string
  prompt: string
  count?: number
  size?: string
  quality?: string
  background?: string
  referenceImages?: ImageReference[]
}

export interface ImageReference {
  dataUrl: string
  mimeType: string
  name?: string
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
    const gatewayUrl = buildGatewayUrl('/')
    let resolved: URL
    try {
      resolved = new URL(String(item.url), gatewayUrl)
    } catch {
      throw new ImageGenerationError(400, 'invalid_image_url', 'Image response contained an invalid URL')
    }
    if (resolved.origin !== new URL(gatewayUrl).origin) {
      throw new ImageGenerationError(400, 'external_image_url', 'Image response URL must use the gateway origin')
    }
    const response = await fetch(resolved, { credentials: 'same-origin' })
    if (!response.ok) await throwImageError(response)
    const blob = await response.blob()
    return { blob, mimeType: blob.type || item.mime_type || 'image/png', width: item.width, height: item.height }
  }
  throw new ImageGenerationError(200, 'invalid_response', 'Image response did not contain image data')
}

function normalizeGeminiPart(part: any): GeneratedImage | undefined {
  const inline = part?.inlineData ?? part?.inline_data
  if (!inline?.data || typeof inline.data !== 'string') return undefined
  const mimeType = inline.mimeType || inline.mime_type || 'image/png'
  return { blob: decodeBase64(inline.data, mimeType), mimeType, width: inline.width, height: inline.height }
}

function splitDataUrl(dataUrl: string): { mimeType: string; data: string } {
  const match = /^data:([^;,]+)?;base64,(.*)$/i.exec(dataUrl.trim())
  if (!match || !match[2]) throw new ImageGenerationError(400, 'invalid_reference_image', 'Reference image must be a base64 data URL')
  return { mimeType: match[1] || 'image/png', data: match[2] }
}

function referenceData(reference: ImageReference): { mimeType: string; data: string } {
  const parsed = splitDataUrl(reference.dataUrl)
  return { mimeType: reference.mimeType || parsed.mimeType, data: parsed.data }
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

export interface ImageEditRequest extends ImageGenerationRequest { image?: Blob | Blob[] }

export async function editImage(apiKey: string, request: ImageEditRequest): Promise<GeneratedImage[]> {
  const form = new FormData()
  form.append('model', request.model)
  form.append('prompt', request.prompt)
  const images = request.image ? (Array.isArray(request.image) ? request.image : [request.image]) : (request.referenceImages ?? []).map((reference) => {
    const parsed = referenceData(reference)
    return decodeBase64(parsed.data, parsed.mimeType)
  })
  if (!images.length) throw new ImageGenerationError(400, 'missing_reference_image', 'At least one reference image is required for image edits')
  images.forEach((image, index) => form.append(index === 0 ? 'image' : 'image[]', image, request.referenceImages?.[index]?.name || `reference-${index + 1}.png`))
  if (request.count) form.append('n', String(request.count))
  if (request.size) form.append('size', request.size)
  if (request.quality) form.append('quality', request.quality)
  if (request.background) form.append('background', request.background)
  const response = await fetch(buildGatewayUrl('/v1/images/edits'), { method: 'POST', headers: authHeaders(apiKey), body: form })
  if (!response.ok) await throwImageError(response)
  const payload = await response.json()
  return Promise.all((payload?.data ?? []).map(normalizeResult))
}

export async function generateGeminiImage(apiKey: string, request: ImageGenerationRequest): Promise<GeneratedImage[]> {
  const url = buildGatewayUrl(`/v1beta/models/${encodeURIComponent(request.model)}:generateContent`)
  const response = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'x-goog-api-key': apiKey },
    body: JSON.stringify(buildGeminiRequest(request)),
  })
  if (!response.ok) await throwImageError(response)
  const payload = await response.json()
  const parts = (payload?.candidates ?? []).flatMap((candidate: any) => candidate?.content?.parts ?? [])
  const images = parts.map(normalizeGeminiPart).filter((image: GeneratedImage | undefined): image is GeneratedImage => Boolean(image))
  if (!images.length) throw new ImageGenerationError(200, 'invalid_response', 'Gemini response did not contain image data')
  return images
}

export function buildGeminiRequest(request: ImageGenerationRequest): Record<string, unknown> {
  const generationConfig: Record<string, unknown> = { responseModalities: ['TEXT', 'IMAGE'] }
  if (request.size) generationConfig.imageConfig = { imageSize: request.size }
  const parts: Record<string, unknown>[] = [{ text: request.prompt }]
  for (const reference of request.referenceImages ?? []) {
    const parsed = referenceData(reference)
    parts.push({ inlineData: { mimeType: parsed.mimeType, data: parsed.data } })
  }
  return { contents: [{ parts }], generationConfig }
}
