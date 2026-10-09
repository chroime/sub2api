<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { MAX_PROMPT_REFERENCE_IMAGES, MAX_PROMPT_REFERENCE_IMAGE_BYTES, MAX_PROMPT_REFERENCE_IMAGE_DATA_URL_LENGTH, type CanvasNode, type PromptReferenceImage } from '../../types'

const ACCEPTED_REFERENCE_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp'])

const props = defineProps<{ node: CanvasNode }>()
const { t } = useI18n()
const emit = defineEmits<{ (event: 'update', patch: Partial<CanvasNode>): void }>()
const input = ref<HTMLInputElement>()
const uploadError = ref('')
const metadata = computed(() => props.node.metadata as Record<string, unknown>)
const prompt = computed(() => {
  return typeof metadata.value.prompt === 'string' ? metadata.value.prompt : typeof metadata.value.text === 'string' ? metadata.value.text : ''
})
const referenceImages = computed<PromptReferenceImage[]>(() => {
  const value = metadata.value.referenceImages
  if (!Array.isArray(value)) return []
  return value.filter((candidate): candidate is PromptReferenceImage => {
    if (!candidate || typeof candidate !== 'object') return false
    const image = candidate as Record<string, unknown>
    return typeof image.dataUrl === 'string' && /^data:image\/(png|jpeg|webp);base64,/i.test(image.dataUrl) && image.dataUrl.length <= MAX_PROMPT_REFERENCE_IMAGE_DATA_URL_LENGTH && typeof image.mimeType === 'string' && ACCEPTED_REFERENCE_TYPES.has(image.mimeType.toLowerCase())
  }).slice(0, MAX_PROMPT_REFERENCE_IMAGES)
})

function update(value: string) { emit('update', { metadata: { prompt: value, text: value } }) }

function readAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => typeof reader.result === 'string' ? resolve(reader.result) : reject(new Error('invalid file data'))
    reader.onerror = () => reject(reader.error ?? new Error('failed to read file'))
    reader.readAsDataURL(file)
  })
}

function appendReferenceToken(value: string, index: number): string {
  const token = `[参考图${index}]`
  const trimmed = value.trimEnd()
  return `${trimmed}${trimmed ? ' ' : ''}${token}`
}

async function addReferences(event: Event) {
  const target = event.target as HTMLInputElement
  const files = Array.from(target.files ?? [])
  target.value = ''
  uploadError.value = ''
  if (!files.length) return
  const remaining = MAX_PROMPT_REFERENCE_IMAGES - referenceImages.value.length
  if (remaining <= 0) {
    uploadError.value = t('infiniteCanvas.prompt.maxReferenceImages', { count: MAX_PROMPT_REFERENCE_IMAGES })
    return
  }
  const accepted = files.slice(0, remaining)
  const next = [...referenceImages.value]
  let nextPrompt = prompt.value
  for (const file of accepted) {
    const mimeType = file.type.toLowerCase()
    if (!ACCEPTED_REFERENCE_TYPES.has(mimeType)) {
      uploadError.value = t('infiniteCanvas.prompt.unsupportedReferenceImage')
      continue
    }
    if (file.size > MAX_PROMPT_REFERENCE_IMAGE_BYTES) {
      uploadError.value = t('infiniteCanvas.prompt.referenceImageTooLarge', { size: '5 MB' })
      continue
    }
    try {
      const dataUrl = await readAsDataUrl(file)
      if (dataUrl.length > MAX_PROMPT_REFERENCE_IMAGE_DATA_URL_LENGTH) {
        uploadError.value = t('infiniteCanvas.prompt.referenceImageTooLarge', { size: '5 MB' })
        continue
      }
      next.push({ dataUrl, mimeType, name: file.name || `reference-${next.length + 1}` })
      nextPrompt = appendReferenceToken(nextPrompt, next.length)
    } catch {
      uploadError.value = t('infiniteCanvas.prompt.referenceImageReadFailed')
    }
  }
  if (next.length !== referenceImages.value.length) emit('update', { metadata: { prompt: nextPrompt, text: nextPrompt, referenceImages: next } })
}

