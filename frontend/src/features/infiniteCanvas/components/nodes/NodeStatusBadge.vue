<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{ status?: string }>()
const { t } = useI18n()
const label = computed(() => t(`infiniteCanvas.status.${props.status && ['pending', 'completed', 'ready', 'failed', 'error'].includes(props.status) ? props.status : 'unknown'}`))
</script>

<template>
  <span class="node-status-badge" :class="`node-status-badge--${status ?? 'unknown'}`">{{ label }}</span>
</template>

<style scoped>
.node-status-badge { display: inline-flex; align-items: center; border-radius: 999px; padding: 2px 7px; font-size: 11px; line-height: 16px; background: #e2e8f0; color: #475569; }
.node-status-badge--pending { background: #fef3c7; color: #92400e; }
.node-status-badge--completed, .node-status-badge--ready { background: #dcfce7; color: #166534; }
.node-status-badge--failed, .node-status-badge--error { background: #fee2e2; color: #991b1b; }
</style>
