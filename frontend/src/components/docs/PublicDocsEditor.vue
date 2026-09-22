<template>
  <section class="docs-editor" aria-labelledby="public-docs-editor-title">
    <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
      <h2 id="public-docs-editor-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.site.docsTitle') }}</h2>
      <div class="flex items-center gap-2" role="tablist" :aria-label="t('admin.settings.site.docsContent')">
        <button type="button" role="tab" class="docs-editor-tab" :class="{ active: mode === 'edit' }" :aria-selected="mode === 'edit'" @click="mode = 'edit'">{{ t('publicDocs.edit') }}</button>
        <button type="button" role="tab" data-testid="docs-preview-tab" class="docs-editor-tab" :class="{ active: mode === 'preview' }" :aria-selected="mode === 'preview'" @click="mode = 'preview'">{{ t('publicDocs.preview') }}</button>
      </div>
    </div>
    <div v-if="mode === 'edit'" class="space-y-4">
      <div>
        <label for="public-docs-title" class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.site.docsTitle') }}</label>
        <input id="public-docs-title" :value="title" type="text" maxlength="120" class="input" :placeholder="t('admin.settings.site.docsTitlePlaceholder')" @input="emit('update:title', ($event.target as HTMLInputElement).value)" />
        <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.site.docsTitleHint') }}</p>
      </div>
      <div>
        <label for="public-docs-markdown" class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.site.docsContent') }}</label>
        <textarea id="public-docs-markdown" :value="content" rows="20" class="input font-mono text-sm" :placeholder="t('admin.settings.site.docsContentPlaceholder')" @input="emit('update:content', ($event.target as HTMLTextAreaElement).value)" />
        <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.site.docsContentHint') }}</p>
      </div>
      <button v-if="!content.trim()" type="button" data-testid="docs-insert-template" class="inline-flex items-center gap-2 rounded-lg border border-cyan-500/30 px-3 py-2 text-sm font-semibold text-cyan-700 hover:bg-cyan-50 dark:text-cyan-200 dark:hover:bg-cyan-900/20" @click="emit('update:content', template)">
        <Icon name="plus" size="sm" />{{ t('publicDocs.insertTemplate') }}
      </button>
    </div>
    <div v-else class="public-theme docs-editor-preview">
      <PublicDocsContent :title="title" :content="content.trim() ? content : template" :preview="true" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PublicDocsContent from './PublicDocsContent.vue'
import { buildIntegrationGuide } from '@/utils/integrationGuide'

const props = withDefaults(defineProps<{ title?: string; content?: string; baseUrl?: string }>(), { title: '', content: '', baseUrl: '' })
const emit = defineEmits<{ 'update:title': [value: string]; 'update:content': [value: string] }>()
const { t, locale } = useI18n()
const mode = ref<'edit' | 'preview'>('edit')
const template = computed(() => buildIntegrationGuide(locale.value, props.baseUrl))
</script>

<style scoped>
.docs-editor { border-top: 1px solid rgb(229 231 235); padding-top: 20px; }
.docs-editor-tab { border-radius: 6px; padding: 7px 12px; color: rgb(107 114 128); font-size: 13px; font-weight: 600; }
.docs-editor-tab.active { background: rgb(8 145 178 / .12); color: rgb(14 116 144); }
.docs-editor-preview { overflow: hidden; border: 1px solid var(--public-border); border-radius: 8px; background: var(--public-bg); padding: 24px; }
.docs-editor-preview :deep(.docs-sidebar) { display: none; }
.docs-editor-preview :deep(.has-chapters) { display: block; }
.docs-editor-preview :deep(.docs-article-header) { margin-bottom: 18px; padding-bottom: 18px; }
</style>
