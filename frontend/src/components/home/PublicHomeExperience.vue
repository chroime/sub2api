<template>
  <div class="home-experience public-theme" data-testid="home-experience">
    <PublicSiteHeader
      :site-name="siteName"
      :site-logo="siteLogo"
      :destination="destination"
      :authenticated="authenticated"
    >
      <RouterLink v-if="showModelPlazaEntry" to="/model-plaza" class="public-nav-link">
        {{ t('nav.modelPlaza') }}
      </RouterLink>
      <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="public-nav-link home-desktop-doc-link">
        {{ t('home.viewDocs') }}
      </a>
      <a v-if="docUrl" :href="docUrl" target="_blank" rel="noopener noreferrer" class="home-mobile-doc-link" :title="t('home.viewDocs')" :aria-label="t('home.viewDocs')">
        <Icon name="book" size="md" aria-hidden="true" />
      </a>
    </PublicSiteHeader>

    <main>
      <section class="home-hero" :class="{ 'is-long-brand': siteName.length >= 20 }" aria-labelledby="home-title">
        <div class="home-scene-lines" aria-hidden="true">
          <span v-for="line in 7" :key="line" />
        </div>
        <div class="home-hero-inner">
          <div class="home-hero-copy">
            <p class="home-eyebrow"><span class="home-status-dot" />{{ t('home.experience.eyebrow') }}</p>
            <h1 id="home-title">{{ siteName }}</h1>
            <p class="home-hero-subtitle">{{ subtitle }}</p>
            <div class="home-hero-actions">
              <RouterLink :to="destination" class="home-primary-action">
                {{ authenticated ? t('home.goToDashboard') : t('home.getStarted') }}
                <Icon name="arrowRight" size="sm" aria-hidden="true" />
              </RouterLink>
              <RouterLink v-if="showModelPlazaEntry" to="/model-plaza" class="home-secondary-action">
                {{ t('home.experience.exploreModels') }}
              </RouterLink>
              <a v-else href="#integration" class="home-secondary-action">{{ t('home.experience.integrationLink') }}</a>
            </div>
          </div>

          <div class="home-flow" role="region" :aria-label="t('home.experience.routePreview')">
            <div class="home-flow-toolbar">
              <span class="home-flow-caption">{{ t('home.experience.routePreview') }}</span>
              <div class="home-route-switch" role="group" :aria-label="t('home.experience.routeProtocol')">
                <button
                  v-for="route in routes"
                  :key="route.id"
                  type="button"
                  :data-testid="`home-route-${route.id}`"
                  :aria-pressed="activeRoute === route.id"
                  :class="{ 'is-active': activeRoute === route.id }"
                  @click="activeRoute = route.id"
                >
                  {{ route.label }}
                </button>
              </div>
            </div>
            <div class="home-flow-track" :style="{ '--route-color': selectedRoute.color }">
              <div class="home-flow-node">
                <span class="home-flow-symbol"><Icon name="link" size="md" aria-hidden="true" /></span>
                <span>{{ t('home.experience.routeInput') }}</span>
              </div>
              <span class="home-flow-wire" aria-hidden="true"><span /></span>
              <div class="home-flow-node home-flow-hub">
                <span class="home-flow-symbol"><SiteLogo :src="siteLogo" alt="" :width="30" :height="30" /></span>
                <span>{{ t('home.experience.routeGateway') }}</span>
              </div>
              <span class="home-flow-wire home-flow-wire-out" aria-hidden="true"><span /></span>
              <div class="home-flow-node home-flow-destination">
                <span class="home-flow-symbol"><PlatformIcon :platform="selectedRoute.platform" size="lg" aria-hidden="true" /></span>
                <span>{{ selectedRoute.label }}</span>
              </div>
            </div>
            <div class="home-flow-bottom">
              <span>{{ t('home.experience.routeProvider') }}</span>
              <code data-testid="home-route-endpoint">POST {{ selectedRoute.endpoint }}</code>
            </div>
          </div>
        </div>
      </section>

      <section class="home-capabilities" aria-labelledby="home-capabilities-title">
        <div class="home-section-inner">
          <div class="home-section-heading">
            <p class="home-section-eyebrow">{{ t('home.experience.capabilitiesEyebrow') }}</p>
            <h2 id="home-capabilities-title">{{ t('home.experience.capabilitiesTitle') }}</h2>
          </div>
          <div class="home-capability-grid">
            <article v-for="feature in features" :key="feature.title" class="home-capability">
              <span class="home-capability-icon"><Icon :name="feature.icon" size="md" aria-hidden="true" /></span>
              <h3>{{ t(feature.title) }}</h3>
              <p>{{ t(feature.body) }}</p>
            </article>
          </div>
        </div>
      </section>

      <section id="integration" class="home-integration" aria-labelledby="home-integration-title">
        <div class="home-section-inner home-integration-layout">
          <div class="home-integration-copy">
            <p class="home-section-eyebrow">{{ t('home.experience.integrationEyebrow') }}</p>
            <h2 id="home-integration-title">{{ t('home.experience.integrationTitle') }}</h2>
            <p>{{ t('home.experience.integrationBody') }}</p>
            <div class="home-provider-list" :aria-label="t('home.experience.routeProvider')">
              <span v-for="provider in providers" :key="provider.id"><PlatformIcon :platform="provider.id" size="md" aria-hidden="true" />{{ provider.label }}</span>
            </div>
          </div>
          <div class="home-code-tool">
            <div class="home-code-header">
              <span><span class="home-code-indicator" />{{ t('home.experience.codeLabel') }}</span>
              <button type="button" :title="t('home.experience.copyCode')" :aria-label="t('home.experience.copyCode')" @click="copyExample">
                <Icon :name="copyState === 'copied' ? 'check' : 'copy'" size="sm" aria-hidden="true" />
              </button>
            </div>
            <pre><code>{{ example }}</code></pre>
            <p class="home-copy-feedback" aria-live="polite">{{ copyState === 'copied' ? t('home.experience.copiedCode') : copyState === 'failed' ? t('home.experience.copyFailed') : t('home.experience.exampleNotice') }}</p>
          </div>
        </div>
      </section>
    </main>

    <footer class="home-footer">&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</footer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import PublicSiteHeader from '@/components/common/PublicSiteHeader.vue'
