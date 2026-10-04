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
import { isCanvasImagePlatform, selectEligibleCanvasKeys } from '@/features/infiniteCanvas/keySelection'
import type { CanvasKeyOption } from '@/features/infiniteCanvas/keySelection'
import type { CanvasBackgroundMode, CanvasNode, CanvasProject, CanvasRepository } from '@/features/infiniteCanvas/types'
import { useInfiniteCanvasStore } from '@/features/infiniteCanvas/stores/useInfiniteCanvasStore'
import { createIndexedDbCanvasRepository } from '@/features/infiniteCanvas/storage/indexedDbCanvasRepository'
import { useCanvasGeneration } from '@/features/infiniteCanvas/composables/useCanvasGeneration'

const props = defineProps<{ repository?: CanvasRepository }>()
const repository = props.repository ?? createIndexedDbCanvasRepository()
const store = useInfiniteCanvasStore(repository)
const generation = useCanvasGeneration({ store, repository, getKeySecret: (keyId) => keyId === undefined ? undefined : keySecrets.get(keyId) })
const authStore = useAuthStore()
const groups = ref<Awaited<ReturnType<typeof userGroupsAPI.getAvailable>>>([])
const eligibleKeys = ref<CanvasKeyOption[]>([])
const keySecrets = new Map<number, string>()
const keysLoading = ref(true)
const activeTab = ref<'canvas' | 'inspector'>('canvas')
const mobileDrawer = ref<'sidebar' | 'inspector' | null>(null)
const showCreateKey = ref(false)
const newKeyName = ref('Canvas image key')
const newKeyGroupId = ref<number | null>(null)
const showDelete = ref(false)
const projectToDelete = ref<string | null>(null)
const saveStatus = ref('')
const importInput = ref<HTMLInputElement>()
const warningMessage = ref('')
let mounted = false
let lifecycleGeneration = 0
let groupsGeneration = -1
const canvasGroups = computed(() => groups.value.filter((group) => group.status === 'active' && group.allow_image_generation && isCanvasImagePlatform(group.platform)))

const activeProject = computed(() => store.activeProject.value)
const selectedNode = computed<CanvasNode | null>(() => activeProject.value?.nodes.find((node) => store.selectedNodeIds.value.includes(node.id)) ?? null)
const selectedKeyId = computed(() => store.activeKeyId.value ?? null)
const lastSaved = computed(() => activeProject.value ? `Last saved ${activeProject.value.updatedAt.toLocaleString()}` : '')

function refreshEligibleKeys(items: Parameters<typeof selectEligibleCanvasKeys>[0] = []): CanvasKeyOption[] {
  eligibleKeys.value = selectEligibleCanvasKeys(items, groups.value)
  const eligibleIds = new Set(eligibleKeys.value.map((option) => option.id))
  for (const id of keySecrets.keys()) if (!eligibleIds.has(id)) keySecrets.delete(id)
  eligibleKeys.value.forEach((option) => keySecrets.set(option.id, option.key))
  const activeId = store.activeKeyId.value
  if (activeId !== undefined && !eligibleIds.has(activeId)) {
    keySecrets.delete(activeId)
    store.setActiveKey(undefined)
  }
  return eligibleKeys.value
}

async function loadKeys(preserveOnFailure = false, generation = lifecycleGeneration): Promise<CanvasKeyOption[] | undefined> {
  if (!mounted || generation !== lifecycleGeneration) return
  keysLoading.value = true
  try {
    const [firstResponse, availableGroups] = await Promise.all([keysAPI.list(1, 100), userGroupsAPI.getAvailable()])
    const allKeys = [...firstResponse.items]
    const pages = firstResponse.pages ?? 1
    for (let page = 2; page <= pages; page += 1) {
      const response = await keysAPI.list(page, 100)
      allKeys.push(...response.items)
    }
    if (!mounted || generation !== lifecycleGeneration) return undefined
    groups.value = availableGroups
    groupsGeneration = generation
    if (!availableGroups.some((group) => group.id === newKeyGroupId.value && group.status === 'active' && group.allow_image_generation && isCanvasImagePlatform(group.platform))) {
      newKeyGroupId.value = availableGroups.find((group) => group.status === 'active' && group.allow_image_generation && isCanvasImagePlatform(group.platform))?.id ?? null
    }
    return refreshEligibleKeys(allKeys)
  } catch {
    if (!mounted || generation !== lifecycleGeneration) return undefined
    if (!preserveOnFailure) eligibleKeys.value = []
    throw new Error('Failed to refresh keys')
  } finally {
    if (mounted && generation === lifecycleGeneration) keysLoading.value = false
  }
}

