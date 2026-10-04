<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { keysAPI, userGroupsAPI } from '@/api'
import { listImageModels, type ImageModel } from '@/api/imageGeneration'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import InfiniteCanvasSurface from '@/features/infiniteCanvas/components/InfiniteCanvasSurface.vue'
import CanvasProjectSidebar from '@/features/infiniteCanvas/components/CanvasProjectSidebar.vue'
import CanvasToolbar from '@/features/infiniteCanvas/components/CanvasToolbar.vue'
import CanvasInspector from '@/features/infiniteCanvas/components/CanvasInspector.vue'
import CanvasKeyPicker from '@/features/infiniteCanvas/components/CanvasKeyPicker.vue'
import { isCanvasImagePlatform, isCanvasModelAllowed, selectEligibleCanvasKeys } from '@/features/infiniteCanvas/keySelection'
import type { CanvasKeyOption } from '@/features/infiniteCanvas/keySelection'
import type { CanvasBackgroundMode, CanvasNode, CanvasProject, CanvasRepository } from '@/features/infiniteCanvas/types'
import { useInfiniteCanvasStore } from '@/features/infiniteCanvas/stores/useInfiniteCanvasStore'
import { createIndexedDbCanvasRepositoryForUser } from '@/features/infiniteCanvas/storage/indexedDbCanvasRepository'
import { useCanvasGeneration } from '@/features/infiniteCanvas/composables/useCanvasGeneration'
import { saveAs } from 'file-saver'
import { exportProject as exportCanvasProject, importProject as importCanvasProject, sanitizeExportProject } from '@/features/infiniteCanvas/storage/projectTransfer'

const props = defineProps<{ repository?: CanvasRepository }>()
function emptyRepository(): CanvasRepository {
  return { async listProjects() { return [] }, async loadProject() { return null }, async saveProject() {}, async deleteProject() {}, async saveAsset(asset) { return asset.storageKey ?? `asset-${Date.now()}` }, async loadAsset() { return undefined }, async deleteAsset() {} }
}
type SwitchableRepository = CanvasRepository & { switchTo(repository: CanvasRepository): void; closeRetired(): void }
function createSwitchableRepository(initial: CanvasRepository): SwitchableRepository {
  let delegate = initial
  const retired: CanvasRepository[] = []
  return {
    switchTo(next) {
      const previous = delegate
      delegate = next
      if (previous !== next) retired.push(previous)
    },
    closeRetired() {
      while (retired.length) {
        const previous = retired.shift()
        if (previous && 'close' in previous && typeof previous.close === 'function') previous.close()
      }
    },
    listProjects: () => delegate.listProjects(), loadProject: (id) => delegate.loadProject(id), saveProject: (project) => delegate.saveProject(project), deleteProject: (id) => delegate.deleteProject(id), saveAsset: (asset) => delegate.saveAsset(asset), loadAsset: (key) => delegate.loadAsset(key), deleteAsset: (key) => delegate.deleteAsset(key),
  }
}
const repository = createSwitchableRepository(props.repository ?? emptyRepository())
const store = useInfiniteCanvasStore(repository)
const authStore = useAuthStore()
const groups = ref<Awaited<ReturnType<typeof userGroupsAPI.getAvailable>>>([])
const eligibleKeys = ref<CanvasKeyOption[]>([])
const keySecrets = new Map<number, string>()
const imageUrls = ref<Record<string, string>>({})
const generation = useCanvasGeneration({ store, repository, getKeySecret: (keyId) => keyId === undefined ? undefined : keySecrets.get(keyId), getKeyPlatform: (keyId) => keyId === undefined ? undefined : eligibleKeys.value.find((option) => option.id === keyId)?.platform })
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
const imageModels = ref<ImageModel[]>([])
const imageModelsLoading = ref(false)
const imageModelsError = ref('')
let modelRequestGeneration = 0
let mounted = false
let lifecycleGeneration = 0
let groupsGeneration = -1
const canvasGroups = computed(() => groups.value.filter((group) => group.status === 'active' && group.allow_image_generation && isCanvasImagePlatform(group.platform)))

const activeProject = computed(() => store.activeProject.value)
const selectedNode = computed<CanvasNode | null>(() => activeProject.value?.nodes.find((node) => store.selectedNodeIds.value.includes(node.id)) ?? null)
const selectedKeyId = computed(() => store.activeKeyId.value ?? null)
const selectedKeyOption = computed(() => eligibleKeys.value.find((option) => option.id === selectedKeyId.value))
const availableImageModels = computed(() => {
  const allowlist = selectedKeyOption.value?.allowedModels
  if (allowlist === undefined) return imageModels.value
  return imageModels.value.filter((model) => isCanvasModelAllowed(model.id, allowlist))
})
const lastSaved = computed(() => activeProject.value ? `Last saved ${activeProject.value.updatedAt.toLocaleString()}` : '')
let assetHydrationGeneration = 0

