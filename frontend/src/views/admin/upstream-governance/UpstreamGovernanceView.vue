<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { SMART_OPERATIONS_SECTIONS, resolveSmartOperationsSection, type SmartOperationsSection } from '@/config/smartOperations'
import SmartOperationsTabs from './SmartOperationsTabs.vue'
import { errorKey, siteStateKeys } from './feedback'
import { intervalValidationKey } from './interval'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImportPanel from './ImportPanel.vue'
import ConnectDialog from './ConnectDialog.vue'
import OnboardDialog from './OnboardDialog.vue'
import SiteEditDialog from './SiteEditDialog.vue'
import SiteOverview from './SiteOverview.vue'
import BalanceMonitorPanel from './BalanceMonitorPanel.vue'
import GovernanceHistory from './GovernanceHistory.vue'
import GovernanceSitesOverview from './GovernanceSitesOverview.vue'
import AutomationPolicyPanel from './AutomationPolicyPanel.vue'
import ReconciliationPanel from './ReconciliationPanel.vue'
import BalanceHealthPanel from './BalanceHealthPanel.vue'
import RechargePlanPanel from './RechargePlanPanel.vue'
import ManagedKeysPanel from './ManagedKeysPanel.vue'
import ModelMonitorPanel from './ModelMonitorPanel.vue'
import ObservationPricingPanel from './ObservationPricingPanel.vue'
import Icon from '@/components/icons/Icon.vue'
import api, {
  type Site,
  type Snapshot,
  type Binding,
  type Page,
  type GovernanceEvent,
  type Check,
  type ManagedKey,
  type KeySelection,
  type AutomationConfiguration,
  type BalanceHealth,
} from '@/api/admin/upstream-governance'
import groupsAPI from '@/api/admin/groups'
import proxiesAPI from '@/api/admin/proxies'
import type { AdminGroup } from '@/types'
const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const tab = computed({
  get: () => resolveSmartOperationsSection(route.params.section),
  set: (section: SmartOperationsSection) => {
    if (navigationLocked.value) return
    const entry = SMART_OPERATIONS_SECTIONS.find(item => item.id === section)!
    void router.push({ path: entry.path, query: route.query })
  },
})
const showOverview = ref(true)
const siteNotFound = ref(false)
let sitesLoaded = false
let siteRefreshTimer: ReturnType<typeof setInterval> | undefined
let summaryRefreshing = false
const automation = ref<AutomationConfiguration | null>(null), balanceHealth = ref<BalanceHealth | null>(null)
const automationBusy = ref(false), observationBusy = ref(false), reconciliationBusy = ref(false), keyBusy = ref(false), rechargeBusy = ref(false), modelBusy = ref(false)
const keysOpen = ref(false), keySelections = ref<KeySelection[]>([]), reconciliationEpoch = ref(0)
const importPreviewEpoch = ref(0)
const importBusy = ref(false)
const balanceBusy = ref(false)
const editBusy = ref(false)
let generation = 0
let healthGeneration = 0
const sites = ref<Site[]>([]),
  active = ref<Site | null>(null),
  snapshot = ref<Snapshot | null>(null),
  overviewSnapshot = ref<Snapshot | null>(null),
  bindings = ref<Binding[]>([])
const managedKeys = ref<ManagedKey[]>([]), importStateReady = ref(false)
const groups = ref<AdminGroup[]>([]),
  proxies = ref<{ id: number; name: string }[]>([])
const events = ref<Page<GovernanceEvent> | null>(null),
  checks = ref<Page<Check> | null>(null)
const busy = ref(false),
  actionBusy = ref(false),
  error = ref(''),
  connecting = ref(false),
  onboarding = ref(false),
  editing = ref(false),
  deleting = ref(false)
const probe = ref<Binding | null>(null),
  probeAction = ref<'check' | 'monitor'>('check')