async function createKey() {
  const generation = lifecycleGeneration
  try {
    const created = await keysAPI.create(newKeyName.value.trim() || 'Canvas image key', newKeyGroupId.value)
    if (!mounted || generation !== lifecycleGeneration) return
    const selectedGroupId = newKeyGroupId.value === null ? null : Number(newKeyGroupId.value)
    if (groupsGeneration !== generation || selectedGroupId === null || !canvasGroups.value.some((group) => group.id === selectedGroupId)) {
      warningMessage.value = 'Image groups are still refreshing. Try creating the key again.'
      showCreateKey.value = false
      return
    }
    const group = created.group ?? groups.value.find((item) => item.id === created.group_id)
    const createdOption = group ? selectEligibleCanvasKeys([created], [group])[0] : undefined
    if (!createdOption) {
      warningMessage.value = 'The new key is not eligible for image generation in its group.'
      showCreateKey.value = false
    } else {
      keySecrets.set(created.id, created.key)
      eligibleKeys.value = [...eligibleKeys.value.filter((item) => item.id !== created.id), createdOption]
      store.setActiveKey(created.id)
      showCreateKey.value = false
    }
    void loadKeys(true, generation).then(() => {
      if (!mounted || generation !== lifecycleGeneration) return
      const refreshedOption = groupsGeneration === generation ? eligibleKeys.value.find((item) => item.id === created.id) : undefined
      if (createdOption && !refreshedOption) {
        warningMessage.value = 'The key group is no longer eligible for image generation.'
        return
      }
      if (refreshedOption && store.activeKeyId.value !== created.id) store.setActiveKey(created.id)
    }).catch(() => {
      if (mounted && generation === lifecycleGeneration) warningMessage.value = 'Key list refresh failed; the new key remains selected.'
    })
  } catch {
    if (mounted && generation === lifecycleGeneration) warningMessage.value = 'Could not create the image key.'
  }
}

