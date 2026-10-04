<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { keysAPI, userGroupsAPI } from '@/api'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import InfiniteCanvasSurface from '@/features/infiniteCanvas/components/InfiniteCanvasSurface.vue'
import CanvasProjectSidebar from '@/features/infiniteCanvas/components/CanvasProjectSidebar.vue'
import CanvasToolbar from '@/features/infiniteCanvas/components/CanvasToolbar.vue'
import CanvasInspector from '@/features/infiniteCanvas/components/CanvasInspector.vue'
import CanvasKeyPicker from '@/features/infiniteCanvas/components/CanvasKeyPicker.vue'
import { selectEligibleCanvasKeys } from '@/features/infiniteCanvas/keySelection'
import type { CanvasKeyOption } from '@/features/infiniteCanvas/keySelection'
import type { CanvasBackgroundMode, CanvasNode, CanvasProject, CanvasRepository } from '@/features/infiniteCanvas/types'
import { useInfiniteCanvasStore } from '@/features/infiniteCanvas/stores/useInfiniteCanvasStore'
import { createIndexedDbCanvasRepository } from '@/features/infiniteCanvas/storage/indexedDbCanvasRepository'

const props = defineProps<{ repository?: CanvasRepository }>()
const repository = props.repository ?? createIndexedDbCanvasRepository()
const store = useInfiniteCanvasStore(repository)
const authStore = useAuthStore()
const groups = ref<Awaited<ReturnType<typeof userGroupsAPI.getAvailable>>>([])
const eligibleKeys = ref<CanvasKeyOption[]>([])
const keySecrets = new Map<number, string>()
const keysLoading = ref(true)
const activeTab = ref<'canvas' | 'inspector'>('canvas')
const showCreateKey = ref(false)
const newKeyName = ref('Canvas image key')
const newKeyGroupId = ref<number | null>(null)
const showDelete = ref(false)
const projectToDelete = ref<string | null>(null)
const saveStatus = ref('')
const importInput = ref<HTMLInputElement>()

const activeProject = computed(() => store.activeProject.value)
const selectedNode = computed<CanvasNode | null>(() => activeProject.value?.nodes.find((node) => store.selectedNodeIds.value.includes(node.id)) ?? null)
const selectedKeyId = computed(() => store.activeKeyId.value ?? null)
const lastSaved = computed(() => activeProject.value ? `Last saved ${activeProject.value.updatedAt.toLocaleString()}` : '')

function refreshEligibleKeys(items: Parameters<typeof selectEligibleCanvasKeys>[0] = []) {
  eligibleKeys.value = selectEligibleCanvasKeys(items, groups.value)
  eligibleKeys.value.forEach((option) => keySecrets.set(option.id, option.key))
}

async function loadKeys() {
  keysLoading.value = true
  try {
    const [firstResponse, availableGroups] = await Promise.all([keysAPI.list(1, 100), userGroupsAPI.getAvailable()])
    const allKeys = [...firstResponse.items]
    const pages = firstResponse.pages ?? 1
    for (let page = 2; page <= pages; page += 1) {
      const response = await keysAPI.list(page, 100)
      allKeys.push(...response.items)
    }
    groups.value = availableGroups
    refreshEligibleKeys(allKeys)
  } catch {
    eligibleKeys.value = []
  } finally {
    keysLoading.value = false
  }
}

async function createKey() {
  const created = await keysAPI.create(newKeyName.value.trim() || 'Canvas image key', newKeyGroupId.value)
  keySecrets.set(created.id, created.key)
  await loadKeys()
  const option = eligibleKeys.value.find((item) => item.id === created.id)
  if (option) store.setActiveKey(option.id)
  showCreateKey.value = false
}

function chooseKey(id: number | null) { store.setActiveKey(id ?? undefined) }
function selectProject(id: string) { store.setActiveProject(id) }
function createProject() { store.createProject(`Canvas ${store.projects.value.length + 1}`) }
function renameProject(id: string, title: string) { store.renameProject(id, title) }
function duplicateProject(id: string) { store.duplicateProject(id) }
function requestDelete(id: string) { projectToDelete.value = id; showDelete.value = true }
async function deleteProject() { if (projectToDelete.value) await store.deleteProject(projectToDelete.value); showDelete.value = false; projectToDelete.value = null }
function updateNode(patch: Partial<CanvasNode>) { if (selectedNode.value) store.updateNode(selectedNode.value.id, patch) }
function removeSelectedNode() { if (selectedNode.value) store.removeNode(selectedNode.value.id) }
function changeBackground(mode: CanvasBackgroundMode) { if (activeProject.value) { activeProject.value.backgroundMode = mode; activeProject.value.updatedAt = new Date() } }
function zoom(factor: number) { if (activeProject.value) activeProject.value.viewport.zoom = Math.max(0.2, Math.min(3, activeProject.value.viewport.zoom * factor)) }
function selectNode(nodeId: string, additive: boolean) { store.selectedNodeIds.value = additive ? [...new Set([...store.selectedNodeIds.value, nodeId])] : [nodeId] }
function moveNode(nodeId: string, position: { x: number; y: number }) { store.updateNode(nodeId, { position }) }
function connectNodes(edge: { sourceNodeId: string; targetNodeId: string; kind: 'prompt' | 'config' | 'reference' }) { store.connectNodes(edge) }
function updateViewport(viewport: CanvasProject['viewport']) {
  const current = activeProject.value?.viewport
  if (!current || (current.x === viewport.x && current.y === viewport.y && current.zoom === viewport.zoom)) return
  activeProject.value!.viewport = { ...viewport }
}
function saveNow() { saveStatus.value = 'Saved'; window.setTimeout(() => { saveStatus.value = '' }, 1600) }
function exportProject() {
  if (!activeProject.value) return
  const blob = new Blob([JSON.stringify(activeProject.value)], { type: 'application/json' })
  const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = `${activeProject.value.title || 'canvas'}.json`; link.click(); URL.revokeObjectURL(url)
}
function importProject() { importInput.value?.click() }
async function handleImport(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]; if (!file) return
  try { const data = JSON.parse(await file.text()) as CanvasProject; if (data.id && data.title) await repository.saveProject(data); await store.ready; } catch { /* invalid files remain ignored */ }
  if (importInput.value) importInput.value.value = ''
}
function clearSecrets() { keySecrets.clear() }