const mutationBusy = computed(() => actionBusy.value || importBusy.value || balanceBusy.value || editBusy.value || automationBusy.value || observationBusy.value || reconciliationBusy.value || keyBusy.value || rechargeBusy.value || modelBusy.value)
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
  if (!showOverview.value && active.value?.id === site.id) {
    // Keep successful loads and unsaved drafts. A failed read can be retried explicitly.
    if (error.value && !working.value) void select(site)
    return
  }
  // Commit the route first so a rejected navigation cannot change the active account.
  void router.push({ path: route.path, query: { ...route.query, site: String(site.id) } })
}
function restoreLocation() {
  if (!sitesLoaded) return
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
  // Do not leave an old site's credentials or action dialog over a new route.
  const refreshKeys = keysOpen.value
  const previousSite = active.value?.id
  keysOpen.value = false
  keySelections.value = []
  editing.value = false
  deleting.value = false
  probe.value = null
  restoreLocation()
  // Closing with browser navigation must reconcile newly created keys just like
  // the dialog's close button, including when the site is cached behind the picker.
  if (refreshKeys && previousSite === active.value?.id) void reloadBindings()
})
function failure(e: unknown) {
  error.value = t(errorKey(e))
}
async function run(action: () => Promise<void>, writes = false) {
  const request = generation
  busy.value = true
  if (writes) actionBusy.value = true
  error.value = ''
  try {
    await action()
  } catch (e) {
    if (request === generation) failure(e)
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
  probe.value = null
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
  events.value = null
  checks.value = null
  bindings.value = []
  managedKeys.value = []
  importStateReady.value = false
  busy.value = true
  error.value = ''
  const results = await Promise.allSettled([
    collected ? Promise.resolve(collected) : api.catalog(site.id),
    api.bindings(site.id),
    api.events(site.id),
    api.checks(site.id),
    api.keys(site.id),
    api.automation(site.id),
    api.balanceHealth(site.id),
  ])
  if (request === generation && active.value?.id === site.id) {
    if (results[0].status === 'fulfilled') snapshot.value = overviewSnapshot.value = results[0].value
    else if ((results[0].reason as { status?: number }).status !== 404)
      failure(results[0].reason)
    if (results[1].status === 'fulfilled') bindings.value = results[1].value
    else failure(results[1].reason)
    if (results[2].status === 'fulfilled') events.value = results[2].value
    else failure(results[2].reason)
    if (results[3].status === 'fulfilled') checks.value = results[3].value
    else failure(results[3].reason)
    if (results[4].status === 'fulfilled') managedKeys.value = results[4].value
    else failure(results[4].reason)
    if (results[5].status === 'fulfilled') automation.value = results[5].value
    if (results[6].status === 'fulfilled') balanceHealth.value = results[6].value
    importStateReady.value = results[1].status === 'fulfilled' && results[4].status === 'fulfilled'
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
  await run(async () => {
    try {
      const collected = await api.sync(id)
      const [list, history, currentBindings, currentKeys] = await Promise.all([api.list(), api.events(id), api.bindings(id), api.keys(id)])
      if (request !== generation) return
      snapshot.value = collected
      overviewSnapshot.value = collected
      sites.value = list
      active.value = list.find((s) => s.id === id) || null
      events.value = history
      bindings.value = currentBindings
      managedKeys.value = currentKeys
      importStateReady.value = true
      await reloadHealth()
    } catch (e) {
      if (request === generation) importStateReady.value = false
      throw e
    }
  }, true)
}
async function remove() {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  let removed = false
  let nextSite: Site | undefined
  await run(async () => {
    await api.remove(id)
    const list = await api.list()
    if (request !== generation) return
    active.value = null
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
  }, true)
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
}
async function reloadBindings() {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  try {
    const [value, keys] = await Promise.all([api.bindings(id), api.keys(id)])
    if (request === generation) {
      bindings.value = value
      managedKeys.value = keys
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
async function page(kind: 'events' | 'checks', n: number) {
  if (!active.value) return
  const id = active.value.id,
    request = generation
  await run(async () => {
    if (kind === 'events') {
      const value = await api.events(id, n)
      if (request === generation) events.value = value
    } else {
      const value = await api.checks(id, n)
      if (request === generation) checks.value = value
    }
  })
}
async function acknowledge(id: number) {
  if (!active.value) return
  const siteId = active.value.id,
    request = generation,
    eventPage = events.value?.page
  await run(async () => {
    await api.acknowledge(siteId, id)
    const value = await api.events(siteId, eventPage)
    if (request === generation) events.value = value
  }, true)
}
function configure(binding: Binding, action: 'check' | 'monitor') {
  if (binding.account_id <= 0 || binding.account_deleted) return
  probe.value = {
    ...binding,
    probe_interval_minutes: binding.probe_interval_minutes || 30,
  }
  probeAction.value = action
}
async function submitProbe() {
  if (!probe.value || !active.value) return
  if (probeAction.value === 'monitor') {
    const intervalError = intervalValidationKey(probe.value.probe_interval_minutes)
    if (intervalError) { error.value = t(intervalError); return }
  }
  const id = active.value.id,
    request = generation,
    b = { ...probe.value },
    action = probeAction.value
  await run(async () => {
    if (action === 'check') {
      await api.check(id, b.id, b.probe_model)
      const value = await api.checks(id)
      if (request === generation) checks.value = value
    } else {
      await api.monitor(id, b.id, {
        enabled: b.probe_enabled,
        model: b.probe_model,
        interval_minutes: b.probe_interval_minutes,
      })
      const value = await api.bindings(id)
      if (request === generation) bindings.value = value
    }
    if (request === generation) probe.value = null
  }, true)
}
function openKeys(selections: KeySelection[] = []) {
  if (working.value || !active.value) return
  keySelections.value = [...selections]
  keysOpen.value = true
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
async function reloadAutomation() {
  if (!active.value) return
  const id = active.value.id, request = generation
  try { const value = await api.automation(id); if (request === generation) automation.value = value }
  catch (e) { if (request === generation) failure(e) }
}
function automationSaved(value: AutomationConfiguration) {
  automation.value = value
  reconciliationEpoch.value++
}
async function reconciled() {
  if (!active.value) return
  const id = active.value.id, request = generation
  await reloadBindings()
  await reloadHealth()
  try { const value = await api.events(id); if (request === generation) events.value = value }
  catch (e) { if (request === generation) failure(e) }
}
onMounted(async () => {
  await load()
  await run(async () => {
    const [g, p] = await Promise.all([groupsAPI.getAll(), proxiesAPI.getAll()])
    groups.value = g
    proxies.value = p.map(({ id, name }) => ({ id, name }))
  })
  sitesLoaded = true
  restoreLocation()
  siteRefreshTimer = setInterval(() => { void refreshSiteSummaries() }, 60_000)
})
onUnmounted(() => {
  generation++
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
          <p class="mt-1.5 max-w-3xl text-sm leading-relaxed text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.workspaceHint') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button class="btn btn-secondary" :disabled="working || navigationLocked" @click="load"><Icon name="refresh" size="sm" class="mr-2" />{{ t('common.refresh') }}</button>
          <button id="governance-add-site" class="btn btn-primary" :disabled="working || navigationLocked" @click="onboarding = true"><Icon name="plus" size="sm" class="mr-2" />{{ t('governance.add') }}</button>
        </div>
      </header>
      <div class="grid min-w-0 items-start gap-5 xl:grid-cols-[17.5rem_minmax(0,1fr)]" data-test="smart-operations-workspace">
      <aside class="min-w-0 xl:sticky xl:top-24" data-test="smart-operations-site-rail" :aria-label="t('governance.upstreamSites')">
        <GovernanceSitesOverview compact :sites="sites" :selected-site-id="showOverview ? null : active?.id" :disabled="navigationLocked" @select="chooseSite" />
      </aside>
      <div class="min-w-0 space-y-4" data-test="smart-operations-content">
      <SmartOperationsTabs v-model="tab" :disabled="navigationLocked" />
      <div :id="`governance-${tab}-panel`" role="tabpanel" :aria-labelledby="`governance-${tab}-tab`" tabindex="0" class="min-w-0 space-y-5 rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500">
      <p v-if="error" role="alert" class="rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-600 dark:border-red-900 dark:bg-red-900/10">{{ error }}</p>
      <p v-if="siteNotFound" role="status" class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-900 dark:bg-amber-900/20 dark:text-amber-300">{{ t('governance.smartOperations.siteNotFound') }}</p>
      <div v-if="showOverview && sites.length" data-test="site-selection-empty" class="flex min-h-64 flex-col items-center justify-center gap-4 rounded-xl border border-dashed border-gray-200 bg-white p-8 text-center dark:border-dark-600 dark:bg-dark-800">
        <span class="rounded-2xl bg-gray-100 p-4 dark:bg-dark-700"><Icon name="server" size="lg" class="text-gray-500" /></span>
        <div><h3 class="text-base font-semibold">{{ t('governance.smartOperations.chooseSite') }}</h3><p class="mt-2 max-w-sm text-sm leading-relaxed text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.chooseSiteHint') }}</p></div>
      </div>
      <p v-if="busy && !active" role="status" class="p-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
      <p v-if="!busy && !sites.length" class="rounded-xl border border-dashed border-gray-200 p-8 text-center text-sm text-gray-500 dark:border-dark-600">{{ t('governance.empty') }}</p>
      <section v-if="active" v-show="!showOverview" data-test="active-site-workspace" class="min-w-0 space-y-4">
        <div class="overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
          <div class="flex flex-wrap items-center gap-3 p-4 sm:p-5">
            <span class="hidden rounded-xl bg-gray-100 p-3 text-gray-600 dark:bg-dark-700 dark:text-dark-200 sm:block"><Icon name="server" size="md" /></span>
            <div class="min-w-0 flex-1"><p class="mb-1 text-xs text-gray-500 dark:text-dark-300">{{ t('governance.smartOperations.activeSite') }}</p><h3 class="break-words text-lg font-semibold">{{ active.name }}</h3><p class="mt-1 break-all text-xs text-gray-500 dark:text-dark-300">{{ active.base_url }} · {{ active.platform }}</p></div>
            <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs dark:bg-dark-700">{{ t('governance.' + (siteStateKeys[active.status] || 'unknown')) }}</span>
          </div>
          <div class="flex flex-wrap gap-2 border-t border-gray-100 bg-gray-50/60 px-4 py-3 dark:border-dark-700 dark:bg-dark-900/30 sm:px-5"><button id="governance-collect" class="btn btn-primary text-sm" :disabled="working || !active.has_credential" @click="sync"><Icon name="refresh" size="sm" class="mr-2" :class="busy ? 'animate-spin' : ''" />{{ busy ? t('common.loading') : t('governance.sync') }}</button><button id="governance-view-keys" data-test="view-keys" class="btn btn-secondary text-sm" :disabled="working" @click="openKeys()">{{ t('governance.viewKeys') }}</button><button class="btn btn-secondary text-sm" :disabled="working" @click="editing = true">{{ t('common.edit') }}</button><button id="governance-reconnect" class="btn btn-secondary text-sm" :disabled="working" @click="connecting = true">{{ t(active.has_credential ? 'governance.reconnect' : 'governance.connect') }}</button><button class="ml-auto flex min-h-11 min-w-11 items-center justify-center rounded-lg text-gray-500 hover:bg-red-50 hover:text-red-600 focus-visible:ring-2 focus-visible:ring-primary-500 disabled:opacity-50 dark:hover:bg-red-900/20" :disabled="working" :aria-label="t('common.delete')" @click="deleting = true"><Icon name="trash" size="sm" /></button></div>
        </div>
        <p v-if="active.status === 'reauth_required'" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('governance.reauth') }}</p><p v-else-if="active.last_error" role="alert" class="text-sm text-red-600">{{ t(errorKey({ reason: active.last_error })) }}</p>
        <div class="min-w-0 overflow-hidden rounded-xl border border-gray-200 bg-white dark:border-dark-600 dark:bg-dark-800">
          <div v-show="tab === 'overview'" data-test="site-overview-tab" class="min-w-0 space-y-5 p-4 sm:p-5">
            <SiteOverview :site="active" :snapshot="overviewSnapshot" :binding-count="bindings.filter(binding => binding.account_id > 0).length" :balance-health="balanceHealth" />
            <div class="flex flex-wrap items-center gap-3 rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900"><span class="font-medium">{{ t(!automation ? 'governance.automationUnknown' : automation.policy.enabled ? 'governance.automationOn' : 'governance.automationOff') }}</span><span class="min-w-0 flex-1 text-xs text-gray-500">{{ t('governance.automationSummaryHint') }}</span><button type="button" class="text-sm font-medium text-primary-700 dark:text-primary-300" @click="tab = 'monitor'">{{ t('governance.configureAutomation') }}</button></div>
            <ReconciliationPanel v-if="snapshot" :key="active.id" :site-id="active.id" :refresh-key="`${snapshot.id}:${reconciliationEpoch}`" :disabled="working" @busy="reconciliationBusy = $event" @applied="reconciled" />
            <p v-else class="rounded-lg bg-gray-50 p-4 text-sm text-gray-500 dark:bg-dark-900">{{ busy ? t('common.loading') : t('governance.noSnapshot') }}</p>
            <BalanceHealthPanel :health="balanceHealth" :disabled="working" @reload="reloadHealth" @configure="tab = 'monitor'" />
          </div>
          <div v-show="tab === 'import'" class="min-w-0 p-4 sm:p-5"><ImportPanel v-if="snapshot && importStateReady" :key="active.id" :site-id="active.id" :site-base-url="active.base_url" :site-platform="active.platform" :bindings="bindings" :managed-keys="managedKeys" :preview-epoch="importPreviewEpoch" :snapshot="snapshot" :groups="groups" :disabled="busy || balanceBusy || automationBusy || reconciliationBusy || keyBusy || rechargeBusy || modelBusy" @busy="importBusy = $event" @applied="reloadBindings" @manage-keys="openKeys" /><p v-else class="py-8 text-center text-sm text-gray-500">{{ busy ? t('common.loading') : t(snapshot ? 'governance.importStateUnavailable' : 'governance.noSnapshot') }}</p></div>
          <div v-show="tab === 'monitor'" class="min-w-0 space-y-5 p-4 sm:p-5"><ObservationPricingPanel :key="active.id" :site-id="active.id" :disabled="working" @busy="observationBusy = $event" @observation-saved="reloadHealth" @pricing-saved="reloadHealth" /><AutomationPolicyPanel :key="active.id" :site-id="active.id" :configuration="automation" :disabled="working" @busy="automationBusy = $event" @saved="automationSaved" @reload="reloadAutomation" /><BalanceMonitorPanel :key="active.id" :site="active" :unit="snapshot?.catalog.account?.unit" :disabled="working" @saved="balanceSaved" @busy="balanceBusy = $event" /><RechargePlanPanel :key="active.id" :site-id="active.id" :disabled="working" @busy="rechargeBusy = $event" /><GovernanceHistory mode="bindings" :bindings="bindings" :groups="groups" :remote-groups="overviewSnapshot?.catalog.groups ?? snapshot?.catalog.groups ?? []" :events="null" :checks="null" :disabled="working" @configure="configure" /></div>
          <div v-if="tab === 'models' && !showOverview" class="min-w-0 p-4 sm:p-5"><ModelMonitorPanel :key="active.id" :site-id="active.id" :remote-groups="overviewSnapshot?.catalog.groups ?? snapshot?.catalog.groups ?? []" :managed-keys="managedKeys" :collected-at="overviewSnapshot?.created_at ?? snapshot?.created_at" :disabled="busy || importBusy || balanceBusy || editBusy || automationBusy || reconciliationBusy || keyBusy || rechargeBusy" @busy="modelBusy = $event" @manage-keys="openKeys()" /></div>
          <div v-show="tab === 'history'" class="min-w-0 p-4 sm:p-5"><GovernanceHistory mode="history" :bindings="[]" :events="events" :checks="checks" :disabled="working" @acknowledge="acknowledge" @page="page" /></div>
        </div>
      </section>
      </div>
      </div>
      </div>
      <BaseDialog v-if="keysOpen && active" :show="true" :title="t('governance.groupKeys')" width="wide" :show-close-button="!keyBusy" :close-on-escape="!keyBusy" @close="closeKeys"><ManagedKeysPanel :key="active.id" :site-id="active.id" :site-version="active.version" :snapshot-id="snapshot?.id || 0" :groups="snapshot?.catalog.groups || []" :selections="keySelections" :disabled="busy || importBusy || balanceBusy || editBusy || automationBusy || reconciliationBusy || rechargeBusy || modelBusy" @busy="keyBusy = $event" @repaired="keyRepaired" /></BaseDialog>
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
        <p v-if="error" class="text-red-600">{{ error }}</p>
        <button class="btn btn-danger mt-4" :disabled="busy" @click="remove">
          {{ t('common.delete') }}
        </button></BaseDialog
      >
      <BaseDialog
        :show="!!probe"
        :title="
          t(probeAction === 'check' ? 'governance.check' : 'governance.monitor')
        "
        :show-close-button="!busy"
        :close-on-escape="!busy"
        @close="probe = null"
        ><form v-if="probe" class="space-y-4" @submit.prevent="submitProbe">
          <p class="text-amber-700">{{ t('governance.billable') }}</p>
          <p v-if="error" class="text-red-600">{{ error }}</p>
          <label class="block"
            >{{ t('governance.model')
            }}<input
              v-model="probe.probe_model"
              class="input w-full"
              :required="
                probeAction === 'check' || probe.probe_enabled
              " /></label
          ><template v-if="probeAction === 'monitor'"
            ><label class="governance-checkbox-label flex items-center gap-2"
              ><input v-model="probe.probe_enabled" type="checkbox" class="governance-checkbox" />{{
                t('governance.enableMonitor')
              }}</label
            ><label class="block"
              >{{ t('governance.interval')
              }}<input
                v-model.number="probe.probe_interval_minutes"
                data-test="probe-interval"
                class="input w-full"
                type="number"
                min="1"
                step="1"
                required /></label></template
          ><button class="btn btn-primary" :disabled="busy">
            {{
              t(
                probeAction === 'check'
                  ? 'governance.confirmCheck'
                  : 'common.save',
              )
            }}
          </button>
        </form></BaseDialog
      >
    </div></AppLayout
  >
</template>
