<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import api, { type LoginCredentials, type Site, type SiteInput } from '@/api/admin/upstream-governance'
import { errorKey } from './feedback'
import { intervalValidationKey } from './interval'

const props = defineProps<{ site: Site; proxies: { id: number; name: string }[] }>()
const emit = defineEmits<{ close: []; saved: [site: Site]; busy: [value: boolean] }>()
const { t } = useI18n()
const source = ref(props.site)
function siteInput(site: Site): SiteInput {
  return { name: site.name, platform: site.platform, base_url: site.base_url, proxy_id: site.proxy_id, enabled: site.enabled, interval_minutes: site.interval_minutes }
}
const form = ref(siteInput(props.site))
const login = ref<LoginCredentials>({ username: '', password: '' })
const savedLogin = ref<LoginCredentials>({ username: '', password: '' })
const loading = ref(true), saving = ref(false), ready = ref(false), error = ref('')
let generation = 0
const locked = computed(() => loading.value || saving.value || !ready.value)
const hasSavedLogin = computed(() => !!savedLogin.value.username && !!savedLogin.value.password)
const normalizedSiteURL = (value: string) => value.trim().replace(/\/$/, '')
const originChanged = computed(() => normalizedSiteURL(form.value.base_url) !== normalizedSiteURL(source.value.base_url) || form.value.platform !== source.value.platform)
function clearPreviousLogin() {
  if (ready.value && originChanged.value && login.value.username === savedLogin.value.username && login.value.password === savedLogin.value.password) {
    login.value = { username: '', password: '' }
  }
}
watch(() => form.value.platform, clearPreviousLogin)
const platformOptions = [{ value: 'sub2api', label: 'Sub2API' }, { value: 'newapi', label: 'New API' }]
const proxyOptions = computed(() => {
  const options: { value: number | null; label: string; disabled?: boolean }[] = [
    { value: null, label: t('governance.direct') },
    ...props.proxies.map(proxy => ({ value: proxy.id, label: proxy.name })),
  ]
  if (form.value.proxy_id != null && !props.proxies.some(proxy => proxy.id === form.value.proxy_id)) {
    options.push({ value: form.value.proxy_id, label: t('governance.unavailableProxy', { id: form.value.proxy_id }), disabled: true })
  }
  return options
})
async function reload() {
  if (saving.value) return
  const request = ++generation
  loading.value = true
  ready.value = false
  error.value = ''
  emit('busy', true)
  try {
    const [sites, credentials] = await Promise.all([api.list(), api.loginCredentials(props.site.id)])
    if (request !== generation) return
    const site = sites.find(item => item.id === props.site.id)
    if (!site) throw { reason: 'not_found' }
    if (site.version !== credentials.version) throw { reason: 'stale_preview' }
    source.value = site
    form.value = siteInput(site)
    savedLogin.value = { username: credentials.username, password: credentials.password }
    login.value = { ...savedLogin.value }
    ready.value = true
  } catch (e) {
    if (request === generation) error.value = editError(e)
  } finally {
    if (request === generation) { loading.value = false; emit('busy', false) }
  }
}
function editError(e: unknown) {
  return t((e as { reason?: string }).reason === 'stale_preview' ? 'governance.siteEditStale' : errorKey(e))
}
function clearLogin() {
  login.value = { username: '', password: '' }
  savedLogin.value = { username: '', password: '' }
}
function close() {
  if (saving.value) return
  generation++
  clearLogin()
  emit('busy', false)
  emit('close')
}
async function save() {
  if (locked.value) return
  const intervalError = intervalValidationKey(form.value.interval_minutes)
  if (intervalError) { error.value = t(intervalError); return }
  const username = login.value.username.trim()
  if (!!username !== !!login.value.password) { error.value = t('governance.loginPairRequired'); return }
  const request = generation
  saving.value = true
  error.value = ''
  emit('busy', true)
  try {
    const changed = username !== savedLogin.value.username || login.value.password !== savedLogin.value.password
    const site = await api.update(source.value.id, {
      ...form.value, version: source.value.version,
      ...(changed ? { login_credentials: { username, password: login.value.password } } : {}),
    })
    if (request !== generation) return
    clearLogin()
    emit('saved', site)
  } catch (e) {
    if (request === generation) error.value = editError(e)
  } finally {
    if (request === generation) { saving.value = false; emit('busy', false) }
  }
}
onMounted(reload)
onUnmounted(() => { generation++; clearLogin(); emit('busy', false) })
</script>

