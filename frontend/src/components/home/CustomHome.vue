<template>
  <div v-if="homeContent.trim()" data-testid="custom-home" class="min-h-screen">
    <iframe v-if="isUrl" :src="homeContent.trim()" :title="siteName" class="h-screen w-full border-0" allowfullscreen />
    <div v-else v-html="homeContent" />
  </div>
  <CompactHome v-else v-bind="props" />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import CompactHome from './CompactHome.vue'
import type { HomepagePresentationProps } from '@/utils/homepageTemplate'

const props = defineProps<HomepagePresentationProps & { homeContent: string }>()
const isUrl = computed(() => /^https?:\/\//i.test(props.homeContent.trim()))
</script>
