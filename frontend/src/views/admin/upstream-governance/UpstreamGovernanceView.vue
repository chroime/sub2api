<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { SMART_OPERATIONS_SECTIONS, LEGACY_SMART_OPERATIONS_SECTIONS, isLegacySmartOperationsSection, resolveSmartOperationsSection, type SmartOperationsSection } from '@/config/smartOperations'
import SmartOperationsTabs from './SmartOperationsTabs.vue'
import GovernanceWorkbench from './GovernanceWorkbench.vue'
import StatusIndicator from './StatusIndicator.vue'
import GovernanceTimeline from './GovernanceTimeline.vue'
import { errorKey, siteStateKeys } from './feedback'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImportPanel from './ImportPanel.vue'
import ConnectDialog from './ConnectDialog.vue'
import OnboardDialog from './OnboardDialog.vue'
import SiteEditDialog from './SiteEditDialog.vue'
import SiteOverview from './SiteOverview.vue'
import GovernanceHistory from './GovernanceHistory.vue'
import GovernanceSitesOverview from './GovernanceSitesOverview.vue'
import AutomationPolicyPanel from './AutomationPolicyPanel.vue'
import ReconciliationPanel from './ReconciliationPanel.vue'
import BalanceCard from './BalanceCard.vue'
import DeliveryRecords from './DeliveryRecords.vue'
import ManagedKeysPanel from './ManagedKeysPanel.vue'
import ModelMonitorPanel from './ModelMonitorPanel.vue'
import ObservationPricingPanel from './ObservationPricingPanel.vue'
import GlobalSettingsView from './GlobalSettingsView.vue'
import ImportTemplatesView from './ImportTemplatesView.vue'
import AccountQualityView from '../account-quality/AccountQualityView.vue'
import Icon from '@/components/icons/Icon.vue'
import api, {
  type Site,
  type Snapshot,
  type Binding,
  type ManagedKey,
  type KeySelection,
  type AutomationConfiguration,
  type BalanceHealth,
  type ReadinessOverview,
} from '@/api/admin/upstream-governance'
import groupsAPI from '@/api/admin/groups'
import proxiesAPI from '@/api/admin/proxies'
import type { AdminGroup } from '@/types'
import modelMonitoringAPI, { type LocalModelWorkspace } from '@/api/admin/upstream-model-monitoring'
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const tab = computed({
  get: () => resolveSmartOperationsSection(route.params.section),
  set: (section: SmartOperationsSection) => {
    if (navigationLocked.value) return
    const entry = SMART_OPERATIONS_SECTIONS.find(item => item.id === section)!
    const query = { ...route.query }
    if (section === 'settings' || section === 'account-quality' || section === 'records') delete query.site
    // Model monitoring always reopens on upstream groups.
    delete query.target
    void router.push({ path: entry.path, query })
  },
})
// Model monitoring checks either the selected upstream's group keys or this
// service's own local group keys (the former local-monitor section).
const modelTarget = computed<'upstream' | 'local'>(() => route.query.target === 'local' ? 'local' : 'upstream')
const localModels = computed(() => tab.value === 'models' && modelTarget.value === 'local')
function chooseModelTarget(target: 'upstream' | 'local') {
  if (navigationLocked.value || (tab.value === 'models' && target === modelTarget.value)) return
  const query = { ...route.query }
  if (target === 'local') {
    query.target = 'local'
    delete query.site
  } else delete query.target
  void router.push({ path: '/admin/upstream-governance/models', query })
}
const groupsView = ref<'import' | 'accounts'>('import')
const settingsView = ref<'settings' | 'templates'>('settings')
const recordsChannel = computed<'email' | 'astrbot' | 'users'>(() => route.query.channel === 'astrbot' || route.query.channel === 'users' ? route.query.channel : 'email')
const recordsTrigger = computed(() => route.query.trigger_type === 'manual_batch' || route.query.trigger_type === 'automatic' ? route.query.trigger_type : undefined)
function chooseRecordsChannel(channel: 'email' | 'astrbot' | 'users') {
  void router.replace({ path: '/admin/upstream-governance/records', query: { ...route.query, channel } })
}
// Old links (import, monitor, local-monitor, notifications) move to the
// regrouped sections without leaving their id in the address bar.
watch(() => route.params.section, value => {
  if (!isLegacySmartOperationsSection(value)) return
  const section = LEGACY_SMART_OPERATIONS_SECTIONS[value]
  const query = { ...route.query }
  if (value === 'local-monitor') {
    query.target = 'local'
    delete query.site
  }
  if (value === 'notifications') {
    // Preserve old links to platform-user notification records.
    query.channel = 'users'
    delete query.site
  }
  void router.replace({ path: SMART_OPERATIONS_SECTIONS.find(item => item.id === section)!.path, query })
}, { immediate: true })
const showOverview = ref(true)
const siteNotFound = ref(false)
let sitesLoaded = false
let sitesInitializing = false
let siteRefreshTimer: ReturnType<typeof setInterval> | undefined
let summaryRefreshing = false
const automation = ref<AutomationConfiguration | null>(null), balanceHealth = ref<BalanceHealth | null>(null), readiness = ref<ReadinessOverview | null>(null)
const readinessLoading = ref(false), readinessReadFailed = ref(false)
const automationBusy = ref(false), observationBusy = ref(false), pricingBusy = ref(false), reconciliationBusy = ref(false), keyBusy = ref(false), modelBusy = ref(false)
const qualityBusy = ref(false), templatesBusy = ref(false)
const siteIndependent = computed(() => tab.value === 'settings' || tab.value === 'account-quality' || tab.value === 'records' || localModels.value)
const localWorkspaceLoading = ref(false), localWorkspaceError = ref('')
const keysOpen = ref(false), keySelections = ref<KeySelection[]>([]), reconciliationEpoch = ref(0)
const importPreviewEpoch = ref(0)
const importBusy = ref(false)
const balanceBusy = ref(false)
const editBusy = ref(false)
let generation = 0
let healthGeneration = 0
let localWorkspaceGeneration = 0
const sites = ref<Site[]>([]),
  active = ref<Site | null>(null),
  localWorkspace = ref<LocalModelWorkspace | null>(null),
  snapshot = ref<Snapshot | null>(null),
  overviewSnapshot = ref<Snapshot | null>(null),
  bindings = ref<Binding[]>([])
