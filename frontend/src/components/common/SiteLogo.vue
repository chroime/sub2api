<template>
  <picture class="site-logo" :class="{ 'site-logo-default': resolvedSrc === DEFAULT_SITE_LOGO }">
    <source
      v-if="resolvedSrc === DEFAULT_SITE_LOGO"
      media="(prefers-reduced-motion: reduce)"
      :srcset="STATIC_SITE_LOGO"
      type="image/svg+xml"
    />
    <img :src="resolvedSrc" :alt="alt" :width="width" :height="height" />
  </picture>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { DEFAULT_SITE_LOGO, STATIC_SITE_LOGO, resolveSiteLogo } from '@/utils/branding'

const props = withDefaults(defineProps<{
  src?: string
  alt: string
  width?: number | string
  height?: number | string
}>(), { src: '', width: 256, height: 256 })

const resolvedSrc = computed(() => resolveSiteLogo(props.src))
</script>

<style scoped>
.site-logo { display: inline-block; flex-shrink: 0; overflow: hidden; vertical-align: middle; }
.site-logo img { display: block; width: 100%; height: 100%; object-fit: contain; }
.site-logo-default {
  border-radius: 25%;
  background: #131617;
}
</style>
