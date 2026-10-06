<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { CanvasBackgroundMode, CanvasProject } from '../types'

defineProps<{ project?: CanvasProject | null; canUndo?: boolean; canRedo?: boolean; saveStatus?: string }>()
const emit = defineEmits<{
  (event: 'background-change', mode: CanvasBackgroundMode): void
  (event: 'zoom', factor: number): void
  (event: 'undo'): void
  (event: 'redo'): void
  (event: 'save'): void
  (event: 'add-nodes'): void
}>()
const { t } = useI18n()
</script>

<template>
  <header class="canvas-toolbar flex flex-wrap items-center gap-2 border-b border-gray-200 bg-white px-3 py-2 dark:border-dark-700 dark:bg-dark-900">
    <button type="button" :title="t('infiniteCanvas.toolbar.undo')" :aria-label="t('infiniteCanvas.toolbar.undo')" :disabled="!canUndo" class="rounded-md border px-2 py-1 text-sm disabled:opacity-40" @click="emit('undo')">&#8630;</button>
    <button type="button" :title="t('infiniteCanvas.toolbar.redo')" :aria-label="t('infiniteCanvas.toolbar.redo')" :disabled="!canRedo" class="rounded-md border px-2 py-1 text-sm disabled:opacity-40" @click="emit('redo')">&#8631;</button>
    <button type="button" :title="t('infiniteCanvas.toolbar.zoomOut')" :aria-label="t('infiniteCanvas.toolbar.zoomOut')" class="rounded-md border px-2 py-1 text-sm" @click="emit('zoom', 0.9)">-</button>
    <button type="button" :title="t('infiniteCanvas.toolbar.zoomIn')" :aria-label="t('infiniteCanvas.toolbar.zoomIn')" class="rounded-md border px-2 py-1 text-sm" @click="emit('zoom', 1.1)">+</button>
    <label class="ml-1 flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">{{ t('infiniteCanvas.toolbar.background') }}
      <select :value="project?.backgroundMode ?? 'grid'" class="rounded-md border bg-transparent px-2 py-1 text-xs" @change="emit('background-change', ($event.target as HTMLSelectElement).value as CanvasBackgroundMode)">
        <option value="grid">{{ t('infiniteCanvas.toolbar.grid') }}</option><option value="dots">{{ t('infiniteCanvas.toolbar.dots') }}</option><option value="plain">{{ t('infiniteCanvas.toolbar.plain') }}</option>
      </select>
    </label>
    <button type="button" :title="t('infiniteCanvas.toolbar.save')" :aria-label="t('infiniteCanvas.toolbar.save')" class="ml-auto rounded-md border px-2 py-1 text-xs" @click="emit('save')">{{ t('infiniteCanvas.toolbar.save') }}</button>
    <button type="button" :title="t('infiniteCanvas.toolbar.addNodes')" class="rounded-md border px-2 py-1 text-xs" @click="emit('add-nodes')">{{ t('infiniteCanvas.toolbar.addNodes') }}</button>
    <span class="text-xs text-gray-500 dark:text-dark-400">{{ saveStatus }}</span>
  </header>
</template>
