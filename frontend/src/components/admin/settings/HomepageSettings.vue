<template>
  <section class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700" aria-labelledby="homepage-template-label">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <label id="homepage-template-label" for="homepage-template" class="font-medium text-gray-900 dark:text-white">
        {{ t('admin.settings.site.homepageTemplate') }}
      </label>
      <button type="button" class="btn btn-secondary btn-sm" data-testid="homepage-preview" @click="preview">
        {{ t('admin.settings.site.homepagePreview') }}
      </button>
    </div>
    <Select id="homepage-template" v-model="template" :options="options" :aria-label="t('admin.settings.site.homepageTemplate')" />
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.site.homepageTemplateHint') }}</p>
    <div v-if="template === 'custom'" class="space-y-2">
      <label for="homepage-content" class="block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.site.homeContent') }}</label>
      <textarea id="homepage-content" v-model="content" rows="6" class="input font-mono text-sm" :placeholder="t('admin.settings.site.homeContentPlaceholder')" />
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.site.homepageCustomHint') }}</p>
      <p class="text-xs text-amber-600 dark:text-amber-400">{{ t('admin.settings.site.homeContentIframeWarning') }}</p>
    </div>
    <p v-if="previewError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ previewError }}</p>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import Select from '@/components/common/Select.vue'
import { homepageTemplates, type HomepageTemplate } from '@/utils/homepageTemplate'
import { createHomepagePreview } from '@/utils/homepagePreview'

const template = defineModel<HomepageTemplate>({ required: true })
const content = defineModel<string>('content', { required: true })
const props = defineProps<{ draft: Record<string, unknown> }>()
const { t } = useI18n()
const router = useRouter()
const previewError = ref('')
const options = computed(() => homepageTemplates.map(value => ({ value, label: t(`admin.settings.site.homepageTemplates.${value}`) })))
function preview() {
  previewError.value = ''
  try {
    const id = createHomepagePreview({ ...props.draft, homepage_template: template.value, home_content: content.value })
    const href = router.resolve({ name: 'HomepagePreview', query: { draft: id } }).href
    window.open(href, '_blank', 'noopener,noreferrer')
  } catch {
    previewError.value = t('admin.settings.site.homepagePreviewFailed')
  }
}
</script>