import SiteLogo from '@/components/common/SiteLogo.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  siteName: string
  subtitle: string
  siteLogo: string
  docUrl: string
  apiBaseUrl: string
  destination: string
  authenticated: boolean
  showModelPlazaEntry: boolean
  currentYear: number
}>()

const { t } = useI18n()
const routes = [
  { id: 'openai', label: 'OpenAI', platform: 'openai', endpoint: '/v1/chat/completions', color: '#87dec0' },
  { id: 'anthropic', label: 'Claude', platform: 'anthropic', endpoint: '/v1/messages', color: '#efbb8e' },
  { id: 'gemini', label: 'Gemini', platform: 'gemini', endpoint: '/v1beta/models/{model}:generateContent', color: '#a3c8f5' },
] as const
const providers = [
  { id: 'openai', label: 'OpenAI' },
  { id: 'anthropic', label: 'Claude' },
  { id: 'gemini', label: 'Gemini' },
  { id: 'grok', label: 'Grok' },
  { id: 'deepseek', label: 'DeepSeek' },
] as const
const features = [
  { icon: 'server', title: 'home.experience.gatewayTitle', body: 'home.experience.gatewayBody' },
  { icon: 'sync', title: 'home.experience.routingTitle', body: 'home.experience.routingBody' },
  { icon: 'chart', title: 'home.experience.visibilityTitle', body: 'home.experience.visibilityBody' },
] as const
const activeRoute = ref<(typeof routes)[number]['id']>('openai')
const selectedRoute = computed(() => routes.find(route => route.id === activeRoute.value)!)
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
const exampleEndpoint = computed(() => {
  let url: URL
  try {
    url = new URL(props.apiBaseUrl.trim() || window.location.origin)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') throw new Error('Unsupported URL scheme')
  } catch {
    url = new URL(window.location.origin)
  }
  const segments = url.pathname.split('/').filter(Boolean)
  if (segments.at(-1)?.toLowerCase() === 'v1') segments.pop()
  url.pathname = `/${[...segments, 'v1', 'chat', 'completions'].join('/')}`
  url.username = ''
  url.password = ''
  url.search = ''
  url.hash = ''
  return url.toString()
})
const example = computed(() => `curl "${exampleEndpoint.value}" \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{"model":"MODEL_ID","messages":[{"role":"user","content":"Hello"}]}'`)

async function copyExample() {
  try {
    await navigator.clipboard.writeText(example.value)
    copyState.value = 'copied'
  } catch {
    copyState.value = 'failed'
  }
}
</script>

<style src="./publicHomeExperience.css"></style>