<template>
  <BaseDialog :show="true" :title="t('governance.edit')" :close-on-escape="!saving" :show-close-button="!saving" @close="close">
    <form id="governance-edit-form" class="space-y-5" @submit.prevent="save">
      <div v-if="loading" role="status" class="flex items-center gap-2 rounded-xl bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-900">
        <Icon name="refresh" size="sm" class="animate-spin" />{{ t('governance.loadingSiteDetails') }}
      </div>
      <div v-if="error" role="alert" class="space-y-2 rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/10 dark:text-red-300">
        <p>{{ error }}</p>
        <button id="governance-edit-reload" type="button" class="font-medium underline" :disabled="loading || saving" @click="reload">{{ t('governance.reloadSiteDetails') }}</button>
      </div>
      <fieldset :disabled="locked" class="min-w-0 space-y-5">
        <section class="space-y-3">
          <h4 class="flex items-center gap-2 text-sm font-semibold"><Icon name="server" size="sm" class="text-primary-600" />{{ t('governance.siteDetails') }}</h4>
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <label for="governance-edit-name" class="mb-1.5 block text-sm text-gray-600 dark:text-gray-300">{{ t('governance.name') }}</label>
              <input id="governance-edit-name" v-model="form.name" class="input w-full" required maxlength="100" />
            </div>
            <div>
              <label for="governance-edit-platform" class="mb-1.5 block text-sm text-gray-600 dark:text-gray-300">{{ t('governance.platform') }}</label>
              <Select id="governance-edit-platform" v-model="form.platform" :options="platformOptions" :disabled="locked" :searchable="false" :aria-label="t('governance.platform')">
                <template #selected="{ option }"><span class="flex items-center gap-2"><Icon name="server" size="sm" class="shrink-0 text-primary-600" />{{ option?.label }}</span></template>
                <template #option="{ option }"><span class="flex items-center gap-2"><Icon name="server" size="sm" class="shrink-0 text-primary-600" />{{ option.label }}</span></template>
              </Select>
            </div>
          </div>
          <div>
            <label for="governance-edit-url" class="mb-1.5 block text-sm text-gray-600 dark:text-gray-300">{{ t('governance.url') }}</label>
            <input id="governance-edit-url" v-model="form.base_url" class="input w-full" type="url" placeholder="https://upstream.example" required @change="clearPreviousLogin" />
          </div>
        </section>
        <section class="space-y-3 rounded-xl border border-primary-100 bg-primary-50/40 p-4 dark:border-primary-900/40 dark:bg-primary-900/10">
          <h4 class="flex items-center gap-2 text-sm font-semibold"><Icon name="key" size="sm" class="text-primary-600" />{{ t('governance.loginDetails') }}</h4>
          <p v-if="ready && !hasSavedLogin" class="text-xs text-gray-500">{{ t('governance.loginDetailsMissing') }}</p>
          <p v-if="originChanged" class="text-xs text-gray-500">{{ t('governance.originLoginReset') }}</p>
          <div>
            <label for="governance-edit-username" class="mb-1.5 block text-sm text-gray-600 dark:text-gray-300">{{ t('governance.username') }}</label>
            <input id="governance-edit-username" v-model="login.username" class="input w-full" type="text" autocomplete="off" spellcheck="false" maxlength="320" />
          </div>
          <div>
            <label for="governance-edit-password" class="mb-1.5 block text-sm text-gray-600 dark:text-gray-300">{{ t('governance.password') }}</label>
            <input id="governance-edit-password" v-model="login.password" class="input w-full font-mono" type="text" autocomplete="off" spellcheck="false" maxlength="4096" />
          </div>
          <p class="text-xs leading-relaxed text-gray-500">{{ t('governance.loginDetailsHint') }}</p>
        </section>
        <section class="space-y-3">
          <h4 class="flex items-center gap-2 text-sm font-semibold"><Icon name="clock" size="sm" class="text-primary-600" />{{ t('governance.collectionSettings') }}</h4>
          <div>
            <label for="governance-edit-proxy" class="mb-1.5 block text-sm text-gray-600 dark:text-gray-300">{{ t('governance.proxy') }}</label>
            <Select id="governance-edit-proxy" v-model="form.proxy_id" :options="proxyOptions" :disabled="locked" :aria-label="t('governance.proxy')">
              <template #selected="{ option }"><span class="flex min-w-0 items-center gap-2"><Icon name="globe" size="sm" class="shrink-0 text-gray-400" /><span class="truncate">{{ option?.label }}</span></span></template>
              <template #option="{ option }"><span class="flex min-w-0 items-center gap-2"><Icon name="globe" size="sm" class="shrink-0 text-gray-400" /><span class="truncate">{{ option.label }}</span></span></template>
            </Select>
          </div>
          <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-gray-50 p-3 dark:bg-dark-900">
            <label class="flex cursor-pointer items-center gap-2 text-sm"><input id="governance-edit-enabled" v-model="form.enabled" type="checkbox" />{{ t('governance.autoOn') }}</label>
            <div class="flex items-center gap-2">
              <label for="governance-edit-interval" class="text-xs text-gray-500">{{ t('governance.interval') }}</label>
              <input id="governance-edit-interval" v-model.number="form.interval_minutes" class="input w-24 text-sm" type="number" min="1" step="1" required />
            </div>
          </div>
        </section>
      </fieldset>
    </form>
    <template #footer>
      <div class="flex items-center justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="saving" @click="close">{{ t('common.cancel') }}</button>
        <button id="governance-edit-save" type="submit" form="governance-edit-form" class="btn btn-primary" :disabled="locked">{{ saving ? t('common.processing') : t('common.save') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>
