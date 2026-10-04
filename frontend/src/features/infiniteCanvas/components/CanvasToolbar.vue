<script setup lang="ts">
import type { CanvasBackgroundMode, CanvasProject } from '../types'

defineProps<{ project?: CanvasProject | null; canUndo?: boolean; canRedo?: boolean; saveStatus?: string }>()
const emit = defineEmits<{
  (event: 'background-change', mode: CanvasBackgroundMode): void
  (event: 'zoom', factor: number): void
  (event: 'undo'): void
  (event: 'redo'): void
  (event: 'save'): void
}>()
</script>

<template>
  <header class="canvas-toolbar flex flex-wrap items-center gap-2 border-b border-gray-200 bg-white px-3 py-2 dark:border-dark-700 dark:bg-dark-900">
    <button type="button" title="Undo" aria-label="Undo" :disabled="!canUndo" class="rounded-md border px-2 py-1 text-sm disabled:opacity-40" @click="emit('undo')">&#8630;</button>
    <button type="button" title="Redo" aria-label="Redo" :disabled="!canRedo" class="rounded-md border px-2 py-1 text-sm disabled:opacity-40" @click="emit('redo')">&#8631;</button>
    <button type="button" title="Zoom out" aria-label="Zoom out" class="rounded-md border px-2 py-1 text-sm" @click="emit('zoom', 0.9)">-</button>
    <button type="button" title="Zoom in" aria-label="Zoom in" class="rounded-md border px-2 py-1 text-sm" @click="emit('zoom', 1.1)">+</button>
    <label class="ml-1 flex items-center gap-2 text-xs text-gray-600 dark:text-gray-300">Background
      <select :value="project?.backgroundMode ?? 'grid'" class="rounded-md border bg-transparent px-2 py-1 text-xs" @change="emit('background-change', ($event.target as HTMLSelectElement).value as CanvasBackgroundMode)">
        <option value="grid">Grid</option><option value="dots">Dots</option><option value="plain">Plain</option>
      </select>
    </label>
    <button type="button" title="Save" aria-label="Save" class="ml-auto rounded-md border px-2 py-1 text-xs" @click="emit('save')">Save</button>
    <span class="text-xs text-gray-500 dark:text-dark-400">{{ saveStatus }}</span>
  </header>
</template>
