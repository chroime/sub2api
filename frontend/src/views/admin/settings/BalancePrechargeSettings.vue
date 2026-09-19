<template>
  <section
    id="balance-precharge-settings"
    class="scroll-mt-40 space-y-4 p-6"
    aria-labelledby="balance-precharge-title"
    :aria-busy="loading || saving"
  >
    <div class="flex items-center justify-between gap-4">
      <div>
        <h4 id="balance-precharge-title" class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.settings.balancePrecharge.title') }}
        </h4>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.balancePrecharge.description') }}</p>
      </div>
      <Toggle
        v-model="enabled"
        :disabled="busy"
        :aria-label="t('admin.settings.balancePrecharge.title')"
        class="disabled:cursor-not-allowed disabled:opacity-50"
      />
    </div>
    <div class="grid gap-4 sm:grid-cols-2">
      <div>
        <label class="input-label" for="precharge-threshold">{{ t('admin.settings.balancePrecharge.threshold') }}</label>
        <input id="precharge-threshold" v-model="threshold" type="text" inputmode="decimal" class="input" :disabled="busy" data-testid="precharge-threshold" @keydown.enter.prevent="save" />
      </div>
      <div>
        <label class="input-label" for="precharge-amount">{{ t('admin.settings.balancePrecharge.amount') }}</label>
        <input id="precharge-amount" v-model="amount" type="text" inputmode="decimal" class="input" :disabled="busy" data-testid="precharge-amount" @keydown.enter.prevent="save" />
      </div>
    </div>
    <div class="space-y-1 text-xs text-gray-500 dark:text-gray-400">
      <p>{{ t('admin.settings.balancePrecharge.behavior') }}</p>
      <p>{{ t('admin.settings.balancePrecharge.overdraft') }}</p>
      <p>{{ t('admin.settings.balancePrecharge.scope') }}</p>
    </div>
    <p v-if="loaded && !valid" data-testid="precharge-validation" class="text-xs text-red-600 dark:text-red-400">
      {{ t('admin.settings.balancePrecharge.invalidAmounts') }}
    </p>
    <div class="flex flex-wrap items-center gap-3">
      <button type="button" class="btn btn-primary" :disabled="busy || !valid" data-testid="precharge-save" @click="save">
        {{ t(saving ? 'admin.settings.saving' : 'common.save') }}
      </button>
      <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(error) }}</p>
      <p v-else-if="saved" role="status" class="text-xs text-green-600 dark:text-green-400">{{ t('admin.settings.balancePrecharge.saved') }}</p>
      <p v-else-if="loading" role="status" class="text-xs text-gray-500">{{ t('common.loading') }}</p>
      <button v-if="error" type="button" class="btn btn-secondary btn-sm" :disabled="loading || saving" data-testid="precharge-retry" @click="load">
        {{ t('admin.settings.balancePrecharge.reload') }}
      </button>
    </div>
    <BalancePrechargeReviews />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getBalancePrechargeSettings, updateBalancePrechargeSettings } from '@/api/admin/settings'
import type { BalancePrechargeSettings } from '@/api/admin/settings'
import Toggle from '@/components/common/Toggle.vue'
import BalancePrechargeReviews from './BalancePrechargeReviews.vue'
import { formatBalancePrechargeMoney, parseBalancePrechargeMoney, validBalancePrechargeAmounts } from '@/utils/balancePrecharge'

const { t } = useI18n()
const enabled = ref(false)
const threshold = ref('0')
const amount = ref('0')
const confirmed = ref<BalancePrechargeSettings | null>(null)
const loaded = ref(false)
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const error = ref('')
const busy = computed(() => !loaded.value || loading.value || saving.value)
const valid = computed(() => validBalancePrechargeAmounts(parseBalancePrechargeMoney(threshold.value), parseBalancePrechargeMoney(amount.value), enabled.value))

function apply(settings: BalancePrechargeSettings) {
  enabled.value = settings.enabled
  threshold.value = formatBalancePrechargeMoney(settings.threshold)
  amount.value = formatBalancePrechargeMoney(settings.amount)
}

async function load() {
  loading.value = true
  loaded.value = false
  saved.value = false
  error.value = ''
  try {
    const settings = await getBalancePrechargeSettings()
    confirmed.value = settings
    apply(settings)
    loaded.value = true
  } catch {
    error.value = 'admin.settings.balancePrecharge.loadFailed'
  } finally {
    loading.value = false
  }
}

async function save() {
  if (busy.value || !valid.value) return
  saving.value = true
  saved.value = false
  error.value = ''
  try {
    const settings = await updateBalancePrechargeSettings({ enabled: enabled.value, threshold: Number(threshold.value), amount: Number(amount.value) })
    confirmed.value = settings
    apply(settings)
    saved.value = true
  } catch {
    if (confirmed.value) apply(confirmed.value)
    loaded.value = false
    error.value = 'admin.settings.balancePrecharge.saveFailed'
  } finally {
    saving.value = false
  }
}

watch([enabled, threshold, amount], () => { saved.value = false }, { flush: 'sync' })
onMounted(() => { void load() })
</script>