function removeReference(index: number) {
  const next = referenceImages.value.filter((_image, imageIndex) => imageIndex !== index)
  const token = `[参考图${index + 1}]`
  let nextPrompt = prompt.value.replace(token, '')
  // Keep the token numbering aligned with the thumbnails after a removal.
  for (let tokenIndex = referenceImages.value.length; tokenIndex > index + 1; tokenIndex -= 1) {
    nextPrompt = nextPrompt.replace(new RegExp(`\\[参考图${tokenIndex}\\]`, 'g'), `[参考图${tokenIndex - 1}]`)
  }
  nextPrompt = nextPrompt.replace(/\s{2,}/g, ' ').trim()
  emit('update', { metadata: { prompt: nextPrompt, text: nextPrompt, referenceImages: next } })
}
</script>

<template>
  <div class="prompt-node" data-canvas-no-zoom>
    <span class="prompt-node__label">{{ t('infiniteCanvas.inspector.prompt') }}</span>
    <textarea :value="prompt" rows="4" data-canvas-no-zoom @pointerdown.stop @input="update(($event.target as HTMLTextAreaElement).value)" />
    <div class="prompt-node__references">
      <div v-for="(image, index) in referenceImages" :key="`${image.dataUrl.slice(0, 24)}-${index}`" class="prompt-node__reference">
        <img :src="image.dataUrl" :alt="image.name || t('infiniteCanvas.prompt.referenceImageAlt', { index: index + 1 })" />
        <button type="button" data-reference-image-remove :aria-label="t('infiniteCanvas.prompt.removeReferenceImage')" @pointerdown.stop @click.stop="removeReference(index)">×</button>
      </div>
    </div>
    <input ref="input" type="file" accept="image/png,image/jpeg,image/webp" multiple class="prompt-node__file-input" data-reference-image-input data-canvas-no-zoom @pointerdown.stop @change="addReferences" />
    <button type="button" class="prompt-node__add" data-reference-image-add data-canvas-no-zoom @pointerdown.stop @click.stop="input?.click()">{{ t('infiniteCanvas.prompt.addReferenceImage') }}</button>
    <small v-if="uploadError" class="prompt-node__error" role="status">{{ uploadError }}</small>
  </div>
</template>

<style scoped>
.prompt-node { display: flex; flex-direction: column; gap: 5px; }
.prompt-node__label { color: #64748b; font-size: 11px; font-weight: 600; }
.prompt-node textarea { min-height: 72px; resize: vertical; width: 100%; box-sizing: border-box; border: 1px solid #cbd5e1; border-radius: 5px; padding: 6px; color: #1e293b; background: #fff; font: inherit; font-size: 12px; }
.prompt-node__references { display: flex; flex-wrap: wrap; gap: 5px; }
.prompt-node__reference { position: relative; width: 42px; height: 42px; overflow: hidden; border: 1px solid #cbd5e1; border-radius: 4px; background: #f8fafc; }
.prompt-node__reference img { width: 100%; height: 100%; object-fit: cover; }
.prompt-node__reference button { position: absolute; right: 1px; top: 1px; width: 15px; height: 15px; padding: 0; border: 0; border-radius: 50%; color: #fff; background: rgb(15 23 42 / 72%); cursor: pointer; font-size: 12px; line-height: 14px; }
.prompt-node__file-input { display: none; }
.prompt-node__add { align-self: flex-start; padding: 3px 6px; border: 1px solid #cbd5e1; border-radius: 4px; color: #475569; background: #f8fafc; cursor: pointer; font: inherit; font-size: 11px; }
.prompt-node__error { color: #b91c1c; font-size: 10px; }
</style>
