<template>
  <section id="story-scene" ref="scene" data-testid="home-story" data-phase="0" :class="{ 'story-english': locale !== 'zh' }" aria-labelledby="story-title">
    <div class="hero">
      <div class="hero-copy">
        <h1 id="story-title">
          <span class="hero-title-line"><strong class="emphasis-count emphasis-one">1</strong><span>{{ copy.api }}</span></span>
          <span class="hero-title-line"><strong class="emphasis-count emphasis-models">8+</strong><span>{{ copy.providers }}</span></span>
          <span class="hero-title-line"><strong class="emphasis-count emphasis-128">128+</strong><span>{{ copy.models }}</span></span>
        </h1>
        <div class="hero-actions">
          <a :href="primaryHref" class="primary-link">{{ resolvedPrimaryLabel }}<Icon name="arrowRight" class="icon" size="sm" aria-hidden="true" /></a>
          <a :href="secondaryHref" class="text-link">{{ resolvedSecondaryLabel }}<Icon name="arrowRight" class="icon" size="sm" aria-hidden="true" /></a>
        </div>
      </div>
      <div class="flow-field">
        <svg class="flow-geometry" viewBox="0 0 560 375" preserveAspectRatio="none" aria-hidden="true">
          <path id="request-track" class="flow-track" d="M72 180 C112 180 137 180 184 180" />
          <path id="model-track" class="flow-track" d="M280 298 L280 375" />
          <circle id="flow-pulse" cx="72" cy="180" r="4" opacity="0" />
        </svg>
        <div class="app-origin" aria-hidden="true"><Icon name="terminal" class="icon" size="md" />{{ t('home.public.models.app') }}</div>
        <div class="response-signal" aria-hidden="true"><Icon name="check" class="icon" size="sm" /></div>
        <div class="mascot-host" role="img" :aria-label="siteName" v-html="mascotMarkup" />
        <div class="scene-caption"><p id="story-caption">{{ copy.captions[0] }}</p></div>
      </div>
    </div>
    <div class="providers" aria-labelledby="providers-title">
      <div class="provider-bridge" aria-hidden="true">
        <svg viewBox="0 0 1160 110" preserveAspectRatio="none" aria-hidden="true">
          <line class="bridge-trunk bridge-trunk-desktop" x1="75%" y1="0" x2="75%" y2="85" />
          <line class="bridge-trunk bridge-trunk-mobile" x1="50%" y1="0" x2="50%" y2="85" />
          <line id="bridge-bus" class="bridge-line" x1="6%" y1="85" x2="94%" y2="85" />
          <line v-for="(provider, index) in providers" :key="provider.name" class="bridge-tap" :data-bridge-provider="index" :x1="`${provider.tap}%`" y1="85" :x2="`${provider.tap}%`" y2="110" />
          <circle id="bridge-pulse" cx="6%" cy="85" r="3.5" opacity="0" />
        </svg>
        <span class="bridge-label-text">{{ copy.routing }}</span>
      </div>
      <div class="section-heading">
        <h2 id="providers-title">{{ copy.providersTitle }}</h2>
        <a v-if="detailsHref" :href="detailsHref" class="text-link">{{ copy.viewModels }}<Icon name="arrowRight" class="icon" size="sm" aria-hidden="true" /></a>
      </div>
      <ul class="providers-grid">
        <li v-for="provider in providers" :key="provider.name" class="provider" :data-provider="provider.name">
          <component :is="detailsHref ? 'a' : 'div'" :href="detailsHref || undefined" class="provider-link">
            <img :src="provider.icon" :class="{ mono: provider.monochrome }" width="30" height="30" alt="">
            <span>{{ provider.name }}</span>
          </component>
        </li>
      </ul>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import openaiIcon from '@/assets/home/providers/openai.svg'
import anthropicIcon from '@/assets/home/providers/anthropic.svg'
import googleIcon from '@/assets/home/providers/google.svg'
import xaiIcon from '@/assets/home/providers/xai.svg'
import zaiIcon from '@/assets/home/providers/zai.svg'
import moonshotIcon from '@/assets/home/providers/moonshot.svg'
import deepseekIcon from '@/assets/home/providers/deepseek.svg'
import minimaxIcon from '@/assets/home/providers/minimax.svg'
import { createHomeStoryMascot } from './homeStoryMascot'
import { startHomeStoryMotion, type HomeStoryController } from './homeStoryMotion'

const props = withDefaults(defineProps<{
  siteName?: string
  primaryHref?: string
  primaryLabel?: string
  secondaryHref?: string
  secondaryLabel?: string
  detailsHref?: string
}>(), {
  siteName: 'Sub2API',
  primaryHref: '/login',
  primaryLabel: '',
  secondaryHref: '/docs',
  secondaryLabel: '',
  detailsHref: '',
})
const { t, locale } = useI18n()
const copy = computed(() => locale.value === 'zh' ? {
  api: '个API', providers: '模型厂家', models: '前沿大模型',
  providersTitle: '支持的模型厂商', viewModels: '查看模型', routing: '智能调度',
  captions: ['一个请求，统一接入。', '连接模型，按需路由。', '结果返回，调用完成。'],
  reducedMotion: '统一接入，按需调用。',
} : {
  api: 'API', providers: 'model providers', models: 'frontier models',
  providersTitle: 'Supported model providers', viewModels: 'View models', routing: 'Smart routing',
  captions: ['One request, one gateway.', 'Routing to the right model.', 'Response returned, done.'],
  reducedMotion: 'One gateway, every model.',
})
const resolvedPrimaryLabel = computed(() => props.primaryLabel || t('home.public.hero.createApiKey'))
const resolvedSecondaryLabel = computed(() => props.secondaryLabel || t('home.public.nav.docs'))
const providers = [
  { name: 'OpenAI', icon: openaiIcon, monochrome: true, tap: 6 },
  { name: 'Anthropic', icon: anthropicIcon, monochrome: true, tap: 18.6 },
  { name: 'Google', icon: googleIcon, monochrome: false, tap: 31.1 },
  { name: 'xAI', icon: xaiIcon, monochrome: true, tap: 43.7 },
  { name: 'Z.ai', icon: zaiIcon, monochrome: true, tap: 56.3 },
  { name: 'Moonshot', icon: moonshotIcon, monochrome: true, tap: 68.9 },
  { name: 'DeepSeek', icon: deepseekIcon, monochrome: false, tap: 81.4 },
  { name: 'MiniMax', icon: minimaxIcon, monochrome: false, tap: 94 },
]
const scene = ref<HTMLElement | null>(null)
const mascotMarkup = createHomeStoryMascot()
let motion: HomeStoryController | undefined

onMounted(() => {
  if (scene.value) motion = startHomeStoryMotion(scene.value, () => copy.value)
})
watch(copy, () => motion?.refresh(), { flush: 'post' })
onBeforeUnmount(() => motion?.dispose())
</script>