function revokeImageUrls() {
  if (typeof URL !== 'undefined' && typeof URL.revokeObjectURL === 'function') Object.values(imageUrls.value).forEach((url) => URL.revokeObjectURL(url))
  imageUrls.value = {}
}

async function hydrateImageUrls(project: CanvasProject | null) {
  const generationId = ++assetHydrationGeneration
  revokeImageUrls()
  if (!project) return
  const entries = await Promise.all(project.nodes.filter((node) => node.type === 'image').map(async (node) => {
    const key = typeof node.metadata.storageKey === 'string' ? node.metadata.storageKey : typeof node.metadata.assetKey === 'string' ? node.metadata.assetKey : undefined
    if (!key) return undefined
    const asset = await repository.loadAsset(key)
    if (!asset || generationId !== assetHydrationGeneration) return undefined
    if (typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return undefined
    return [node.id, URL.createObjectURL(asset.blob)] as const
  }))
  if (generationId !== assetHydrationGeneration) {
    entries.forEach((entry) => { if (entry && typeof URL !== 'undefined' && typeof URL.revokeObjectURL === 'function') URL.revokeObjectURL(entry[1]) })
    return
  }
  imageUrls.value = Object.fromEntries(entries.filter((entry): entry is readonly [string, string] => Boolean(entry)))
}

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
function createProject() {
  void store.ready.then(() => {
    store.createProject(`Canvas ${store.projects.value.length + 1}`)
    addStarterNodes({ x: 120, y: 100 })
  })
}
function addStarterNodes(point = { x: 120, y: 100 }) {
  const project = activeProject.value ?? store.createProject(`Canvas ${store.projects.value.length + 1}`)
  if (project.nodes.some((node) => node.type === 'prompt') || project.nodes.some((node) => node.type === 'config')) return
  const prompt = store.addNode({ type: 'prompt', position: point, metadata: { prompt: '' } })
  const config = store.addNode({ type: 'config', position: { x: point.x + 300, y: point.y }, metadata: { model: '', size: '1024x1024', quality: '', count: 1, background: '' } })
  store.connectNodes({ sourceNodeId: prompt.id, targetNodeId: config.id, kind: 'prompt' })
  store.connectNodes({ sourceNodeId: config.id, targetNodeId: prompt.id, kind: 'config' })
}
function renameProject(id: string, title: string) { store.renameProject(id, title) }
function duplicateProject(id: string) { store.duplicateProject(id) }
function requestDelete(id: string) { projectToDelete.value = id; showDelete.value = true }
async function deleteProject() { if (projectToDelete.value) await store.deleteProject(projectToDelete.value); showDelete.value = false; projectToDelete.value = null }
function updateNode(patch: Partial<CanvasNode>) { if (selectedNode.value) store.updateNode(selectedNode.value.id, patch) }
function updateCanvasNode(nodeId: string, patch: Partial<CanvasNode>) { store.updateNode(nodeId, patch) }
function retryCanvasNode(nodeId: string) { void generation.retryImageNode(nodeId) }
function generateCanvasNode(nodeId: string) {
  const project = activeProject.value
  const node = project?.nodes.find((candidate) => candidate.id === nodeId)
  if (!project || !node) return
  const promptId = node.type === 'prompt' ? node.id : project.edges.find((edge) => edge.targetNodeId === node.id && edge.kind === 'prompt')?.sourceNodeId ?? project.nodes.find((candidate) => candidate.type === 'prompt')?.id
  const configId = node.type === 'config' ? node.id : project.edges.find((edge) => edge.targetNodeId === node.id && edge.kind === 'config')?.sourceNodeId ?? project.nodes.find((candidate) => candidate.type === 'config')?.id
  if (promptId && configId) void generation.generateFromNodes(promptId, configId)
}
async function removeCanvasNode(nodeId: string) {
  const url = imageUrls.value[nodeId]
  if (url && typeof URL !== 'undefined' && typeof URL.revokeObjectURL === 'function') { URL.revokeObjectURL(url); const next = { ...imageUrls.value }; delete next[nodeId]; imageUrls.value = next }
  const node = activeProject.value?.nodes.find((candidate) => candidate.id === nodeId)
  if (node?.type === 'image') await generation.removeImageNode(nodeId)
  else store.removeNode(nodeId)
}
async function removeSelectedNode() { if (selectedNode.value) await removeCanvasNode(selectedNode.value.id) }
function changeBackground(mode: CanvasBackgroundMode) { store.setBackgroundMode(mode) }
function zoom(factor: number) { const current = activeProject.value?.viewport; if (current) store.updateViewport({ ...current, zoom: Math.max(0.2, Math.min(3, current.zoom * factor)) }) }
function selectNode(nodeId: string, additive: boolean) { store.selectedNodeIds.value = additive ? [...new Set([...store.selectedNodeIds.value, nodeId])] : [nodeId] }
function moveNode(nodeId: string, position: { x: number; y: number }) { store.updateNode(nodeId, { position }) }
function connectNodes(edge: { sourceNodeId: string; targetNodeId: string; kind: 'prompt' | 'config' | 'reference' }) { store.connectNodes(edge) }
function handleEmptyCanvasDoubleClick(point: { x: number; y: number }) { addStarterNodes(point) }
function updateViewport(viewport: CanvasProject['viewport']) {
  const current = activeProject.value?.viewport
  if (!current || (current.x === viewport.x && current.y === viewport.y && current.zoom === viewport.zoom)) return
  store.updateViewport(viewport)
}
async function saveNow() { await store.saveProject(); saveStatus.value = 'Saved'; window.setTimeout(() => { saveStatus.value = '' }, 1600) }
async function exportProject() {
  if (!activeProject.value) return
  try {
    const blob = await exportCanvasProject(activeProject.value, repository)
    const safeTitle = sanitizeExportProject(activeProject.value).title.replace(/[^a-z0-9._-]+/gi, '-').replace(/^-+|-+$/g, '').slice(0, 80) || 'canvas'
    saveAs(blob, `${safeTitle}-${new Date().toISOString().slice(0, 10)}.canvas.zip`)
  } catch { warningMessage.value = 'Could not export this canvas.' }
}
function importProject() { importInput.value?.click() }
async function handleImport(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0]; if (!file) return
  warningMessage.value = 'Importing canvas...'
  try {
    const imported = await importCanvasProject(file, repository)
    if (!store.importProject(imported)) throw new Error('Invalid canvas project')
    warningMessage.value = ''
  } catch { warningMessage.value = 'The canvas file is invalid or too large.' }
  if (importInput.value) importInput.value.value = ''
}
function clearSecrets() { keySecrets.clear(); eligibleKeys.value = []; groups.value = []; groupsGeneration = -1; newKeyGroupId.value = null }
async function loadImageModels(generation = lifecycleGeneration) {
  const option = selectedKeyOption.value
  const secret = option ? keySecrets.get(option.id) : undefined
  const requestId = ++modelRequestGeneration
  imageModelsError.value = ''
  imageModels.value = []
  if (!secret) { imageModelsLoading.value = false; return }
  imageModelsLoading.value = true
  try {
    const models = await listImageModels(secret)
    if (!mounted || generation !== lifecycleGeneration || requestId !== modelRequestGeneration) return
    imageModels.value = models
  } catch {
    if (mounted && generation === lifecycleGeneration && requestId === modelRequestGeneration) imageModelsError.value = 'Could not load image models.'
  } finally {
    if (requestId === modelRequestGeneration) imageModelsLoading.value = false
  }
}
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

