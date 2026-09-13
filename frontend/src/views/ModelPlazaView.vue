<template>
  <!-- 后台内嵌形态:?embedded=1 且已登录,套完整后台布局 -->
  <AppLayout v-if="isEmbedded">
    <ModelPlazaContent :response="data" :loading="loading" :error="loadFailed" embedded />
  </AppLayout>

  <!-- 独立形态:自带导航条(logo/站名 + 登录/回后台) -->
  <div v-else class="public-model-plaza min-h-screen">
    <PlazaNavBar />
    <main class="public-model-plaza-main mx-auto max-w-7xl px-4 py-8 sm:px-6 lg:px-8 lg:py-10">
      <ModelPlazaContent :response="data" :loading="loading" :error="loadFailed" />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import PlazaNavBar from '@/components/modelPlaza/PlazaNavBar.vue'
import ModelPlazaContent from '@/components/modelPlaza/ModelPlazaContent.vue'
import { getModelPlaza, type ModelPlazaResponse } from '@/api/modelPlaza'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

// embedded=1 但未登录(如转发的链接)自动降级为独立形态。
const isEmbedded = computed(() => route.query.embedded === '1' && authStore.isAuthenticated)

const data = ref<ModelPlazaResponse | null>(null)
const loading = ref(true)
const loadFailed = ref(false)

onMounted(async () => {
  // 独立形态导航条需要站点名/Logo;有 __APP_CONFIG__ 注入时同步命中缓存。
  void appStore.fetchPublicSettings()
  try {
    data.value = await getModelPlaza()
  } catch {
    loadFailed.value = true
  } finally {
    loading.value = false
  }
})
</script>

<style scoped>
.public-model-plaza {
  --plaza-bg: #070c11;
  --plaza-panel: #0a1016;
  --plaza-border: rgb(148 163 184 / 0.16);
  min-height: 100vh;
  background: var(--plaza-bg);
  color: #e2e8f0;
}

.public-model-plaza-main {
  position: relative;
}

.public-model-plaza-main::before {
  position: absolute;
  inset: 0 1rem auto;
  height: 1px;
  background: linear-gradient(90deg, transparent, rgb(94 234 212 / 0.28), transparent);
  content: '';
  pointer-events: none;
}

:deep(.model-plaza-content) {
  color: #e2e8f0;
}

:deep(.model-plaza-content .rounded-xl),
:deep(.model-plaza-content .rounded-lg) {
  border-color: var(--plaza-border);
  background-color: var(--plaza-panel);
}

:deep(.model-plaza-content h1) { color: #f1f5f9; }
:deep(.model-plaza-content > div > p) { color: #94a3b8; }
:deep(.model-plaza-content .plaza-description) {
  border-color: var(--plaza-border);
  background: rgb(10 16 22 / 0.78);
  box-shadow: 0 18px 45px rgb(0 0 0 / 0.16);
}

:deep(.model-plaza-content a) {
  color: #99f6e4;
}

:deep(.model-plaza-content a:hover) {
  color: #ccfbf1;
}

@media (max-width: 639px) {
  .public-model-plaza-main { padding-top: 1.5rem; }
}
</style>
