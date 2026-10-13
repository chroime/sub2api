<script setup lang="ts">
import { computed, ref } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { buildCanvasUrl, DEFAULT_CANVAS_ORIGIN, normalizeCanvasOrigin } from '@/features/canvasHost/protocol'

type LoadState = 'loading' | 'ready' | 'error'

const frameKey = ref(0)
const loadState = ref<LoadState>('loading')

const canvasOrigin = computed(() => {
  try {
    return normalizeCanvasOrigin(import.meta.env.VITE_CANVAS_ORIGIN || DEFAULT_CANVAS_ORIGIN)
  } catch {
    return DEFAULT_CANVAS_ORIGIN
  }
})
const canvasUrl = computed(() => buildCanvasUrl(canvasOrigin.value))

function reloadCanvas() {
  loadState.value = 'loading'
  frameKey.value += 1
}

function openCanvas() {
  const opened = window.open(canvasUrl.value, '_blank')
  if (!opened) loadState.value = 'error'
}
</script>

<template>
  <AppLayout>
    <section class="flex min-h-[calc(100vh-8rem)] flex-col overflow-hidden rounded-lg bg-white shadow-sm dark:bg-dark-900" data-canvas-embed>
      <header class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-700">
        <div class="min-w-0">
          <h1 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ $t('infiniteCanvas.title') }}</h1>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ $t('infiniteCanvas.embed.description') }}</p>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <button type="button" data-canvas-open class="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 transition hover:bg-gray-50 dark:border-dark-600 dark:text-dark-200 dark:hover:bg-dark-800" @click="openCanvas">
            {{ $t('infiniteCanvas.embed.openNewTab') }}
          </button>
          <button type="button" data-canvas-reload class="rounded-md bg-primary-600 px-3 py-1.5 text-sm text-white transition hover:bg-primary-700" @click="reloadCanvas">
            {{ $t('infiniteCanvas.embed.reload') }}
          </button>
        </div>
      </header>
      <div class="relative min-h-0 flex-1">
        <iframe
          :key="frameKey"
          :src="canvasUrl"
          :title="$t('infiniteCanvas.title')"
          class="h-full min-h-[calc(100vh-13rem)] w-full border-0"
          referrerpolicy="strict-origin-when-cross-origin"
          allow="clipboard-read; clipboard-write"
          data-canvas-iframe
          @load="loadState = 'ready'"
          @error="loadState = 'error'"
        />
        <div v-if="loadState === 'error'" class="absolute inset-0 flex items-center justify-center bg-white/90 p-6 text-center dark:bg-dark-900/90">
          <div>
            <p class="text-sm text-gray-600 dark:text-dark-300">{{ $t('infiniteCanvas.embed.loadFailed') }}</p>
            <button type="button" class="mt-3 rounded-md bg-primary-600 px-3 py-1.5 text-sm text-white" @click="reloadCanvas">{{ $t('infiniteCanvas.embed.retry') }}</button>
          </div>
        </div>
        <div v-else-if="loadState === 'loading'" class="pointer-events-none absolute inset-x-0 top-3 flex justify-center">
          <span class="rounded-md bg-white/90 px-3 py-1 text-xs text-gray-500 shadow-sm dark:bg-dark-800/90 dark:text-dark-300">{{ $t('infiniteCanvas.embed.loading') }}</span>
        </div>
      </div>
    </section>
  </AppLayout>
</template>