watch(() => authStore.user?.id, (userId) => {
  lifecycleGeneration += 1
  clearSecrets()
  if (!props.repository) repository.switchTo(userId === undefined || userId === null ? emptyRepository() : createIndexedDbCanvasRepositoryForUser(userId))
  void store.switchRepository(repository).then((applied) => {
    if (!applied) return
    repository.closeRetired()
    return loadKeys(false, lifecycleGeneration).catch(() => undefined)
  }).catch(() => undefined)
})
watch(selectedKeyOption, () => { void loadImageModels() })
watch(() => activeProject.value?.id, (id, previous) => { if (id !== previous) void hydrateImageUrls(activeProject.value) })
watch(() => activeProject.value?.nodes.map((node) => `${node.id}:${typeof node.metadata.storageKey === 'string' ? node.metadata.storageKey : typeof node.metadata.assetKey === 'string' ? node.metadata.assetKey : ''}`).join('|'), () => { void hydrateImageUrls(activeProject.value) })
onMounted(async () => {
  mounted = true
  lifecycleGeneration += 1
  if (!props.repository && authStore.user?.id !== undefined && authStore.user?.id !== null) repository.switchTo(createIndexedDbCanvasRepositoryForUser(authStore.user.id))
  await store.ready
  if (!props.repository && authStore.user?.id !== undefined && authStore.user?.id !== null) {
    const applied = await store.switchRepository(repository)
    if (applied) repository.closeRetired()
  }
  await hydrateImageUrls(activeProject.value)
  await loadKeys(false, lifecycleGeneration).catch(() => undefined)
})
onBeforeUnmount(() => { mounted = false; lifecycleGeneration += 1; assetHydrationGeneration += 1; revokeImageUrls(); clearSecrets() })
</script>

