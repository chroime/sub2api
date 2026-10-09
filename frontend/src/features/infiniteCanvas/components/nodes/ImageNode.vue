<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CanvasNode } from '../../types'
import NodeStatusBadge from './NodeStatusBadge.vue'

const props = defineProps<{ node: CanvasNode; imageUrl?: string }>()
const { t } = useI18n()
const emit = defineEmits<{ (event: 'delete'): void; (event: 'retry'): void }>()
const download = () => {
  if (!props.imageUrl) return
  const mime = typeof props.node.metadata.mimeType === 'string' ? props.node.metadata.mimeType.toLowerCase() : 'image/png'
  const extension = mime === 'image/jpeg' ? 'jpg' : mime === 'image/webp' ? 'webp' : mime === 'image/gif' ? 'gif' : 'png'
  const link = document.createElement('a'); link.href = props.imageUrl; link.download = `${props.node.id}.${extension}`; link.click()
}
</script>

<template>
  <div class="image-node" data-canvas-no-zoom>
    <div v-if="imageUrl" class="image-node__preview"><img :src="imageUrl" :alt="t('infiniteCanvas.image.alt')" loading="lazy" decoding="async" /></div>
    <div v-else class="image-node__empty">{{ node.metadata.status === 'pending' ? t('infiniteCanvas.image.generating') : t('infiniteCanvas.image.empty') }}</div>
    <div class="image-node__meta"><NodeStatusBadge :status="String(node.metadata.status ?? '')" /><span v-if="typeof node.metadata.model === 'string'">{{ node.metadata.model }}</span></div>
    <p v-if="typeof node.metadata.prompt === 'string'" class="image-node__prompt">{{ node.metadata.prompt }}</p>
    <p v-if="typeof node.metadata.error === 'string'" class="image-node__error">{{ node.metadata.error }}</p>
    <div class="image-node__actions"><button type="button" data-canvas-no-zoom data-canvas-image-download :disabled="!imageUrl" @pointerdown.stop @click.stop="download">{{ t('infiniteCanvas.image.download') }}</button><button v-if="node.metadata.status === 'failed' || node.metadata.status === 'error'" type="button" data-canvas-no-zoom @pointerdown.stop @click.stop="emit('retry')">{{ t('infiniteCanvas.image.retry') }}</button><button type="button" data-canvas-no-zoom @pointerdown.stop @click.stop="emit('delete')">{{ t('infiniteCanvas.image.delete') }}</button></div>
  </div>
</template>

<style scoped>
.image-node { display: flex; flex-direction: column; gap: 6px; font-size: 11px; }
.image-node__preview, .image-node__empty { width: 100%; height: 150px; border-radius: 5px; overflow: hidden; background: #f1f5f9; display: flex; align-items: center; justify-content: center; color: #64748b; }
.image-node__preview img { display: block; width: 100%; height: 100%; object-fit: contain; }
.image-node__meta { display: flex; align-items: center; justify-content: space-between; gap: 6px; color: #64748b; }
.image-node__prompt, .image-node__error { margin: 0; overflow-wrap: anywhere; }
.image-node__prompt { display: -webkit-box; overflow: hidden; -webkit-box-orient: vertical; -webkit-line-clamp: 3; }
.image-node__error { color: #b91c1c; }
.image-node__actions { display: flex; gap: 5px; }
.image-node__actions button { border: 1px solid #cbd5e1; border-radius: 4px; padding: 3px 6px; background: #fff; color: #334155; cursor: pointer; font-size: 11px; }
.image-node__actions button:disabled { cursor: not-allowed; opacity: .5; }
</style>
