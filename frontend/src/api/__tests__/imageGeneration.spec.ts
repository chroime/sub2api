import { beforeEach, describe, expect, it, vi } from 'vitest'
import { buildGeminiRequest, editImage, generateImage, ImageGenerationError, listImageModels } from '../imageGeneration'

describe('image generation gateway adapter', () => {
  beforeEach(() => vi.restoreAllMocks())

  it('lists image-capable models with GET and bearer auth', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: [
      { id: 'gpt-image-1' }, { id: 'gpt-4o', metadata: { image: false } }, { id: 'gemini-image', capabilities: ['image'] }
    ] }), { status: 200 }))
    await expect(listImageModels('secret')).resolves.toHaveLength(2)
    expect(fetchMock.mock.calls[0][0]).toContain('/v1/models')
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ headers: { Authorization: 'Bearer secret' } })
  })

  it('posts generation parameters and normalizes base64 output', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: [{ b64_json: 'aGVsbG8=', mime_type: 'image/png' }] }), { status: 200 }))
    const result = await generateImage('secret', { model: 'gpt-image-1', prompt: 'hello', count: 2, size: '1024x1024', quality: 'high' })
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(fetchMock.mock.calls[0][0]).toContain('/v1/images/generations')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toMatchObject({ model: 'gpt-image-1', prompt: 'hello', n: 2, size: '1024x1024', quality: 'high' })
    expect(result[0].blob).toBeInstanceOf(Blob)
  })

  it('uses multipart form data for edits and exposes structured errors', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ error: { code: 'bad_request', message: 'Nope' } }), { status: 422, statusText: 'Unprocessable' }))
    await expect(editImage('secret', { model: 'm', prompt: 'p', image: new Blob(['x']) })).rejects.toMatchObject({ status: 422, code: 'bad_request', message: 'Nope' } satisfies Partial<ImageGenerationError>)
    const init = fetchMock.mock.calls[0][1] as RequestInit
    expect(init.method).toBe('POST')
    expect(init.body).toBeInstanceOf(FormData)
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer secret')
    expect(init.headers).not.toHaveProperty('Content-Type')
  })

  it('builds Gemini multimodal image requests', () => {
    expect(buildGeminiRequest({ model: 'gemini-image', prompt: 'draw', size: '1K' })).toEqual({
      contents: [{ parts: [{ text: 'draw' }] }], generationConfig: { responseModalities: ['TEXT', 'IMAGE'], imageConfig: { imageSize: '1K' } }
    })
  })
})
