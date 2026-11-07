<script setup lang="ts">
import { computed } from 'vue'
import Icon from '@/components/icons/Icon.vue'
import sub2apiLogo from '@/assets/governance/sub2api.svg'
import newapiLogo from '@/assets/governance/newapi.png'

const props = defineProps<{ platform?: string }>()
const logo = computed(() => props.platform === 'sub2api' ? sub2apiLogo : props.platform === 'newapi' ? newapiLogo : null)
const label = computed(() => props.platform === 'sub2api' ? 'Sub2API' : 'New API')
</script>

<template>
  <img v-if="logo" :src="logo" :alt="label" width="16" height="16" class="h-4 w-4 shrink-0 rounded" />
  <Icon v-else name="server" size="sm" class="shrink-0 text-primary-600" aria-hidden="true" />
</template>
