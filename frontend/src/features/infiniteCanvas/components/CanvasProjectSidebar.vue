<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { CanvasProject } from '../types'

defineProps<{ projects: CanvasProject[]; activeProjectId?: string | null; mobileOpen?: boolean }>()
const emit = defineEmits<{
  (event: 'select', id: string): void
  (event: 'new'): void
  (event: 'rename', id: string, title: string): void
  (event: 'duplicate', id: string): void
  (event: 'delete', id: string): void
  (event: 'import'): void
  (event: 'export'): void
  (event: 'close'): void
}>()
const { t } = useI18n()
const showRename = ref(false)
const renameProject = ref<CanvasProject | null>(null)
const renameTitle = ref('')
function promptRename(project: CanvasProject) {
  renameProject.value = project
  renameTitle.value = project.title
  showRename.value = true
}
function closeRename() {
  showRename.value = false
  renameProject.value = null
  renameTitle.value = ''
}
function confirmRename() {
  const project = renameProject.value
  const title = renameTitle.value.trim()
  if (!project || !title) return
  emit('rename', project.id, title)
  closeRename()
}
</script>

<template>
  <aside :class="['canvas-project-sidebar w-full shrink-0 border-b border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900 lg:static lg:block lg:w-64 lg:border-b-0 lg:border-r', mobileOpen ? 'fixed inset-y-0 left-0 z-40 block w-72 shadow-xl' : 'hidden lg:block']">
    <div class="mb-3 flex items-center justify-between">
      <h2 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('infiniteCanvas.sidebar.title') }}</h2>
      <div class="flex items-center gap-2"><button type="button" :aria-label="t('infiniteCanvas.sidebar.close')" class="text-xs text-gray-500 lg:hidden" @click="emit('close')">{{ t('infiniteCanvas.sidebar.close') }}</button><button type="button" class="rounded-md bg-primary-600 px-2 py-1 text-xs font-medium text-white" data-new-project @click="emit('new')">{{ t('infiniteCanvas.sidebar.new') }}</button></div>
    </div>
    <div v-if="projects.length" class="space-y-1">
      <div v-for="project in projects" :key="project.id" :class="['group flex items-center rounded-md', project.id === activeProjectId ? 'bg-primary-50 dark:bg-primary-900/30' : 'hover:bg-gray-50 dark:hover:bg-dark-800']">
        <button type="button" :data-canvas-project="project.id" :class="['min-w-0 flex-1 rounded-md px-3 py-2 text-left text-sm', project.id === activeProjectId ? 'text-primary-700 dark:text-primary-300' : 'text-gray-700 dark:text-gray-300']" @click="emit('select', project.id)"><span class="truncate">{{ project.title }}</span></button>
        <button type="button" class="hidden px-1 text-xs text-gray-400 hover:text-primary-600 group-hover:inline" :title="t('infiniteCanvas.sidebar.rename')" :data-rename-project="project.id" @click.stop="promptRename(project)">&#9998;</button>
        <button type="button" class="hidden px-1 text-xs text-gray-400 hover:text-primary-600 group-hover:inline" :title="t('infiniteCanvas.sidebar.duplicate')" :data-duplicate-project="project.id" @click.stop="emit('duplicate', project.id)">&#10697;</button>
        <button type="button" class="hidden px-1 pr-2 text-xs text-red-400 hover:text-red-600 group-hover:inline" :title="t('infiniteCanvas.sidebar.delete')" :data-delete-project="project.id" @click.stop="emit('delete', project.id)">&#215;</button>
      </div>
    </div>
    <div v-else data-canvas-empty="projects" class="rounded-md border border-dashed border-gray-300 px-3 py-5 text-center text-xs text-gray-500 dark:border-dark-600 dark:text-dark-400">{{ t('infiniteCanvas.sidebar.noProjects') }}</div>
    <div class="mt-4 flex gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
      <button type="button" class="text-xs text-gray-500 hover:text-primary-600" @click="emit('import')">{{ t('infiniteCanvas.sidebar.import') }}</button>
      <button type="button" class="text-xs text-gray-500 hover:text-primary-600" @click="emit('export')">{{ t('infiniteCanvas.sidebar.export') }}</button>
    </div>
  </aside>
  <BaseDialog :show="showRename" :title="t('infiniteCanvas.project.rename')" width="narrow" @close="closeRename">
    <label class="block text-sm text-gray-700 dark:text-gray-200">
      {{ t('infiniteCanvas.sidebar.namePrompt') }}
      <input
        v-model="renameTitle"
        data-rename-input
        type="text"
        maxlength="200"
        class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm outline-none focus:border-primary-500 focus:ring-2 focus:ring-primary-500/20 dark:border-dark-600 dark:bg-dark-800"
        @keydown.enter.prevent="confirmRename"
        @keydown.esc.prevent="closeRename"
      />
    </label>
    <template #footer>
      <button type="button" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 dark:border-dark-600 dark:text-gray-200" @click="closeRename">{{ t('common.cancel') }}</button>
      <button type="button" data-rename-submit :disabled="!renameTitle.trim()" class="rounded-md bg-primary-600 px-3 py-2 text-sm text-white disabled:cursor-not-allowed disabled:opacity-50" @click="confirmRename">{{ t('common.confirm') }}</button>
    </template>
  </BaseDialog>
</template>
