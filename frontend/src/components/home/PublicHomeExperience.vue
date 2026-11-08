<template>
  <div class="home-experience public-theme public-app-theme" data-testid="home-experience">
    <PublicSiteHeader
      :site-name="siteName"
      :site-logo="siteLogo"
      :destination="destination"
      :authenticated="authenticated"
      variant="app"
    >
      <RouterLink v-if="showModelPlazaEntry" to="/model-plaza" class="public-nav-link">
        {{ t('nav.modelPlaza') }}
      </RouterLink>
      <span v-else class="public-nav-link home-disabled-nav" aria-disabled="true" :title="t('home.experience.modelPlazaUnavailable')">
        {{ t('nav.modelPlaza') }}<Icon name="lock" size="xs" aria-hidden="true" />
      </span>
      <a :href="docUrl" :target="docUrl === '/docs' ? undefined : '_blank'" :rel="docUrl === '/docs' ? undefined : 'noopener noreferrer'" class="public-nav-link">
        {{ t('publicDocs.title') }}
      </a>
    </PublicSiteHeader>

    <main>
      <section class="home-hero" :class="{ 'is-long-brand': siteName.length >= 20 }" aria-labelledby="home-title">
        <div class="home-hero-inner">
          <div class="home-hero-copy">
            <h1 id="home-title">{{ siteName }}</h1>
            <p class="home-hero-promise"><span>{{ t('home.experience.heroLead') }}</span><span>{{ t('home.experience.heroAccent') }}</span></p>
            <div class="home-hero-actions">
              <RouterLink :to="keyDestination" class="home-primary-action" data-testid="home-create-key">
                <Icon name="key" size="sm" aria-hidden="true" />
                {{ t('home.experience.createKey') }}
                <Icon name="arrowRight" size="sm" aria-hidden="true" />
              </RouterLink>
              <a :href="docUrl" :target="externalDocs ? '_blank' : undefined" :rel="externalDocs ? 'noopener noreferrer' : undefined" class="home-secondary-action"><Icon name="book" size="sm" aria-hidden="true" />{{ t('home.experience.docsAction') }}</a>
            </div>
            <ul class="home-hero-notes"><li v-for="item in heroNotes" :key="item"><Icon name="checkCircle" size="xs" aria-hidden="true" />{{ t(item) }}</li></ul>
          </div>

          <div class="home-flow" role="region" :aria-label="t('home.experience.routePreview')">
            <div class="home-flow-toolbar">
              <span class="home-flow-caption"><Icon name="terminal" size="sm" aria-hidden="true" />{{ t('home.experience.routePreview') }}</span>
              <span class="home-preview-label">{{ t('home.experience.codeLabel') }}</span>
            </div>
            <div class="home-flow-content">
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
                  <PlatformIcon :platform="route.platform" size="sm" aria-hidden="true" />
                  {{ route.label }}
                </button>
              </div>
              <div class="home-flow-track">
              <div class="home-flow-node">
                <span class="home-flow-symbol"><Icon name="link" size="md" aria-hidden="true" /></span>
                <span>{{ t('home.experience.routeInput') }}</span>
              </div>
              <span class="home-flow-wire" aria-hidden="true"><span /></span>
              <div class="home-flow-node home-flow-hub">
                <span class="home-flow-logo"><SiteLogo :src="siteLogo" alt="" :width="44" :height="44" /></span>
                <span>{{ t('home.experience.routeGateway') }}</span>
              </div>
              <span class="home-flow-wire home-flow-wire-out" aria-hidden="true"><span /></span>
              <div class="home-flow-node home-flow-destination">
                <span class="home-flow-symbol"><PlatformIcon :platform="selectedRoute.platform" size="lg" aria-hidden="true" /></span>
                <span>{{ selectedRoute.label }}</span>
              </div>
              </div>
              <div class="home-flow-bottom">
                <code data-testid="home-route-endpoint">POST {{ selectedRoute.endpoint }}</code>
              </div>
              <a href="#integration" class="home-flow-detail">{{ t('home.experience.integrationLink') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></a>
            </div>
          </div>
        </div>
        <div class="home-section-inner">
          <div class="home-ecosystem">
            <div class="home-ecosystem-copy"><p>{{ t('home.experience.ecosystemTitle') }}</p><span>{{ t('home.experience.ecosystemHint') }}</span></div>
            <ul class="home-provider-list" :aria-label="t('home.experience.routeProvider')">
              <li v-for="provider in providers" :key="provider.model"><ModelIcon :model="provider.model" size="22px" aria-hidden="true" />{{ provider.label }}</li>
            </ul>
          </div>
        </div>
      </section>

      <section class="home-capabilities" aria-labelledby="home-capabilities-title">
        <div class="home-section-inner">
          <div class="home-section-heading">
            <h2 id="home-capabilities-title">{{ t('home.experience.capabilitiesTitle') }}</h2>
            <p>{{ t('home.experience.workspaceBody') }}</p>
          </div>
          <div class="home-capability-grid">
            <article v-for="feature in features" :key="feature.title" class="home-capability">
              <span class="home-capability-icon"><Icon :name="feature.icon" size="lg" aria-hidden="true" /></span>
              <h3>{{ t(feature.title) }}</h3>
              <p>{{ t(feature.body) }}</p>
              <RouterLink v-if="feature.target === 'models' && showModelPlazaEntry" to="/model-plaza" class="home-text-link">{{ t('home.experience.exploreModels') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></RouterLink>
              <span v-else-if="feature.target === 'models'" class="home-text-link home-disabled-nav" aria-disabled="true"><Icon name="lock" size="xs" aria-hidden="true" />{{ t('home.experience.modelPlazaUnavailable') }}</span>
              <a v-else-if="feature.target === 'docs'" :href="docUrl" :target="externalDocs ? '_blank' : undefined" :rel="externalDocs ? 'noopener noreferrer' : undefined" class="home-text-link">{{ t('home.experience.docsAction') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></a>
              <RouterLink v-else :to="destination" class="home-text-link">{{ t('home.goToDashboard') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></RouterLink>
            </article>
          </div>
        </div>
      </section>

      <section id="integration" class="home-integration" aria-labelledby="home-integration-title">
        <div class="home-section-inner home-integration-layout">
          <div class="home-integration-copy">
            <h2 id="home-integration-title">{{ t('home.experience.integrationTitle') }}</h2>
            <p>{{ t('home.experience.integrationBody') }}</p>
            <ol class="home-integration-steps"><li v-for="(step, index) in steps" :key="step.title"><span class="home-step-number">0{{ index + 1 }}</span><div><h3>{{ t(step.title) }}</h3><p>{{ t(step.body) }}</p></div></li></ol>
          </div>
          <div class="home-code-tool">
            <div class="home-code-header">
              <span><Icon name="terminal" size="sm" aria-hidden="true" />{{ selectedRoute.label }} <span class="home-code-language">cURL</span></span>
              <button type="button" :title="t('home.experience.copyCode')" :aria-label="t('home.experience.copyCode')" @click="copyExample">
                <Icon :name="copyState === 'copied' ? 'check' : 'copy'" size="sm" aria-hidden="true" />
                {{ copyState === 'copied' ? t('home.experience.copiedCode') : t('home.experience.copyCode') }}
              </button>
            </div>
            <pre><code>{{ example }}</code></pre>
            <p class="home-copy-feedback" aria-live="polite">{{ copyState === 'copied' ? t('home.experience.copiedCode') : copyState === 'failed' ? t('home.experience.copyFailed') : t('home.experience.exampleNotice') }}</p>
          </div>
        </div>
      </section>
      <section class="home-section-inner home-start" aria-labelledby="home-start-title"><div class="home-start-panel"><div><h2 id="home-start-title">{{ t('home.experience.startTitle') }}</h2><p>{{ t('home.experience.startBody') }}</p></div><RouterLink :to="keyDestination" class="home-primary-action">{{ t('home.experience.createKey') }}<Icon name="arrowRight" size="sm" aria-hidden="true" /></RouterLink></div></section>
    </main>

    <footer class="home-footer home-section-inner"><span class="home-footer-brand"><SiteLogo :src="siteLogo" alt="" :width="24" :height="24" />{{ siteName }}<span>{{ subtitle }}</span></span><span>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</span></footer>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PublicSiteHeader from '@/components/common/PublicSiteHeader.vue'
import SiteLogo from '@/components/common/SiteLogo.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ModelIcon from '@/components/common/ModelIcon.vue'
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
const externalDocs = computed(() => props.docUrl !== '/docs')
const keyDestination = computed(() => props.authenticated ? '/keys' : { path: '/login', query: { redirect: '/keys' } })
const heroNotes = ['home.experience.protocolTag', 'home.experience.usageTag', 'home.experience.keyTag']
const steps = [
  { title: 'home.experience.stepKeyTitle', body: 'home.experience.stepKeyBody' },
  { title: 'home.experience.stepEndpointTitle', body: 'home.experience.stepEndpointBody' },
  { title: 'home.experience.stepModelTitle', body: 'home.experience.stepModelBody' },
]
const routes = [
  { id: 'openai', label: 'OpenAI', platform: 'openai', endpoint: '/v1/chat/completions' },
  { id: 'anthropic', label: 'Claude', platform: 'anthropic', endpoint: '/v1/messages' },
  { id: 'gemini', label: 'Gemini', platform: 'gemini', endpoint: '/v1beta/models/{model}:generateContent' },
] as const
const providers = [
  { model: 'gpt', label: 'OpenAI' },
  { model: 'claude', label: 'Claude' },
  { model: 'gemini', label: 'Gemini' },
  { model: 'grok', label: 'Grok' },
  { model: 'deepseek', label: 'DeepSeek' },
  { model: 'qwen', label: 'Qwen' },
  { model: 'glm', label: 'GLM' },
  { model: 'kimi', label: 'Kimi' },
  { model: 'minimax', label: 'MiniMax' },
  { model: 'doubao', label: 'Doubao' },
  { model: 'hunyuan', label: 'Hunyuan' },
  { model: 'ernie', label: 'ERNIE' },
  { model: 'mistral', label: 'Mistral' },
  { model: 'llama', label: 'Meta Llama' },
  { model: 'command', label: 'Cohere' },
  { model: 'perplexity', label: 'Perplexity' },
] as const
const features = [
  { icon: 'grid', title: 'home.experience.gatewayTitle', body: 'home.experience.gatewayBody', target: 'models' },
  { icon: 'book', title: 'home.experience.routingTitle', body: 'home.experience.routingBody', target: 'docs' },
  { icon: 'chart', title: 'home.experience.visibilityTitle', body: 'home.experience.visibilityBody', target: 'console' },
] as const
const activeRoute = ref<(typeof routes)[number]['id']>('openai')
const selectedRoute = computed(() => routes.find(route => route.id === activeRoute.value)!)
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
let copyGeneration = 0
watch(activeRoute, () => { copyGeneration++; copyState.value = 'idle' })
const exampleEndpoint = computed(() => {
  let url: URL
  try {
    url = new URL(props.apiBaseUrl.trim() || window.location.origin, window.location.origin)
    if (url.protocol !== 'https:' && url.protocol !== 'http:') throw new Error('Unsupported URL scheme')
  } catch {
    url = new URL(window.location.origin)
  }
  const segments = url.pathname.split('/').filter(Boolean)
  if (segments.at(-1)?.toLowerCase() === 'v1') segments.pop()
  url.pathname = `/${segments.concat(selectedRoute.value.endpoint.slice(1).replace('{model}', 'MODEL_ID').split('/')).join('/')}`
  url.username = ''
  url.password = ''
  url.search = ''
  url.hash = ''
  return url.toString()
})
const example = computed(() => {
  const protocol = activeRoute.value
  const headers = protocol === 'anthropic'
    ? ['x-api-key: YOUR_API_KEY', 'anthropic-version: 2023-06-01']
    : [protocol === 'gemini' ? 'x-goog-api-key: YOUR_API_KEY' : 'Authorization: Bearer YOUR_API_KEY']
  const body = protocol === 'gemini'
    ? { contents: [{ parts: [{ text: 'Hello' }] }] }
    : { model: 'MODEL_ID', ...(protocol === 'anthropic' ? { max_tokens: 1024 } : {}), messages: [{ role: 'user', content: 'Hello' }] }
  return [`curl "${exampleEndpoint.value}"`, ...headers.map(header => `  -H "${header}"`), '  -H "Content-Type: application/json"', `  -d '${JSON.stringify(body, null, 2)}'`].join(' \\\n')
})

async function copyExample() {
  const request = ++copyGeneration
  const value = example.value
  try {
    await navigator.clipboard.writeText(value)
    if (request === copyGeneration && example.value === value) copyState.value = 'copied'
  } catch {
    if (request === copyGeneration && example.value === value) copyState.value = 'failed'
  }
}
</script>

<style src="./publicHomeExperience.css"></style>
