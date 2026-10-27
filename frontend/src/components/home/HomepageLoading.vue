<template>
  <div class="public-theme min-h-screen" role="status" aria-busy="true">
    <span class="sr-only">{{ t('common.loading') }}</span>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineOptions({ inheritAttrs: false })
const { t } = useI18n()
</script>