function chooseKey(id: number | null) { store.setActiveKey(id ?? undefined) }
function selectProject(id: string) { store.setActiveProject(id) }
function createProject() { store.createProject(`Canvas ${store.projects.value.length + 1}`) }
function renameProject(id: string, title: string) { store.renameProject(id, title) }
function duplicateProject(id: string) { store.duplicateProject(id) }
function requestDelete(id: string) { projectToDelete.value = id; showDelete.value = true }
async function deleteProject() { if (projectToDelete.value) await store.deleteProject(projectToDelete.value); showDelete.value = false; projectToDelete.value = null }
function updateNode(patch: Partial<CanvasNode>) { if (selectedNode.value) store.updateNode(selectedNode.value.id, patch) }
function updateCanvasNode(nodeId: string, patch: Partial<CanvasNode>) { store.updateNode(nodeId, patch) }
function retryCanvasNode(nodeId: string) { void generation.retryImageNode(nodeId) }
function removeSelectedNode() { if (selectedNode.value) store.removeNode(selectedNode.value.id) }
function changeBackground(mode: CanvasBackgroundMode) { store.setBackgroundMode(mode) }
function zoom(factor: number) { const current = activeProject.value?.viewport; if (current) store.updateViewport({ ...current, zoom: Math.max(0.2, Math.min(3, current.zoom * factor)) }) }
function selectNode(nodeId: string, additive: boolean) { store.selectedNodeIds.value = additive ? [...new Set([...store.selectedNodeIds.value, nodeId])] : [nodeId] }
function moveNode(nodeId: string, position: { x: number; y: number }) { store.updateNode(nodeId, { position }) }
function connectNodes(edge: { sourceNodeId: string; targetNodeId: string; kind: 'prompt' | 'config' | 'reference' }) { store.connectNodes(edge) }
function updateViewport(viewport: CanvasProject['viewport']) {
  const current = activeProject.value?.viewport
  if (!current || (current.x === viewport.x && current.y === viewport.y && current.zoom === viewport.zoom)) return
  store.updateViewport(viewport)
}
async function saveNow() { await store.saveProject(); saveStatus.value = 'Saved'; window.setTimeout(() => { saveStatus.value = '' }, 1600) }
function exportProject() {
  if (!activeProject.value) return
  const blob = new Blob([JSON.stringify({ schemaVersion: 1, ...activeProject.value })], { type: 'application/json' })
  const url = URL.createObjectURL(blob); const link = document.createElement('a'); link.href = url; link.download = `${activeProject.value.title || 'canvas'}.json`; link.click(); URL.revokeObjectURL(url)
}
function importProject() { importInput.value?.click() }
const IMPORT_SECRET_FIELDS = new Set(['key', 'apiKey', 'secret', 'token', 'access_token', 'activeKeySecret'])
function unwrapImportPayload(data: unknown): unknown {
  if (!data || typeof data !== 'object' || Array.isArray(data)) return data
  const record = data as Record<string, unknown>
  if (!Object.prototype.hasOwnProperty.call(record, 'project')) return data
  if (Object.keys(record).some((key) => key !== 'project' || IMPORT_SECRET_FIELDS.has(key))) return undefined
  return record.project
}
async function handleImport(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]; if (!file) return
  try {
    const raw = await file.text()
    if (raw.length > 2_000_000) throw new Error('Import too large')
    const data = JSON.parse(raw) as unknown
    const payload = unwrapImportPayload(data)
    if (payload === undefined) throw new Error('Invalid canvas envelope')
    if (!store.importProject(payload)) throw new Error('Invalid canvas project')
  } catch { warningMessage.value = 'The canvas file is invalid or too large.' }
  if (importInput.value) importInput.value.value = ''
}
function clearSecrets() { keySecrets.clear(); eligibleKeys.value = []; groups.value = []; groupsGeneration = -1; newKeyGroupId.value = null }
const canCreateKey = computed(() => {
  const selectedGroupId = newKeyGroupId.value === null ? null : Number(newKeyGroupId.value)
  return groupsGeneration === lifecycleGeneration && selectedGroupId !== null && canvasGroups.value.some((group) => group.id === selectedGroupId)
})
function closeDrawer() { activeTab.value = 'canvas'; mobileDrawer.value = null }
function openDrawer(drawer: 'sidebar' | 'inspector') {
  if (mobileDrawer.value === drawer) {
    closeDrawer()
    return
  }
  activeTab.value = drawer === 'inspector' ? 'inspector' : 'canvas'
  mobileDrawer.value = drawer
}
function selectTab(tab: 'canvas' | 'inspector') {
  activeTab.value = tab
  mobileDrawer.value = tab === 'inspector' ? 'inspector' : null
}

watch(() => authStore.user?.id, () => { lifecycleGeneration += 1; clearSecrets(); void loadKeys(false, lifecycleGeneration).catch(() => undefined) })
onMounted(async () => { mounted = true; lifecycleGeneration += 1; await store.ready; await loadKeys(false, lifecycleGeneration).catch(() => undefined) })
onBeforeUnmount(() => { mounted = false; lifecycleGeneration += 1; clearSecrets() })
</script>

