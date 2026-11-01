/**
 * 按模型名识别模型厂商。ModelIcon 的图标与模型广场的厂商树共用这一套规则，
 * 返回值即 ModelIcon 的图标 key。
 */
export function resolveModelVendor(model: string): string | null {
  const modelLower = model.toLowerCase()

  // OpenAI models
  if (modelLower.startsWith('gpt') || modelLower.startsWith('o1') ||
      modelLower.startsWith('o3') || modelLower.startsWith('o4') ||
      modelLower.includes('chatgpt') || modelLower.includes('dall-e') ||
      modelLower.includes('whisper') || modelLower.includes('tts-1') ||
      modelLower.includes('text-embedding-3') || modelLower.includes('text-moderation') ||
      modelLower.includes('babbage') || modelLower.includes('davinci') ||
      modelLower.includes('curie') || modelLower.includes('ada')) return 'openai'

  // Anthropic Claude
  if (modelLower.includes('claude')) return 'claude'

  // Google Gemini
  if (modelLower.includes('gemini') || modelLower.includes('gemma') ||
      modelLower.includes('learnlm') || modelLower.includes('imagen-') ||
      modelLower.includes('veo-')) return 'gemini'

  // Zhipu GLM
  if (modelLower.includes('glm') || modelLower.includes('chatglm') ||
      modelLower.includes('cogview') || modelLower.includes('cogvideo')) return 'zhipu'

  // Alibaba Qwen
  if (modelLower.includes('qwen') || modelLower.includes('qwq')) return 'qwen'

  // DeepSeek
  if (modelLower.includes('deepseek')) return 'deepseek'

  // Mistral
  if (modelLower.includes('mistral') || modelLower.includes('mixtral') ||
      modelLower.includes('codestral') || modelLower.includes('pixtral') ||
      modelLower.includes('voxtral') || modelLower.includes('magistral')) return 'mistral'

  // Meta Llama
  if (modelLower.includes('llama')) return 'meta'

  // Cohere
  if (modelLower.includes('command') || modelLower.includes('c4ai-') ||
      modelLower.includes('embed-')) return 'cohere'

  // Yi
  if (modelLower.startsWith('yi-') || modelLower.startsWith('yi ')) return 'yi'

  // xAI Grok
  if (modelLower.includes('grok')) return 'xai'

  // Moonshot
  if (modelLower.includes('moonshot') || modelLower.includes('kimi')) return 'moonshot'

  // Doubao (ByteDance)
  if (modelLower.includes('doubao')) return 'doubao'

  // MiniMax
  if (modelLower.includes('abab') || modelLower.includes('minimax')) return 'minimax'

  // Baidu Wenxin
  if (modelLower.includes('ernie') || modelLower.includes('wenxin')) return 'wenxin'

  // iFlytek Spark
  if (modelLower.includes('spark')) return 'spark'

  // Tencent Hunyuan
  if (modelLower.includes('hunyuan')) return 'hunyuan'

  // Cloudflare
  if (modelLower.includes('@cf/')) return 'cloudflare'

  // Midjourney
  if (modelLower.includes('mj_') || modelLower.includes('midjourney')) return 'midjourney'

  // Perplexity
  if (modelLower.includes('perplexity') || modelLower.includes('pplx')) return 'perplexity'

  // Jina
  if (modelLower.includes('jina')) return 'jina'

  // OpenRouter
  if (modelLower.includes('openrouter')) return 'openrouter'

  // Suno
  if (modelLower.includes('suno')) return 'suno'

  // Ollama
  if (modelLower.includes('ollama')) return 'ollama'

  // 360
  if (modelLower.includes('360')) return 'ai360'

  // Dify
  if (modelLower.includes('dify')) return 'dify'

  // Coze
  if (modelLower.includes('coze')) return 'coze'

  return null
}

/** 名称识别不出时，按单一厂商平台兜底（composite/antigravity 等多厂商平台不兜底）。 */
const PLATFORM_VENDOR: Record<string, string> = {
  openai: 'openai',
  anthropic: 'claude',
  gemini: 'gemini',
  grok: 'xai',
  deepseek: 'deepseek',
  kimi: 'moonshot',
  zhipu: 'zhipu',
  minimax: 'minimax'
}

export const OTHER_MODEL_VENDOR = 'other'

export function modelVendorOf(model: string, platform?: string): string {
  return resolveModelVendor(model) ?? (platform ? PLATFORM_VENDOR[platform] : undefined) ?? OTHER_MODEL_VENDOR
}

const MODEL_VENDOR_LABELS: Record<string, string> = {
  openai: 'OpenAI',
  claude: 'Anthropic',
  gemini: 'Google',
  zhipu: 'Zhipu GLM',
  qwen: 'Qwen',
  deepseek: 'DeepSeek',
  mistral: 'Mistral',
  meta: 'Meta',
  cohere: 'Cohere',
  yi: '01.AI',
  xai: 'xAI',
  moonshot: 'Moonshot',
  doubao: 'Doubao',
  minimax: 'MiniMax',
  wenxin: 'Baidu ERNIE',
  spark: 'iFlytek Spark',
  hunyuan: 'Tencent Hunyuan',
  cloudflare: 'Cloudflare',
  midjourney: 'Midjourney',
  perplexity: 'Perplexity',
  jina: 'Jina',
  openrouter: 'OpenRouter',
  suno: 'Suno',
  ollama: 'Ollama',
  ai360: '360',
  dify: 'Dify',
  coze: 'Coze'
}

/** 厂商品牌名；未知 key 原样返回（“其他”由调用方本地化）。 */
export function modelVendorLabel(key: string): string {
  return MODEL_VENDOR_LABELS[key] ?? key
}

/** 国产模型厂商：渠道/分组定价按人民币录入。 */
const DOMESTIC_MODEL_VENDORS = new Set([
  'zhipu',
  'qwen',
  'deepseek',
  'yi',
  'moonshot',
  'doubao',
  'minimax',
  'wenxin',
  'spark',
  'hunyuan',
  'ai360'
])

export function isDomesticModelVendor(key: string): boolean {
  return DOMESTIC_MODEL_VENDORS.has(key)
}