const managedKeys = ref<ManagedKey[]>([]), importStateReady = ref(false)
const groups = ref<AdminGroup[]>([]),
  proxies = ref<{ id: number; name: string }[]>([])
const busy = ref(false),
  actionBusy = ref(false),
  error = ref(''),
  connecting = ref(false),
  onboarding = ref(false),
  editing = ref(false),
  deleting = ref(false)
const deleteKeepAccounts = ref(false), notice = ref('')
const deleteError = ref('')
const collectionBusyNotice = ref<number | null>(null)
// Only a direct collection failure can repeat the site's persisted error.
// Metadata reads may fail independently even when their error text is equal.
const collectionError = ref<{ reason: string; siteId: number } | null>(null)
const activeErrorReason = computed(() => active.value?.status === 'reauth_required' ? 'reauth_required' : active.value?.last_error || '')
const showSiteError = computed(() => {
  const repeated = error.value && collectionError.value && collectionError.value.siteId === active.value?.id && collectionError.value.reason === activeErrorReason.value
  return Boolean(activeErrorReason.value) && !repeated
})
const siteMenuOpen = ref(false)
const mutationBusy = computed(() => actionBusy.value || importBusy.value || balanceBusy.value || editBusy.value || automationBusy.value || observationBusy.value || pricingBusy.value || reconciliationBusy.value || keyBusy.value || modelBusy.value || qualityBusy.value || templatesBusy.value)
const working = computed(() => busy.value || mutationBusy.value)
const navigationLocked = computed(() => mutationBusy.value || connecting.value || onboarding.value)
// The sidebar can navigate independently of this view. Protect in-flight writes there too.
const stopNavigationGuard = router.beforeEach((to, from) => {
  // Authentication redirects must take precedence over preserving an operation.
  if (to.path === '/login') return
  if (to.fullPath !== from.fullPath && navigationLocked.value) return false
})
function chooseSite(site: Site) {
  if (navigationLocked.value) return
  // The rail stays on pages that have no site; picking one opens its overview.
  if (siteIndependent.value) {
    void router.push({ path: '/admin/upstream-governance', query: { site: String(site.id) } })
    return
  }
  if (!showOverview.value && active.value?.id === site.id) {
    // Keep successful loads and unsaved drafts. A failed read can be retried explicitly.
    if (error.value && !working.value) void select(site)
    return
  }
  // Commit the route first so a rejected navigation cannot change the active account.
  void router.push({ path: route.path, query: { ...route.query, site: String(site.id) } })
}
function showAllSiteTasks() {
  if (navigationLocked.value) return
  const query = { ...route.query }
  delete query.site
  void router.push({ path: '/admin/upstream-governance', query })
}
function navigateFromReadiness(target: ReadinessOverview['checks'][number]['target_tab']) {
  if (navigationLocked.value) return
  // Readiness items still name the pre-regrouping sections.
  if (target === 'local-monitor') {
    chooseModelTarget('local')
    return
  }
  tab.value = resolveSmartOperationsSection(target)
}
function navigateFromWorkbench(target: { siteId: number; section: SmartOperationsSection }) {
  if (navigationLocked.value || !Number.isSafeInteger(target?.siteId) || !sites.value.some(site => site.id === target.siteId)) return
  const section = SMART_OPERATIONS_SECTIONS.find(item => item.id === target.section)
  if (!section) return
  // A task opens its existing destination only; it never authorizes, repairs,
  // probes or applies a policy simply because the administrator navigated.
  void router.push({ path: section.path, query: { ...route.query, site: String(target.siteId) } })
}
function restoreLocation() {
  if (!sitesLoaded) return
  if (siteIndependent.value) {
    generation++
    active.value = null
    siteNotFound.value = false
    showOverview.value = true
    return
  }
  const value = route.query.site
  if (value === undefined) {
    siteNotFound.value = false
    showOverview.value = true
    return
  }
  const id = typeof value === 'string' && /^[1-9]\d*$/.test(value) ? Number(value) : NaN
  const site = Number.isSafeInteger(id) ? sites.value.find(item => item.id === id) : undefined
  siteNotFound.value = !site
  if (!site) {
    generation++
    busy.value = false
    active.value = null
    showOverview.value = true
    return
  }
  if (active.value?.id === id) {
    showOverview.value = false
    return
  }
  void select(site, undefined, false)
}
watch(() => route.fullPath, () => {
  collectionBusyNotice.value = null
  // Do not leave an old site's credentials or action dialog over a new route.
  const refreshKeys = keysOpen.value
  const previousSite = active.value?.id
  keysOpen.value = false
  keySelections.value = []
  editing.value = false
  deleting.value = false
  restoreLocation()
  // Closing with browser navigation must reconcile newly created keys just like
  // the dialog's close button, including when the site is cached behind the picker.
  if (refreshKeys && previousSite === active.value?.id) void reloadBindings()
})
function clearFailure() {
  error.value = ''
  collectionError.value = null
  collectionBusyNotice.value = null
}
function failure(e: unknown, collectionSiteId?: number) {
  error.value = t(errorKey(e))
  const reason = (e as { reason?: unknown } | null)?.reason
  collectionError.value = collectionSiteId && typeof reason === 'string' && reason !== '' ? { reason, siteId: collectionSiteId } : null
}
function reportSelectionReadFailure(kind: 'catalog' | 'bindings' | 'keys', e: unknown) {
  // Selecting a site hydrates several panels in parallel. A panel that is not
  // visible must not turn the whole workspace into a generic operation error;
  // the panel itself already renders its unavailable/empty state.
  if (kind === 'catalog') failure(e)
}
async function run(action: () => Promise<void>, writes = false, options: { scope?: 'delete'; onFailure?: (e: unknown) => void } = {}) {
  const request = generation
  busy.value = true
  if (writes) actionBusy.value = true
  if (options.scope === 'delete') deleteError.value = ''
  else clearFailure()
  try {
    await action()
  } catch (e) {
    if (request === generation) {
      if (options.scope === 'delete') deleteError.value = t(errorKey(e))
      else if (options.onFailure) options.onFailure(e)
      else failure(e)
    }
  } finally {
    if (writes) actionBusy.value = false
    if (request === generation) busy.value = false
  }
}
async function load() {
  const request = generation
  await run(async () => {
    const data = await api.list()
    if (request !== generation) return
    sites.value = data
    if (active.value)
      active.value = data.find((s) => s.id === active.value?.id) || null
    if (sitesLoaded) restoreLocation()
  })
}
async function loadLocalWorkspace() {
  if (localWorkspaceLoading.value) return
  if (route.query.site !== undefined || !localModels.value || !showOverview.value) return
  const request = ++localWorkspaceGeneration
  localWorkspaceLoading.value = true
  localWorkspaceError.value = ''
  try {
    const workspace = await modelMonitoringAPI.localWorkspace()
    if (request !== localWorkspaceGeneration || !localModels.value || !showOverview.value) return
    localWorkspace.value = workspace
  } catch (e) {
    if (request !== localWorkspaceGeneration || !localModels.value || !showOverview.value) return
    localWorkspace.value = null
    localWorkspaceError.value = t('governance.modelMonitoring.localWorkspaceUnavailable')
  } finally {
    if (request === localWorkspaceGeneration) localWorkspaceLoading.value = false
  }
}
function retryLocalWorkspace() {
  if (navigationLocked.value) return
  void loadLocalWorkspace()
}
watch([tab, localModels, showOverview, () => route.query.site], ([section, local, overview, siteQuery]) => {
  if (isLegacySmartOperationsSection(route.params.section)) return
  if ((section === 'account-quality' || section === 'settings' || section === 'records' || local) && siteQuery !== undefined) {
    const query = { ...route.query }
    delete query.site
    void router.replace({ path: route.path, query })
    return
  }
  if (local && overview && siteQuery === undefined) void loadLocalWorkspace()
  else {
    ++localWorkspaceGeneration
    localWorkspaceLoading.value = false
  }
}, { immediate: true })
async function refreshSiteSummaries() {
  if (summaryRefreshing || navigationLocked.value || busy.value) return
  summaryRefreshing = true
  const request = generation
  try {
    const data = await api.list()
    if (request !== generation || navigationLocked.value) return
    sites.value = data
    if (active.value) active.value = data.find(site => site.id === active.value?.id) || null
    restoreLocation()
  } catch {
    // Keep the last visible operational state when a background read fails.
  } finally {
    summaryRefreshing = false
  }
}
async function select(site: Site, collected?: Snapshot, updateLocation = true) {
  if (mutationBusy.value) return
  const request = ++generation
  connecting.value = false
  editing.value = false
  deleting.value = false
  active.value = site
  showOverview.value = false
  siteNotFound.value = false
  if (updateLocation) void router.push({ path: route.path, query: { ...route.query, site: String(site.id) } })
  keysOpen.value = false
  keySelections.value = []
  automation.value = null
  balanceHealth.value = null
  snapshot.value = null
  overviewSnapshot.value = null
  readiness.value = null
  readinessLoading.value = true
  readinessReadFailed.value = false
  bindings.value = []
  managedKeys.value = []
  importStateReady.value = false
  busy.value = true
  clearFailure()
  const results = await Promise.allSettled([
    collected ? Promise.resolve(collected) : api.catalog(site.id),
    api.bindings(site.id),
    api.keys(site.id),
    api.automation(site.id),
    api.balanceHealth(site.id),
    api.readiness(site.id),
  ])
  if (request === generation && active.value?.id === site.id) {
    if (results[0].status === 'fulfilled') snapshot.value = overviewSnapshot.value = results[0].value
    else if ((results[0].reason as { status?: number }).status !== 404)
      reportSelectionReadFailure('catalog', results[0].reason)
    if (results[1].status === 'fulfilled') bindings.value = results[1].value
    else reportSelectionReadFailure('bindings', results[1].reason)
    if (results[2].status === 'fulfilled') managedKeys.value = results[2].value
    else reportSelectionReadFailure('keys', results[2].reason)
    if (results[3].status === 'fulfilled') automation.value = results[3].value
    if (results[4].status === 'fulfilled') balanceHealth.value = results[4].value
    if (results[5].status === 'fulfilled') {
      readiness.value = results[5].value
      readinessReadFailed.value = false
    } else {
      readiness.value = null
      readinessReadFailed.value = true
    }
    readinessLoading.value = false
    importStateReady.value = results[1].status === 'fulfilled' && results[2].status === 'fulfilled'
    busy.value = false
  }
}
async function edited(site: Site) {
  editing.value = false
  editBusy.value = false
  siteCreated(site)
  await select(site)
}
async function sync() {
  if (!active.value) return
  const id = active.value.id
  const request = generation
  let collectionFailed = false
  const directCollectionBusy = (e: unknown) => collectionFailed && (e as { reason?: string } | null)?.reason === 'site_busy'
  await run(async () => {
    try {
      const collected = await api.sync(id).catch((e: unknown) => { collectionFailed = true; throw e })
      const [list, currentBindings, currentKeys, currentReadiness] = await Promise.all([api.list(), api.bindings(id), api.keys(id), api.readiness(id).catch(() => null)])
      if (request !== generation) return
      snapshot.value = collected
      overviewSnapshot.value = collected
      sites.value = list
      active.value = list.find((s) => s.id === id) || null
      bindings.value = currentBindings
      managedKeys.value = currentKeys
      readiness.value = currentReadiness
      importStateReady.value = true
      await reloadHealth()
    } catch (e) {
      if (request === generation && !directCollectionBusy(e)) importStateReady.value = false
      throw e
    }
  }, true, { onFailure: e => {
    if (directCollectionBusy(e)) collectionBusyNotice.value = id
    else failure(e, collectionFailed ? id : undefined)
  } })
}
async function remove() {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  let removed = false
  let nextSite: Site | undefined
  const keepAccounts = deleteKeepAccounts.value
  await run(async () => {
    const result = await api.remove(id, { keepAccounts })
    if (keepAccounts) notice.value = t('governance.deleteDialog.keptNotice', { count: result?.kept_accounts ?? 0 })
    const list = await api.list()
    if (request !== generation) return
    active.value = null
    clearFailure()
    deleting.value = false
    sites.value = list
    snapshot.value = null
    overviewSnapshot.value = null
    bindings.value = []
    managedKeys.value = []
    importStateReady.value = false
    showOverview.value = true
    nextSite = list[0]
    removed = true
  }, true, { scope: 'delete' })
  // Internal post-delete navigation must happen after releasing the write lock.
  if (removed) {
    if (nextSite) await select(nextSite)
    else {
      const query = { ...route.query }
      delete query.site
      await router.replace({ path: route.path, query })
    }
  }
}
async function connected(site?: Site) {
  connecting.value = false
  if (!active.value || (site && site.id !== active.value.id)) return
  const updated = site || {
    ...active.value,
    has_credential: true,
    status: 'connected',
    last_error: '',
  }
  siteCreated(updated)
  active.value = updated
  await sync()
}
function siteCreated(site: Site) {
  sites.value = [...sites.value.filter((s) => s.id !== site.id), site]
}
function balanceSaved(site: Site) {
  siteCreated(site)
  if (active.value?.id === site.id) active.value = site
  void reloadHealth()
  void reloadReadiness()
}
async function reloadBindings() {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  try {
    // Readiness is derived from bindings and managed-key health too. Refresh it
    // alongside the metadata, but do not let an optional checklist read block
    // the import workbench when the endpoint is temporarily unavailable.
    const [value, keys, currentReadiness] = await Promise.all([
      api.bindings(id),
      api.keys(id),
      api.readiness(id).catch(() => null),
    ])
    if (request === generation) {
      bindings.value = value
      managedKeys.value = keys
      if (currentReadiness) readiness.value = currentReadiness
      importStateReady.value = true
    }
  } catch (e) {
    if (request === generation) {
      importStateReady.value = false
      failure(e)
    }
  }
}
async function onboarded(site: Site, collected: Snapshot) {
  onboarding.value = false
  const updated = {
    ...site,
    has_credential: true,
    status: 'healthy',
    last_error: '',
    last_sync_at: collected.created_at,
  }
  siteCreated(updated)
  await select(updated, collected)
}
// Deleting a site that still owns imported accounts needs the keep-accounts path.
const importedAccountCount = computed(() => bindings.value.filter(binding => binding.account_id > 0 && !binding.account_deleted).length)
function openDelete() {
  deleteError.value = ''
  deleteKeepAccounts.value = importedAccountCount.value > 0 || managedKeys.value.length > 0
  deleting.value = true
}
function openKeys(selections: KeySelection[] = []) {
  if (working.value || !active.value) return
  keySelections.value = [...selections]
  keysOpen.value = true
}
// Import templates are shared settings; the import page only picks them.
function manageImportTemplates() {
  if (navigationLocked.value) return
  settingsView.value = 'templates'
  tab.value = 'settings'
}
function openLocalKeys() {
  if (!working.value) void router.push('/keys')
}
async function closeKeys() {
  if (keyBusy.value) return
  keysOpen.value = false
  await reloadBindings()
}
async function keyRepaired() {
  importPreviewEpoch.value++
  await reloadBindings()
}
async function reloadHealth() {
  if (!active.value) return
  const id = active.value.id, request = generation, healthRequest = ++healthGeneration
  try {
    const [health, latest] = await Promise.all([
      api.balanceHealth(id),
      api.catalog(id).catch(e => {
        if ((e as { status?: number }).status === 404) return null
        throw e
      }),
    ])
    if (request === generation && healthRequest === healthGeneration) {
      balanceHealth.value = health
      overviewSnapshot.value = latest
    }
  } catch (e) {
    if (request === generation && healthRequest === healthGeneration) {
      balanceHealth.value = null
      overviewSnapshot.value = null
      failure(e)
    }
  }
}
async function reloadReadiness() {
  if (!active.value) return
  const id = active.value.id
  const request = generation
  readinessLoading.value = true
  readinessReadFailed.value = false
  try {
    const value = await api.readiness(id)
    if (request === generation && active.value?.id === id) {
      readiness.value = value
      readinessReadFailed.value = false
    }
  } catch {
    if (request === generation && active.value?.id === id) readinessReadFailed.value = true
  } finally {
    if (request === generation && active.value?.id === id) readinessLoading.value = false
  }
}
async function reloadAfterPolicyChange() {
  await Promise.all([reloadHealth(), reloadReadiness()])
}
async function reloadAutomation() {
  if (!active.value) return
  const id = active.value.id, request = generation
  try { const value = await api.automation(id); if (request === generation) automation.value = value }
  catch (e) { if (request === generation) failure(e) }
}
function automationSaved(value: AutomationConfiguration) {
  automation.value = value
  reconciliationEpoch.value++
  void reloadReadiness()
}
async function reconciled() {
  if (!active.value) return
  await reloadBindings()
  await reloadHealth()
}
async function initializeSites() {
  if (sitesLoaded || sitesInitializing) return
  sitesInitializing = true
  await load()
  await run(async () => {
    const [g, p] = await Promise.all([groupsAPI.getAll(), proxiesAPI.getAll()])
    groups.value = g
    proxies.value = p.map(({ id, name }) => ({ id, name }))
  })
  sitesLoaded = true
  sitesInitializing = false
  restoreLocation()
  siteRefreshTimer = setInterval(() => { void refreshSiteSummaries() }, 60_000)
}
watch(() => active.value?.id, () => { siteMenuOpen.value = false })
// The site rail is shown on every section, so the list always loads.
onMounted(() => { void initializeSites() })
onUnmounted(() => {
  generation++
  localWorkspaceGeneration++
  if (siteRefreshTimer) clearInterval(siteRefreshTimer)
  stopNavigationGuard()
})
</script>
<template>
  <AppLayout>
    <div class="min-w-0 space-y-5 text-gray-900 dark:text-gray-100">
      <header class="flex flex-wrap items-end justify-between gap-4">
        <div class="min-w-0">
          <h2 class="text-2xl font-semibold tracking-tight">{{ t('governance.smartOperations.title') }}</h2>
        </div>
        <div v-if="tab !== 'records'" class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" :disabled="working || navigationLocked" @click="load"><Icon name="refresh" size="sm" class="mr-2" />{{ t('common.refresh') }}</button>
          <button id="governance-add-site" class="btn btn-primary" :disabled="working || navigationLocked" @click="onboarding = true"><Icon name="plus" size="sm" class="mr-2" />{{ t('governance.add') }}</button>
        </div>
      </header>
      <div class="grid min-w-0 items-start gap-5" :class="tab !== 'records' ? 'xl:grid-cols-[17.5rem_minmax(0,1fr)]' : ''" data-test="smart-operations-workspace">
      <aside v-if="tab !== 'records'" class="min-w-0 space-y-3 xl:sticky xl:top-24" data-test="smart-operations-site-rail" :aria-label="t('governance.upstreamSites')">
        <button id="governance-all-sites" class="flex min-h-11 w-full items-center gap-2 rounded-xl border px-4 py-3 text-left text-sm font-medium transition-colors disabled:opacity-50" :class="showOverview && tab === 'overview' ? 'border-primary-200 bg-primary-50 text-primary-800 dark:border-primary-800 dark:bg-primary-900/20 dark:text-primary-200' : 'border-gray-200 bg-white text-gray-700 hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200'" type="button" :disabled="navigationLocked" :aria-current="showOverview && tab === 'overview' ? 'page' : undefined" @click="showAllSiteTasks"><Icon name="globe" size="sm" />{{ t('governance.workbench.allSitesEntry') }}</button>
        <GovernanceSitesOverview compact :sites="sites" :selected-site-id="showOverview ? null : active?.id" :disabled="navigationLocked" @select="chooseSite" />
      </aside>
      <div class="min-w-0 space-y-4" data-test="smart-operations-content">
      <SmartOperationsTabs v-model="tab" :disabled="navigationLocked" />
      <div :id="`governance-${tab}-panel`" role="tabpanel" :aria-labelledby="`governance-${tab}-tab`" tabindex="0" class="min-w-0 space-y-5 rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">
      <p v-if="error" role="alert" data-test="governance-error" class="rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-600 dark:border-red-900 dark:bg-red-900/10">{{ error }}</p>
      <p v-if="collectionBusyNotice === active?.id" role="status" data-test="collection-busy-notice" class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-900/10 dark:text-amber-300">{{ t('governance.collectionBusy') }}</p>
      <p v-if="notice" role="status" data-test="governance-notice" class="rounded-xl border border-primary-100 bg-primary-50 p-3 text-sm text-primary-800 dark:border-primary-900/40 dark:bg-primary-900/10 dark:text-primary-300">{{ notice }}</p>
      <p v-if="siteNotFound" role="status" class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-900/20 dark:text-amber-300">{{ t('governance.smartOperations.siteNotFound') }}</p>
      <template v-if="tab === 'settings'">
        <div class="flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800" role="group" data-test="settings-views">
          <button v-for="view in (['settings', 'templates'] as const)" :key="view" type="button" :data-test="'settings-view-' + view" class="min-h-9 rounded-md px-3 text-sm transition-colors" :class="settingsView === view ? 'bg-white font-medium text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500'" :aria-pressed="settingsView === view" @click="settingsView = view">{{ t(`governance.settingsTab.${view}`) }}</button>
        </div>
        <GlobalSettingsView v-if="settingsView === 'settings'" />
        <ImportTemplatesView v-else-if="settingsView === 'templates'" @busy="templatesBusy = $event" />
      </template>
      <DeliveryRecords v-else-if="tab === 'records'" :initial-channel="recordsChannel" :initial-trigger="recordsTrigger" @update:channel="chooseRecordsChannel" />
      <AccountQualityView v-else-if="tab === 'account-quality'" @busy="qualityBusy = $event" />
      <GovernanceWorkbench v-else-if="tab === 'overview' && !siteNotFound && (showOverview || active)" :site-id="showOverview ? undefined : active?.id" :disabled="navigationLocked" @navigate="navigateFromWorkbench" />
      <div v-if="tab === 'models'" class="flex flex-wrap items-center gap-3">
        <div class="flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800" role="group" data-test="model-target-switch" :aria-label="t('governance.modelsTab.target')">
          <button v-for="target in (['upstream', 'local'] as const)" :key="target" type="button" :data-test="'model-target-' + target" class="min-h-9 rounded-md px-3 text-sm transition-colors disabled:opacity-50" :class="modelTarget === target ? 'bg-white font-medium text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500'" :aria-pressed="modelTarget === target" :disabled="navigationLocked" @click="chooseModelTarget(target)">{{ t(`governance.modelsTab.${target}`) }}</button>
        </div>

      </div>
      <section v-if="localModels && showOverview && localWorkspaceLoading" data-test="local-model-workspace-loading" class="min-w-0 rounded-xl border border-dashed border-gray-200 bg-white p-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:bg-dark-800">{{ t('governance.modelMonitoring.localWorkspaceLoading') }}</section>
      <section v-else-if="localModels && showOverview && localWorkspaceError" data-test="local-model-workspace-error" class="min-w-0 rounded-xl border border-amber-200 bg-amber-50 p-5 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-900/20 dark:text-amber-300">
        <p>{{ localWorkspaceError }}</p><p class="mt-2 text-xs">{{ t('governance.modelMonitoring.localWorkspaceUnavailableHint') }}</p><div class="mt-4 flex flex-wrap gap-3"><button type="button" class="btn btn-secondary text-sm" data-test="local-workspace-retry" :disabled="navigationLocked" @click="retryLocalWorkspace">{{ t('governance.modelMonitoring.retry') }}</button><button type="button" class="btn btn-secondary text-sm" data-test="local-workspace-manage-keys" :disabled="navigationLocked" @click="openLocalKeys">{{ t('governance.viewKeys') }}</button></div>
      </section>
      <section v-else-if="localModels && showOverview && localWorkspace" data-test="local-model-workspace" class="min-w-0 space-y-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800 sm:p-5">

        <ModelMonitorPanel :key="`local-${localWorkspace.site.id}`" :site-id="localWorkspace.site.id" :remote-groups="[]" :managed-keys="[]" :local-only="true" :local-targets="localWorkspace.targets || []" :disabled="working" @busy="modelBusy = $event" @manage-keys="openLocalKeys" />
      </section>
      <div v-if="showOverview && sites.length && tab !== 'overview' && !siteIndependent" data-test="site-selection-empty" class="flex min-h-64 flex-col items-center justify-center gap-4 rounded-xl border border-dashed border-gray-200 bg-white p-8 text-center dark:border-dark-600 dark:bg-dark-800">
        <span class="rounded-2xl bg-gray-100 p-4 dark:bg-dark-700"><Icon name="server" size="lg" class="text-gray-500" /></span>
        <div><h3 class="text-base font-semibold">{{ t('governance.smartOperations.chooseSite') }}</h3><p class="mt-2 max-w-sm text-sm leading-relaxed text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.chooseSiteHint') }}</p></div>
      </div>
      <p v-if="busy && !active" role="status" class="p-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-if="!busy && !sites.length && !siteIndependent" class="rounded-xl border border-dashed border-gray-200 p-8 text-center text-sm text-gray-500 dark:border-dark-600">{{ t('governance.empty') }}</p>
      <section v-if="active && !siteIndependent" v-show="!showOverview" data-test="active-site-workspace" class="min-w-0 space-y-4">
        <div data-test="active-site-header" class="flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 bg-white px-4 py-3 dark:border-dark-600 dark:bg-dark-800">
          <div class="min-w-0 flex-1">
            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <h3 class="truncate text-base font-semibold" :title="active.base_url">{{ active.name }}</h3>
              <StatusIndicator :label="t('governance.' + (siteStateKeys[active.status] || 'unknown'))" :tone="active.status === 'healthy' ? 'success' : active.status === 'error' ? 'danger' : 'warning'" />
              <span class="text-xs text-gray-500">{{ active.platform === 'sub2api' ? 'Sub2API' : 'New API' }}</span>
            </div>
            <p class="mt-0.5 truncate text-xs text-gray-500">{{ active.base_url }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <button id="governance-collect" class="btn btn-primary text-sm" :disabled="working || !active.has_credential" @click="sync"><Icon name="refresh" size="sm" class="mr-2" :class="busy ? 'animate-spin' : ''" />{{ busy ? t('common.loading') : t('governance.sync') }}</button>
            <button id="governance-view-keys" data-test="view-keys" class="btn btn-secondary text-sm" :disabled="working" @click="openKeys()">{{ t('governance.viewKeys') }}</button>
            <div class="relative">
              <button data-test="site-more" type="button" class="btn btn-secondary text-sm" :aria-expanded="siteMenuOpen" :disabled="working" @click="siteMenuOpen = !siteMenuOpen"><Icon name="more" size="sm" /><span class="sr-only">{{ t('governance.siteHeader.more') }}</span></button>
              <div v-show="siteMenuOpen" data-test="site-more-menu" class="absolute right-0 z-20 mt-1 w-44 rounded-xl border border-gray-200 bg-white p-1 shadow-lg dark:border-dark-600 dark:bg-dark-800">
                <button data-test="site-edit" type="button" class="block w-full rounded-lg px-3 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-700" :disabled="working" @click="siteMenuOpen = false; editing = true">{{ t('common.edit') }}</button>
                <button id="governance-reconnect" type="button" class="block w-full rounded-lg px-3 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-700" :disabled="working" @click="siteMenuOpen = false; connecting = true">{{ t(active.has_credential ? 'governance.reconnect' : 'governance.connect') }}</button>
                <button data-test="site-delete" type="button" class="block w-full rounded-lg px-3 py-2 text-left text-sm text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20" :disabled="working" :aria-label="t('common.delete')" @click="siteMenuOpen = false; openDelete()">{{ t('common.delete') }}</button>
              </div>
            </div>
          </div>
        </div>
        <p v-if="showSiteError && active.status === 'reauth_required'" role="alert" data-test="site-error" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('governance.reauth') }}</p><p v-else-if="showSiteError && active.last_error" role="alert" data-test="site-error" class="text-sm text-red-600">{{ t(errorKey({ reason: active.last_error })) }}</p>
        <div class="min-w-0 overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
          <div v-show="tab === 'overview'" data-test="site-overview-tab" class="min-w-0 space-y-5 p-4 sm:p-5">
            <SiteOverview :site="active" :snapshot="overviewSnapshot" :binding-count="bindings.filter(binding => binding.account_id > 0).length" :balance-health="balanceHealth" :readiness="readiness" :readiness-loading="readinessLoading" :readiness-read-failed="readinessReadFailed" @navigate="navigateFromReadiness" @retry-readiness="reloadReadiness" />
            <ReconciliationPanel v-if="snapshot" :key="active.id" :site-id="active.id" :refresh-key="`${snapshot.id}:${reconciliationEpoch}`" :disabled="working" @busy="reconciliationBusy = $event" @applied="reconciled" />
            <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-900">{{ busy ? t('common.loading') : t('governance.noSnapshot') }}</p>
          </div>
          <div v-show="tab === 'groups'" data-test="site-groups-tab" class="min-w-0 space-y-4 p-4 sm:p-5">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="flex flex-wrap gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-900" role="group" data-test="groups-views">
                <button v-for="view in (['import', 'accounts'] as const)" :key="view" type="button" :data-test="'groups-view-' + view" class="min-h-9 rounded-md px-3 text-sm transition-colors" :class="groupsView === view ? 'bg-white font-medium text-gray-900 shadow-sm dark:bg-dark-600 dark:text-gray-100' : 'text-gray-500'" :aria-pressed="groupsView === view" @click="groupsView = view">{{ t(`governance.groupsTab.${view}`) }}<span v-if="view === 'accounts'" class="ml-1 tabular-nums text-gray-400">{{ importedAccountCount }}</span></button>
              </div>
              <div data-test="managed-keys-card" class="flex flex-wrap items-center gap-3 text-sm">
                <span class="text-gray-500">{{ t('governance.groupsTab.keyCount', { count: managedKeys.length }) }}</span>
                <span v-if="active.key_issue_count" class="font-medium text-red-600 dark:text-red-400">{{ t('governance.keyIssues', { count: active.key_issue_count }) }}</span>
                <button type="button" class="btn btn-secondary text-sm" :disabled="working" @click="openKeys()">{{ t('governance.groupsTab.manageKeys') }}</button>
              </div>
            </div>
            <div v-show="groupsView === 'import'" class="min-w-0"><ImportPanel v-if="snapshot && importStateReady" :key="active.id" :site-id="active.id" :site-base-url="active.base_url" :site-platform="active.platform" :bindings="bindings" :managed-keys="managedKeys" :preview-epoch="importPreviewEpoch" :snapshot="snapshot" :groups="groups" :disabled="busy || balanceBusy || automationBusy || reconciliationBusy || keyBusy || modelBusy" @busy="importBusy = $event" @applied="reloadBindings" @manage-keys="openKeys" @manage-templates="manageImportTemplates" /><p v-else class="py-8 text-center text-sm text-gray-500">{{ busy ? t('common.loading') : t(snapshot ? 'governance.importStateUnavailable' : 'governance.noSnapshot') }}</p></div>
            <GovernanceHistory v-if="groupsView === 'accounts'" :bindings="bindings" :groups="groups" :remote-groups="overviewSnapshot?.catalog.groups ?? snapshot?.catalog.groups ?? []" />
          </div>
          <div v-show="tab === 'automation'" data-test="site-automation-tab" class="min-w-0 space-y-5 p-4 sm:p-5">
            <ObservationPricingPanel :key="active.id" section="observation" :site-id="active.id" :disabled="working" @busy="observationBusy = $event" @observation-saved="reloadAfterPolicyChange" />
            <AutomationPolicyPanel :key="active.id" :site-id="active.id" :configuration="automation" :disabled="working" @busy="automationBusy = $event" @saved="automationSaved" @reload="reloadAutomation" />
            <BalanceCard :key="active.id" :site="active" :health="balanceHealth" :balance="overviewSnapshot?.catalog.account?.balance" :unit="snapshot?.catalog.account?.unit" :disabled="working" @reload="reloadHealth" @saved="balanceSaved" @busy="balanceBusy = $event" />
            <!-- Group pricing is a local-group sale price rule; prices themselves are in the group import details. -->
            <ObservationPricingPanel :key="active.id" section="pricing" :site-id="active.id" :disabled="working" @busy="pricingBusy = $event" @pricing-saved="reloadAfterPolicyChange" />
          </div>
          <div v-if="tab === 'models' && !showOverview" class="min-w-0 p-4 sm:p-5"><ModelMonitorPanel :key="active.id" :site-id="active.id" :remote-groups="overviewSnapshot?.catalog.groups ?? snapshot?.catalog.groups ?? []" :managed-keys="managedKeys" :collected-at="overviewSnapshot?.created_at ?? snapshot?.created_at" :disabled="busy || importBusy || balanceBusy || editBusy || automationBusy || reconciliationBusy || keyBusy" @busy="modelBusy = $event" @manage-keys="openKeys()" /></div>
          <div v-show="tab === 'history'" class="min-w-0 space-y-5 p-4 sm:p-5">
            <GovernanceTimeline v-if="tab === 'history' && !showOverview" :site-id="active.id" :remote-groups="overviewSnapshot?.catalog.groups ?? snapshot?.catalog.groups ?? []" :disabled="navigationLocked" />
          </div>
        </div>
      </section>
      </div>
      </div>
      </div>
      <BaseDialog v-if="keysOpen && active" :show="true" :title="t('governance.groupKeys')" width="wide" :show-close-button="!keyBusy" :close-on-escape="!keyBusy" @close="closeKeys"><ManagedKeysPanel :key="active.id" :site-id="active.id" :site-version="active.version" :snapshot-id="snapshot?.id || 0" :groups="snapshot?.catalog.groups || []" :selections="keySelections" :disabled="busy || importBusy || balanceBusy || editBusy || automationBusy || reconciliationBusy || modelBusy" @busy="keyBusy = $event" @repaired="keyRepaired" /></BaseDialog>
      <OnboardDialog
        v-if="onboarding"
        :proxies="proxies"
        @close="onboarding = false"
        @created="siteCreated"
        @completed="onboarded"
      />
      <ConnectDialog
        v-if="connecting && active"
        :site-id="active.id"
        :site-version="active.version"
        :site-url="active.base_url"
        :platform="active.platform"
        @close="connecting = false"
        @connected="connected"
      />
      <SiteEditDialog
        v-if="editing && active"
        :key="active.id"
        :site="active"
        :proxies="proxies"
        @close="editing = false"
        @saved="edited"
        @busy="editBusy = $event"
      />
      <BaseDialog
        :show="deleting"
        :title="t('common.delete')"
        :show-close-button="!busy"
        :close-on-escape="!busy"
        @close="deleting = false"
        ><p>{{ t('governance.deleteNotice') }}</p>
        <label v-if="importedAccountCount || managedKeys.length" class="governance-checkbox-label mt-4 flex items-start gap-2 text-sm"
          ><input v-model="deleteKeepAccounts" data-test="delete-keep-accounts" type="checkbox" class="governance-checkbox mt-0.5" /><span
            >{{ t('governance.deleteDialog.keepAccounts', { count: importedAccountCount }) }}<span class="mt-1 block text-xs text-gray-500">{{ t('governance.deleteDialog.keepAccountsHint') }}</span></span
          ></label
        >
        <p v-if="deleteError" role="alert" data-test="delete-error" class="mt-3 text-red-600">{{ deleteError }}</p>
        <button data-test="confirm-delete-site" class="btn btn-danger mt-4" :disabled="busy" @click="remove">
          {{ t(deleteKeepAccounts ? 'governance.deleteDialog.confirmKeep' : 'common.delete') }}
        </button></BaseDialog
      >
    </div></AppLayout
  >
</template>
