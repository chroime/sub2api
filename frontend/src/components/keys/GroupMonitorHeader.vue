<template>
  <div class="key-group-monitor-grid border-b border-gray-100 px-2.5 pb-2 pt-1 text-[10px] text-gray-500 dark:border-dark-700 dark:text-gray-400" data-group-monitor-header>
    <span>{{ t('keys.group') }}</span>
    <span class="text-center whitespace-nowrap">{{ t('keys.groupMonitoring.availability') }}</span>
    <span class="text-center whitespace-nowrap">{{ t('keys.groupMonitoring.intelligence') }}</span>
    <span class="pr-5 text-right whitespace-nowrap">{{ t('admin.groups.rateLabel') }}</span>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import './group-monitor.css'
const { t } = useI18n()
</script>