<template>
  <AppLayout>
    <div class="infinite-canvas-view min-h-[calc(100vh-8rem)] overflow-hidden rounded-lg bg-gray-100 shadow-sm dark:bg-dark-950 dark:bg-dark-950" data-page="infinite-canvas">
      <div class="flex min-h-[calc(100vh-8rem)] flex-col lg:flex-row">
        <CanvasProjectSidebar :projects="store.projects.value" :active-project-id="activeProject?.id" :mobile-open="mobileDrawer === 'sidebar'" @select="selectProject" @new="createProject" @rename="renameProject" @duplicate="duplicateProject" @delete="requestDelete" @import="importProject" @export="exportProject" @close="closeDrawer" />
        <section class="flex min-w-0 flex-1 flex-col">
          <CanvasToolbar :project="activeProject" :can-undo="store.canUndo.value" :can-redo="store.canRedo.value" :save-status="saveStatus || lastSaved" @background-change="changeBackground" @zoom="zoom" @undo="store.undo" @redo="store.redo" @save="saveNow" @add-nodes="addStarterNodes" />
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
              <InfiniteCanvasSurface :project="activeProject" :image-urls="imageUrls" :selected-node-ids="store.selectedNodeIds.value" :image-models="availableImageModels" :image-models-loading="imageModelsLoading" :image-models-error="imageModelsError" @node-select="selectNode" @node-move="moveNode" @node-delete="removeCanvasNode" @node-update="updateCanvasNode" @node-retry="retryCanvasNode" @node-generate="generateCanvasNode" @edge-create="connectNodes" @viewport-update="updateViewport" @empty-canvas-double-click="handleEmptyCanvasDoubleClick" @retry-image-models="loadImageModels" />
              <div v-if="selectedKeyId && !imageModelsLoading && !imageModelsError && !availableImageModels.length" class="pointer-events-none absolute left-1/2 top-6 w-72 -translate-x-1/2 rounded-md border border-amber-200 bg-amber-50 p-3 text-center text-xs text-amber-800 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-200" data-canvas-empty="image-models">No image models available for this key.</div>
            </div>
            <CanvasInspector v-if="activeProject" :mobile-open="mobileDrawer === 'inspector'" :node="selectedNode" @update="updateNode" @delete="removeSelectedNode" @close="closeDrawer" />
          </div>
          <div class="border-t border-gray-200 bg-white p-3 dark:border-dark-700 dark:bg-dark-900"><CanvasKeyPicker :options="eligibleKeys" :model-value="selectedKeyId" :loading="keysLoading" @update:model-value="chooseKey" @create-key="showCreateKey = true" /></div>
        </section>
      </div>
      <p v-if="warningMessage" role="status" class="border-t border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">{{ warningMessage }}</p>
    </div>
    <input ref="importInput" type="file" accept=".zip,.canvas.zip,application/zip" class="hidden" @change="handleImport" />
    <BaseDialog :show="showCreateKey" title="Create image key" width="narrow" @close="showCreateKey = false">
      <div class="space-y-3"><label class="block text-sm">Name<input v-model="newKeyName" class="mt-1 w-full rounded-md border px-3 py-2 dark:border-dark-600 dark:bg-dark-800" /></label><label class="block text-sm">Group<select v-model="newKeyGroupId" class="mt-1 w-full rounded-md border px-3 py-2 dark:border-dark-600 dark:bg-dark-800"><option :value="null">Select group</option><option v-for="group in canvasGroups" :key="group.id" :value="group.id">{{ group.name }}</option></select></label></div>
      <template #footer><button type="button" :disabled="!canCreateKey" class="rounded-md bg-primary-600 px-3 py-2 text-sm text-white disabled:cursor-not-allowed disabled:opacity-50" @click="createKey">Create</button></template>
    </BaseDialog>
    <ConfirmDialog :show="showDelete" title="Delete project" message="Delete this project and its nodes?" danger @confirm="deleteProject" @cancel="showDelete = false" />
  </AppLayout>
</template>
