<template>
  <section aria-labelledby="public-integration-title" class="public-home-integration border-b border-white/10 bg-[#070c11] text-slate-100" :class="{ 'public-home-integration-b2': props.presentation === 'b2' }">
    <div class="mx-auto grid max-w-7xl gap-10 px-5 py-16 sm:px-8 sm:py-20 lg:grid-cols-[0.85fr_1.15fr] lg:items-center lg:gap-14 lg:px-10">
      <div>
        <h2 id="public-integration-title" class="mt-2 text-3xl font-semibold tracking-normal text-white sm:text-4xl">{{ t('home.public.integration.title') }}</h2>
        <p class="mt-4 text-sm leading-7 text-slate-400 sm:text-base">{{ t('home.public.integration.description', { siteName: props.siteName }) }}</p>
        <ul v-if="props.presentation !== 'b2'" class="mt-7 space-y-3" role="list"><li v-for="item in resolvedBenefits" :key="item" class="flex items-start gap-3 text-sm text-slate-300"><span class="integration-benefit-icon mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full border border-cyan-300/30 bg-cyan-300/10 text-cyan-200"><Icon name="check" size="xs" :stroke-width="2" aria-hidden="true" /></span><span>{{ item }}</span></li></ul>
        <a :href="props.docsHref || '/docs'" class="mt-8 inline-flex min-h-11 items-center gap-2 rounded-lg border border-white/15 bg-white/[0.03] px-4 py-2.5 text-sm font-semibold text-slate-100 transition hover:border-cyan-300/50 hover:bg-cyan-300/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-200"><span>{{ t('home.public.integration.apiReference') }}</span><Icon name="externalLink" size="sm" aria-hidden="true" /></a>
      </div>
      <div class="overflow-hidden rounded-md border border-white/10 bg-[#111a22] shadow-xl shadow-black/25">
        <div class="flex flex-col gap-3 border-b border-white/10 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"><div class="flex items-center gap-2" role="tablist" :aria-label="t('home.public.integration.codePanelAria')"><button v-for="sample in resolvedSamples" :id="`tab-${sample.id}`" :key="sample.id" type="button" role="tab" :data-code="sample.id" aria-controls="code-panel" :aria-selected="activeId === sample.id" class="rounded-md px-3 py-1.5 text-xs font-semibold transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-200" :class="activeId === sample.id ? 'bg-cyan-300 text-[#071014]' : 'text-slate-400 hover:bg-white/10 hover:text-white'" @click="activeId = sample.id">{{ sample.label }}</button></div><button id="copy-code" type="button" class="inline-flex items-center gap-1.5 self-start rounded-md border border-white/10 px-2.5 py-1.5 text-xs font-semibold text-slate-400 transition hover:border-cyan-300/40 hover:text-cyan-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-200 sm:self-auto" :title="t('home.public.integration.copy')" @click="emit('copy', activeSample.code)"><Icon name="copy" size="xs" aria-hidden="true" />{{ t('home.public.integration.copy') }}</button></div>
        <div id="code-panel" class="integration-code-scroll p-4 sm:p-6" role="tabpanel" :aria-labelledby="`tab-${activeSample.id}`" :aria-label="t('home.public.integration.codePanelFor', { label: activeSample.label })"><pre id="code-output" class="integration-code font-mono text-xs leading-6 text-slate-300"><code><template v-for="(line, index) in activeSample.code.split('\n')" :key="`${activeSample.id}-${index}`"><span class="integration-line-number select-none">{{ String(index + 1).padStart(2, ' ') }}  </span><span class="integration-line-text">{{ line }}</span><br /></template></code></pre></div>
        <div class="flex flex-col gap-2 border-t border-white/10 bg-[#0b1117] px-4 py-3 text-xs sm:flex-row sm:items-center sm:justify-between sm:px-6"><span class="text-slate-500">{{ t('home.public.integration.baseUrl') }}</span><code class="overflow-x-auto whitespace-nowrap font-mono text-cyan-200">{{ props.baseUrl }}</code></div>
      </div>
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
interface CodeSample { id: string; label: string; code: string }
const props = withDefaults(defineProps<{ siteName?: string; baseUrl?: string; docsHref?: string; model?: string; benefits?: string[]; samples?: CodeSample[]; presentation?: 'default' | 'b2' }>(), { siteName: 'Sub2API', baseUrl: 'https://api.example.com/v1', docsHref: '/docs', model: 'gpt-5.6-sol', benefits: () => [], presentation: 'default' })
const emit = defineEmits<{ copy: [code: string] }>()
const { t } = useI18n()
const resolvedBenefits = computed(() => props.benefits?.length ? props.benefits : [t('home.public.integration.benefits.sdk'), t('home.public.integration.benefits.failover'), t('home.public.integration.benefits.limits')])
const modelLiteral = computed(() => JSON.stringify(props.model))
const baseUrlLiteral = computed(() => JSON.stringify(props.baseUrl))
const resolvedSamples = computed<CodeSample[]>(() => props.samples || [
  { id: 'curl', label: t('home.public.integration.curl'), code: [`curl ${props.baseUrl}/chat/completions \\`, '  -H "Authorization: Bearer $SUB2API_KEY" \\', '  -H "Content-Type: application/json" \\', "  -d '{", `    \"model\":${modelLiteral.value},`, '    "messages":[{"role":"user","content":"Hello"}]', "  }'"].join('\n') },
  { id: 'javascript', label: t('home.public.integration.javascript'), code: [`import OpenAI from 'openai'`, '', `const client = new OpenAI({`, '  apiKey: process.env.SUB2API_KEY,', `  baseURL: ${baseUrlLiteral.value},`, '})', '', 'const response = await client.chat.completions.create({', `  model: ${modelLiteral.value},`, "  messages: [{ role: 'user', content: 'Hello' }],", '})'].join('\n') },
  { id: 'python', label: t('home.public.integration.python'), code: [`from openai import OpenAI`, '', 'client = OpenAI(', `    api_key=os.environ['SUB2API_KEY'],`, `    base_url=${baseUrlLiteral.value},`, ')', '', 'response = client.chat.completions.create(', `    model=${modelLiteral.value},`, "    messages=[{'role': 'user', 'content': 'Hello'}],", ')'].join('\n') },
])
const activeId = ref(props.samples?.[0]?.id || 'curl')
const activeSample = computed(() => resolvedSamples.value.find((sample) => sample.id === activeId.value) || resolvedSamples.value[0] || { id: '', label: '', code: '' })
</script>
<style scoped>
.integration-code-scroll { overflow: hidden; }
.integration-code { color: #cbd5e1; white-space: pre; max-width: 100%; overflow: hidden; font-size: clamp(9px, .72vw, 12px); letter-spacing: -.01em; }
.integration-line-number { color: #64748b; }
.integration-line-text { color: #cbd5e1; }
</style>

<style>
.public-home-light .integration-benefit-icon {
  border-color: #5eead4;
  background: #ccfbf1;
  color: #047857;
  box-shadow: 0 0 0 2px rgb(20 184 166 / .08);
}
.public-home-light .integration-code-scroll { color: #334155; }
</style>