<template>
  <AppLayout>
    <div class="infinite-canvas-view min-h-[calc(100vh-8rem)] overflow-hidden rounded-lg bg-gray-100 shadow-sm dark:bg-dark-950 dark:bg-dark-950" data-page="infinite-canvas">
      <div class="flex min-h-[calc(100vh-8rem)] flex-col lg:flex-row">
        <CanvasProjectSidebar :projects="store.projects.value" :active-project-id="activeProject?.id" :mobile-open="mobileDrawer === 'sidebar'" @select="selectProject" @new="createProject" @rename="renameProject" @duplicate="duplicateProject" @delete="requestDelete" @import="importProject" @export="exportProject" @close="closeDrawer" />
        <section class="flex min-w-0 flex-1 flex-col">
          <CanvasToolbar :project="activeProject" :can-undo="store.canUndo.value" :can-redo="store.canRedo.value" :save-status="saveStatus || lastSaved" @background-change="changeBackground" @zoom="zoom" @undo="store.undo" @redo="store.redo" @save="saveNow" />
          <div class="flex gap-1 border-b border-gray-200 bg-white px-3 pt-2 dark:border-dark-700 dark:bg-dark-900 lg:hidden">
            <button type="button" aria-label="Open projects" data-canvas-drawer="sidebar" class="rounded-t-md px-3 py-1 text-xs" @click="openDrawer('sidebar')">Projects</button>
            <button type="button" data-canvas-tab="canvas" :class="['rounded-t-md px-3 py-1 text-xs', activeTab === 'canvas' ? 'is-active bg-gray-100 font-semibold dark:bg-dark-800' : '']" @click="selectTab('canvas')">Canvas</button>
            <button type="button" data-canvas-tab="inspector" :class="['rounded-t-md px-3 py-1 text-xs', activeTab === 'inspector' ? 'is-active bg-gray-100 font-semibold dark:bg-dark-800' : '']" @click="selectTab('inspector')">Inspector</button>
            <button type="button" aria-label="Open inspector" data-canvas-drawer="inspector" class="rounded-t-md px-3 py-1 text-xs" @click="openDrawer('inspector')">Inspect</button>
          </div>
          <div class="flex min-h-0 flex-1 flex-col lg:flex-row">
            <button v-if="mobileDrawer" type="button" aria-label="Close canvas drawer" class="fixed inset-0 z-30 bg-black/30 lg:hidden" @click="closeDrawer" />
            <div v-if="!activeProject" data-canvas-empty="projects" class="flex min-h-[420px] flex-1 items-center justify-center p-8 text-center text-sm text-gray-500 dark:text-dark-400">No projects yet. <button type="button" class="ml-1 text-primary-600 hover:underline" @click="createProject">Create a project</button></div>
            <div v-else class="relative min-h-[420px] min-w-0 flex-1 overflow-auto" :class="{ hidden: activeTab !== 'canvas' }">
              <InfiniteCanvasSurface :project="activeProject" :selected-node-ids="store.selectedNodeIds.value" @node-select="selectNode" @node-move="moveNode" @node-delete="store.removeNode" @node-update="updateCanvasNode" @node-retry="retryCanvasNode" @edge-create="connectNodes" @viewport-update="updateViewport" />
              <div v-if="!eligibleKeys.length" class="pointer-events-none absolute left-1/2 top-6 w-72 -translate-x-1/2 rounded-md border border-amber-200 bg-amber-50 p-3 text-center text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-200" data-canvas-empty="image-models">No image models available for this canvas.</div>
            </div>
            <CanvasInspector v-if="activeProject" :mobile-open="mobileDrawer === 'inspector'" :node="selectedNode" @update="updateNode" @delete="removeSelectedNode" @close="closeDrawer" />
          </div>
          <div class="border-t border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900"><CanvasKeyPicker :options="eligibleKeys" :model-value="selectedKeyId" :loading="keysLoading" @update:model-value="chooseKey" @create-key="showCreateKey = true" /></div>
        </section>
      </div>
      <p v-if="warningMessage" role="status" class="border-t border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">{{ warningMessage }}</p>
    </div>
    <input ref="importInput" type="file" accept="application/json" class="hidden" @change="handleImport" />
    <BaseDialog :show="showCreateKey" title="Create image key" width="narrow" @close="showCreateKey = false">
      <div class="space-y-3"><label class="block text-sm">Name<input v-model="newKeyName" class="mt-1 w-full rounded-md border px-3 py-2 dark:border-dark-600 dark:bg-dark-800" /></label><label class="block text-sm">Group<select v-model="newKeyGroupId" class="mt-1 w-full rounded-md border px-3 py-2 dark:border-dark-600 dark:bg-dark-800"><option :value="null">Select group</option><option v-for="group in canvasGroups" :key="group.id" :value="group.id">{{ group.name }}</option></select></label></div>
      <template #footer><button type="button" :disabled="!canCreateKey" class="rounded-md bg-primary-600 px-3 py-2 text-sm text-white disabled:cursor-not-allowed disabled:opacity-50" @click="createKey">Create</button></template>
    </BaseDialog>
    <ConfirmDialog :show="showDelete" title="Delete project" message="Delete this project and its nodes?" danger @confirm="deleteProject" @cancel="showDelete = false" />
  </AppLayout>
</template>