watch(() => authStore.user?.id, () => { clearSecrets(); void loadKeys() })
onMounted(async () => { await store.ready; await loadKeys() })
onBeforeUnmount(clearSecrets)
</script>

<template>
  <AppLayout>
    <div class="infinite-canvas-view min-h-[calc(100vh-8rem)] overflow-hidden rounded-lg bg-gray-100 shadow-sm dark:bg-dark-950 dark:bg-dark-950" data-page="infinite-canvas">
      <div class="flex min-h-[calc(100vh-8rem)] flex-col lg:flex-row">
        <CanvasProjectSidebar :projects="store.projects.value" :active-project-id="activeProject?.id" @select="selectProject" @new="createProject" @rename="renameProject" @duplicate="duplicateProject" @delete="requestDelete" @import="importProject" @export="exportProject" />
        <section class="flex min-w-0 flex-1 flex-col">
          <CanvasToolbar :project="activeProject" :can-undo="store.canUndo.value" :can-redo="store.canRedo.value" :save-status="saveStatus || lastSaved" @background-change="changeBackground" @zoom="zoom" @undo="store.undo" @redo="store.redo" @save="saveNow" />
          <div class="flex gap-1 border-b border-gray-200 bg-white px-3 pt-2 dark:border-dark-700 dark:bg-dark-900 lg:hidden">
            <button type="button" data-canvas-tab="canvas" :class="['rounded-t-md px-3 py-1 text-xs', activeTab === 'canvas' ? 'is-active bg-gray-100 font-semibold dark:bg-dark-800' : '']" @click="activeTab = 'canvas'">Canvas</button>
            <button type="button" data-canvas-tab="inspector" :class="['rounded-t-md px-3 py-1 text-xs', activeTab === 'inspector' ? 'is-active bg-gray-100 font-semibold dark:bg-dark-800' : '']" @click="activeTab = 'inspector'">Inspector</button>
          </div>
          <div class="flex min-h-0 flex-1 flex-col lg:flex-row">
            <div v-if="!activeProject" data-canvas-empty="projects" class="flex min-h-[420px] flex-1 items-center justify-center p-8 text-center text-sm text-gray-500 dark:text-dark-400">No projects yet. <button type="button" class="ml-1 text-primary-600 hover:underline" @click="createProject">Create a project</button></div>
            <div v-else class="relative min-h-[420px] min-w-0 flex-1 overflow-auto" :class="{ hidden: activeTab !== 'canvas' }">
              <InfiniteCanvasSurface :project="activeProject" :selected-node-ids="store.selectedNodeIds.value" @node-select="selectNode" @node-move="moveNode" @node-delete="store.removeNode" @edge-create="connectNodes" @viewport-update="updateViewport" />
              <div v-if="!eligibleKeys.length" class="pointer-events-none absolute left-1/2 top-6 w-72 -translate-x-1/2 rounded-md border border-amber-200 bg-amber-50 p-3 text-center text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-200" data-canvas-empty="image-models">No image models available for this canvas.</div>
            </div>
            <CanvasInspector v-if="activeProject" :class="{ 'hidden lg:block': activeTab !== 'inspector' }" :node="selectedNode" @update="updateNode" @delete="removeSelectedNode" />
          </div>
          <div class="border-t border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900"><CanvasKeyPicker :options="eligibleKeys" :model-value="selectedKeyId" :loading="keysLoading" @update:model-value="chooseKey" @create-key="showCreateKey = true" /></div>
        </section>
      </div>
    </div>
    <input ref="importInput" type="file" accept="application/json" class="hidden" @change="handleImport" />
    <BaseDialog :show="showCreateKey" title="Create image key" width="narrow" @close="showCreateKey = false">
      <div class="space-y-3"><label class="block text-sm">Name<input v-model="newKeyName" class="mt-1 w-full rounded-md border px-3 py-2 dark:border-dark-600 dark:bg-dark-800" /></label><label class="block text-sm">Group<select v-model="newKeyGroupId" class="mt-1 w-full rounded-md border px-3 py-2 dark:border-dark-600 dark:bg-dark-800"><option :value="null">Select group</option><option v-for="group in groups.filter((item) => item.allow_image_generation)" :key="group.id" :value="group.id">{{ group.name }}</option></select></label></div>
      <template #footer><button type="button" class="rounded-md bg-primary-600 px-3 py-2 text-sm text-white" @click="createKey">Create</button></template>
    </BaseDialog>
    <ConfirmDialog :show="showDelete" title="Delete project" message="Delete this project and its nodes?" danger @confirm="deleteProject" @cancel="showDelete = false" />
  </AppLayout>
</template>
