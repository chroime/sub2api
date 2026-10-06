<template>
  <section aria-labelledby="supported-models-title" class="model-families">
    <div class="routing-inner">
      <div class="routing-heading">
        <h2 id="supported-models-title">{{ t('home.public.models.title') }}</h2>
        <a v-if="detailsHref" :href="detailsHref" class="routing-details">{{ t('home.public.models.pricing') }}<Icon name="arrowRight" size="sm" /></a>
      </div>
      <div ref="graph" class="routing-graph">
        <svg class="routing-lines" :viewBox="`0 0 ${size.width} ${size.height}`" preserveAspectRatio="none" aria-hidden="true">
          <path :d="inputPath" class="routing-wire" />
          <path :d="inputPath" class="routing-input-pulse" pathLength="100" />
          <g v-for="(family, index) in families" :key="family.name">
            <path :d="paths[index]" class="routing-branch" :class="{ 'is-highlighted': activeFamily === index }" />
            <path :d="paths[index]" class="routing-pulse" pathLength="100" :style="{ animationDelay: `${index * 1.4}s` }" />
          </g>
        </svg>
        <div ref="application" class="routing-app">
          <span class="routing-app-icon"><Icon name="terminal" size="xl" /></span>
          <strong>{{ t('home.public.models.app') }}</strong>
          <span>{{ t('home.public.models.appType') }}</span>
        </div>
        <div ref="gateway" class="routing-gateway">
          <SiteLogo class="dancing-gateway-logo" :src="siteLogo" :alt="siteName" :width="80" :height="96" />
          <strong>{{ siteName }}</strong>
          <span>{{ t('home.public.models.gateway') }}</span>
        </div>
        <ul class="model-family-list">
          <li v-for="(family, index) in families" :key="family.name" @mouseenter="activeFamily = index" @mouseleave="activeFamily = null" @focusin="activeFamily = index" @focusout="activeFamily = null">
            <component :is="detailsHref ? 'a' : 'div'" :href="detailsHref || undefined" class="model-node" :class="{ 'is-highlighted': activeFamily === index }">
              <span class="model-node-icon"><ModelIcon :model="family.name" size="28px" :class="{ monochrome: family.monochrome }" aria-hidden="true" /></span>
              <div><h3>{{ family.name }}</h3><p>{{ family.provider }}</p></div>
            </component>
          </li>
        </ul>
      </div>
      <p class="routing-coverage">{{ t('home.public.models.coverage') }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue'
import { useResizeObserver } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import SiteLogo from '@/components/common/SiteLogo.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'

withDefaults(defineProps<{ detailsHref?: string; siteName?: string; siteLogo?: string }>(), { siteName: 'Sub2API' })
const { t } = useI18n()
const families = [
  { name: 'GPT', provider: 'OpenAI', monochrome: true },
  { name: 'Claude', provider: 'Anthropic' },
  { name: 'Gemini', provider: 'Google' },
  { name: 'Grok', provider: 'xAI', monochrome: true },
  { name: 'GLM', provider: 'Z.ai' },
  { name: 'Kimi', provider: 'Moonshot AI', monochrome: true },
  { name: 'DeepSeek', provider: 'DeepSeek' },
  { name: 'MiniMax', provider: 'MiniMax', monochrome: true },
]
const graph = ref<HTMLElement>()
const application = ref<HTMLElement>()
const gateway = ref<HTMLElement>()
const activeFamily = ref<number | null>(null)
const size = ref({ width: 1000, height: 440 })
const paths = ref<string[]>(families.map(() => ''))
const inputPath = ref('')

// Measure real grid positions so paths stay attached across locales and breakpoints.
function updatePaths() {
  if (!graph.value || !application.value || !gateway.value) return
  const bounds = graph.value.getBoundingClientRect()
  if (!bounds.width || !bounds.height) return
  const app = application.value.getBoundingClientRect()
  const hub = gateway.value.getBoundingClientRect()
  size.value = { width: bounds.width, height: bounds.height }
  const appX = app.left - bounds.left + app.width / 2
  const hubX = hub.left - bounds.left + hub.width / 2
  const bottom = hub.bottom - bounds.top
  inputPath.value = `M ${appX} ${app.bottom - bounds.top} V ${hub.top - bounds.top}`
  paths.value = Array.from(graph.value.querySelectorAll('.model-node')).map(node => {
    const box = node.getBoundingClientRect()
    const x = box.left - bounds.left + box.width / 2
    const y = box.top - bounds.top
    return `M ${hubX} ${bottom} V ${y - 16} H ${x} V ${y}`
  })
}
useResizeObserver(graph, updatePaths)
onMounted(() => { void nextTick(updatePaths) })
</script>

<style scoped>
.model-families { background: #0b0f14; color: #e2e8f0; padding: 56px 20px 72px; letter-spacing: 0; border-bottom: 1px solid rgb(255 255 255 / .06); }
.routing-inner { max-width: 1200px; margin: 0 auto; }
.routing-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 20px; }
.routing-heading h2 { margin: 0; font-size: 26px; line-height: 1.35; font-weight: 650; letter-spacing: -.01em; }
.routing-details { display: inline-flex; align-items: center; gap: 8px; min-height: 40px; font-size: 13px; color: #a7f3d0; transition: color 160ms ease; }
.routing-details:hover { color: #ffffff; }
.routing-graph { position: relative; display: grid; grid-template-columns: minmax(0, 1fr); justify-items: center; gap: 42px; margin-top: 32px; padding: 12px 0; }
.routing-lines { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; pointer-events: none; }
.routing-wire, .routing-branch { fill: none; stroke: #27413f; stroke-width: 1; stroke-linejoin: round; transition: stroke 160ms ease; }
.routing-branch.is-highlighted { stroke: #34d399; stroke-width: 1.5; }
.routing-pulse, .routing-input-pulse { fill: none; stroke: #5eead4; stroke-width: 2; stroke-linecap: round; stroke-dasharray: 8 92; stroke-dashoffset: 108; opacity: 0; animation: route-flow 12s linear infinite; }
.routing-input-pulse { animation: route-input 2s linear infinite; }
.routing-app, .routing-gateway { position: relative; min-width: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; background: #0a1016; }
.routing-app { width: min(200px, 100%); min-height: 116px; gap: 8px; }
.routing-app-icon { display: flex; align-items: center; justify-content: center; width: 56px; height: 56px; color: #a5b4c5; }
.routing-app strong { font-size: 16px; font-weight: 600; }
.routing-app > span:last-child { color: #7c8d9c; font-size: 11px; }
.routing-gateway { width: min(240px, 100%); min-height: 176px; padding: 20px 12px; border: 1px solid rgb(52 211 153 / .35); border-radius: 6px; gap: 10px; box-shadow: 0 12px 28px rgb(0 0 0 / .14); }
.dancing-gateway-logo { display: block; width: 80px; height: 96px; flex-shrink: 0; object-fit: contain; }
.routing-gateway strong { max-width: 100%; overflow-wrap: anywhere; font-size: 20px; line-height: 1.3; }
.routing-gateway span { font-size: 12px; color: #93b4a8; }
.routing-gateway code { font-size: 12px; color: #6ee7b7; }
.model-family-list { position: relative; display: grid; width: 100%; grid-template-columns: repeat(4, minmax(0, 1fr)); column-gap: 32px; row-gap: 32px; margin: 0; padding: 0; list-style: none; }
.model-family-list li { min-width: 0; }
.model-node { display: flex; min-width: 0; min-height: 68px; align-items: center; justify-content: center; gap: 12px; padding: 10px 12px; background: #0a1016; border: 1px solid transparent; transition: border-color 160ms ease, background-color 160ms ease, color 160ms ease; }
.model-node-icon { box-sizing: border-box; display: flex; width: 56px; height: 56px; flex-shrink: 0; align-items: center; justify-content: center; border: 1px solid transparent; border-radius: 9px; background: transparent; }
.model-node-icon svg { display: block; flex-shrink: 0; width: 28px; height: 28px; }
.model-node div { min-width: 0; }
.model-node h3 { color: #dbe5ec; font-size: 15px; font-weight: 600; line-height: 22px; }
.model-node p { color: #7c8d9c; font-size: 11px; line-height: 18px; overflow-wrap: anywhere; }
.model-node.is-highlighted { border-color: transparent; background: rgb(16 40 38 / .55); }
.model-node.is-highlighted h3 { color: #a7f3d0; }
.model-node:focus-visible, .routing-details:focus-visible { outline: 2px solid #6ee7b7; outline-offset: 4px; }
.monochrome :deep(path) { fill: #dbe5ec; }
.routing-coverage { margin-top: 36px; text-align: center; color: #7c8d9c; font-size: 13px; }
:global(.public-home-light .model-families), :global(.public-home-light .routing-app), :global(.public-home-light .routing-gateway), :global(.public-home-light .model-node) { background: #f9fafb; color: #0f172a; }
:global(.public-home-light .model-node h3) { color: #0f172a; }
:global(.public-home-light .model-node .monochrome path) { fill: #334155; }
:global(.public-home-light .routing-details), :global(.public-home-light .routing-gateway code) { color: #047857; }
:global(.public-home-light .routing-gateway span), :global(.public-home-light .routing-app > span:last-child), :global(.public-home-light .model-node p), :global(.public-home-light .routing-coverage) { color: #526971; }
:global(.public-home-light .routing-branch), :global(.public-home-light .routing-wire) { stroke: #b3c8c0; }
:global(.public-home-light .routing-branch.is-highlighted) { stroke: #059669; }
@keyframes route-flow { 0% { opacity: 0; stroke-dashoffset: 106; } 1% { opacity: 1; } 11% { opacity: 1; stroke-dashoffset: 0; } 12%, 100% { opacity: 0; stroke-dashoffset: 0; } }
@keyframes route-input { 0% { opacity: 0; stroke-dashoffset: 106; } 10%, 80% { opacity: 1; } 100% { opacity: 0; stroke-dashoffset: 0; } }
@media (min-width: 640px) { .model-families { padding-right: 32px; padding-left: 32px; } }
@media (min-width: 1024px) { .model-families { padding-right: 40px; padding-left: 40px; } }
@media (max-width: 767px) {
  .routing-heading h2 { font-size: 24px; }
  .routing-graph { margin-top: 28px; }
  .routing-gateway strong { font-size: 18px; }
  .model-family-list { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .model-node { gap: 8px; padding: 10px 4px; }
  .model-node h3 { font-size: 14px; }
  .routing-coverage { font-size: 12px; line-height: 22px; margin-top: 24px; }
}
@media (max-width: 479px) {
  .model-family-list { grid-template-columns: minmax(0, 1fr); row-gap: 12px; }
  .routing-graph { gap: 32px; }
}
@media (prefers-reduced-motion: reduce) { .routing-pulse, .routing-input-pulse { animation: none; display: none; } .routing-branch, .model-node { transition: none; } }
</style>

<style>
/* Model routing is a scoped child component; keep the light-theme contrast
   overrides global so the page-level theme class can reach it. */
.public-home-light .routing-wire,
.public-home-light .routing-branch { stroke: #8aa6a2; stroke-width: 1.25; }
.public-home-light .routing-branch.is-highlighted { stroke: #047857; stroke-width: 2.25; }
.public-home-light .routing-pulse,
.public-home-light .routing-input-pulse { stroke: #059669; }
.public-home-light .model-node {
  background: #ffffff;
  border-color: #d7e2e2;
  border-bottom-color: #d7e2e2;
  box-shadow: 0 1px 2px rgb(15 23 42 / .04);
}
.public-home-light .model-node-icon { border-color: #d7e2e2; background: #f8fafc; }
.public-home-light .model-node:hover,
.public-home-light .model-node:focus-visible,
.public-home-light .model-node.is-highlighted {
  border-color: #059669;
  background: #ecfdf5;
  box-shadow: 0 0 0 2px rgb(16 185 129 / .16), 0 4px 12px rgb(15 23 42 / .08);
}
.public-home-light .model-node:hover .model-node-icon,
.public-home-light .model-node:focus-visible .model-node-icon,
.public-home-light .model-node.is-highlighted .model-node-icon { border-color: #6ee7b7; background: #d1fae5; }
.public-home-light .model-node.is-highlighted h3 { color: #065f46; }
.public-home-light .model-families,
.public-home-light .routing-app { background: transparent; }
.public-home-light .routing-gateway { background: #ffffff; }
.public-home-light .routing-details:hover,
.public-home-light .routing-details:focus-visible { color: #065f46; }
</style>
