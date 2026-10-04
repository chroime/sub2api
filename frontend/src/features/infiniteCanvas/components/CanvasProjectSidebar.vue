<script setup lang="ts">
import type { CanvasProject } from '../types'

defineProps<{ projects: CanvasProject[]; activeProjectId?: string | null }>()
const emit = defineEmits<{
  (event: 'select', id: string): void
  (event: 'new'): void
  (event: 'rename', id: string, title: string): void
  (event: 'duplicate', id: string): void
  (event: 'delete', id: string): void
  (event: 'import'): void
  (event: 'export'): void
}>()
function promptRename(project: CanvasProject) {
  const title = window.prompt('Project name', project.title)
  if (title?.trim()) emit('rename', project.id, title.trim())
}
</script>

<template>
  <aside class="canvas-project-sidebar w-full shrink-0 border-b border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900 lg:w-64 lg:border-b-0 lg:border-r">
    <div class="mb-3 flex items-center justify-between">
      <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Projects</h2>
      <button type="button" class="rounded-md bg-primary-600 px-2 py-1 text-xs font-medium text-white" data-new-project @click="emit('new')">New</button>
    </div>
    <div v-if="projects.length" class="space-y-1">
      <div v-for="project in projects" :key="project.id" :class="['group flex items-center rounded-md', project.id === activeProjectId ? 'bg-primary-50 dark:bg-primary-900/30' : 'hover:bg-gray-50 dark:hover:bg-dark-800']">
        <button type="button" :data-canvas-project="project.id" :class="['min-w-0 flex-1 rounded-md px-3 py-2 text-left text-sm', project.id === activeProjectId ? 'text-primary-700 dark:text-primary-300' : 'text-gray-700 dark:text-gray-300']" @click="emit('select', project.id)"><span class="truncate">{{ project.title }}</span></button>
        <button type="button" class="hidden px-1 text-xs text-gray-400 hover:text-primary-600 group-hover:inline" title="Rename project" :data-rename-project="project.id" @click.stop="promptRename(project)">&#9998;</button>
        <button type="button" class="hidden px-1 text-xs text-gray-400 hover:text-primary-600 group-hover:inline" title="Duplicate project" :data-duplicate-project="project.id" @click.stop="emit('duplicate', project.id)">&#10697;</button>
        <button type="button" class="hidden px-1 pr-2 text-xs text-red-400 hover:text-red-600 group-hover:inline" title="Delete project" :data-delete-project="project.id" @click.stop="emit('delete', project.id)">&#215;</button>
      </div>
    </div>
    <div v-else data-canvas-empty="projects" class="rounded-md border border-dashed border-gray-300 px-3 py-5 text-center text-xs text-gray-500 dark:border-dark-600 dark:text-dark-400">No projects yet</div>
    <div class="mt-4 flex gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
      <button type="button" class="text-xs text-gray-500 hover:text-primary-600" @click="emit('import')">Import</button>
      <button type="button" class="text-xs text-gray-500 hover:text-primary-600" @click="emit('export')">Export</button>
    </div>
  </aside>
</template>
