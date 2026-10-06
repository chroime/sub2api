<template>
  <footer class="public-home-footer">
    <div class="public-home-footer-inner">
      <div class="public-footer-brand">
        <div class="public-footer-brand-mark">
          <SiteLogo :src="siteLogo" :alt="siteName" class="public-footer-brand-icon" />
        </div>
        <div class="min-w-0">
          <strong>{{ siteName }}</strong>
          <p>{{ subtitle || t('home.public.footer.brandDescription') }}</p>
        </div>
      </div>

      <section v-if="contactItems.length" class="public-contact-panel" aria-labelledby="public-contact-title">
        <div class="public-footer-heading">
          <h2 id="public-contact-title">{{ t('home.public.footer.contactTitle') }}</h2>
        </div>
        <ul class="public-contact-list" role="list">
          <li v-for="item in contactItems" :key="`${item.label}-${item.value}`" class="public-contact-item">
            <span class="public-contact-icon" aria-hidden="true"><Icon :name="item.icon" size="sm" /></span>
            <span class="public-contact-copy">
              <span class="public-contact-label">{{ item.label }}</span>
              <span class="public-contact-value">{{ item.value }}</span>
            </span>
            <button type="button" class="public-contact-copy-button" :title="t('home.public.footer.copyContact')" :aria-label="`${t('home.public.footer.copyContact')}: ${item.value}`" @click="copyContact(item)">
              <Icon :name="copiedValue === item.value ? 'check' : 'copy'" size="xs" aria-hidden="true" />
            </button>
          </li>
        </ul>
      </section>

      <nav class="public-footer-links" :aria-label="t('home.public.footer.linksLabel')">
        <a v-if="docsHref" :href="docsHref">{{ t('home.public.nav.docs') }}<Icon name="arrowRight" size="xs" aria-hidden="true" /></a>
        <a v-if="modelPlazaHref" :href="modelPlazaHref">{{ t('home.public.nav.modelPlaza') }}<Icon name="arrowRight" size="xs" aria-hidden="true" /></a>
      </nav>
    </div>
    <div class="public-footer-bottom">
      <span>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.public.footer.builtForTeams') }}</span>
      <span>{{ t('home.public.footer.serviceStatus') }}</span>
    </div>
  </footer>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import SiteLogo from './SiteLogo.vue'
import Icon from '@/components/icons/Icon.vue'

type ContactIcon = 'users' | 'chatBubble' | 'chat' | 'mail'
interface ContactItem { label: string; value: string; icon: ContactIcon }

const props = withDefaults(defineProps<{
  siteName: string
  siteLogo?: string
  subtitle?: string
  contactInfo?: string
  docsHref?: string
  modelPlazaHref?: string
  animatedMascot?: boolean
}>(), { siteLogo: '', subtitle: '', contactInfo: '', docsHref: '', modelPlazaHref: '', animatedMascot: false })

const { t } = useI18n()
const copiedValue = ref('')
const currentYear = new Date().getFullYear()

const contactItems = computed<ContactItem[]>(() => {
  const raw = props.contactInfo.trim()
  if (!raw) return []

  return raw
    .split(/[\r\n;；|]+/)
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map((entry) => {
      const match = entry.match(/^([^:：]{1,16})\s*[:：]\s*(.+)$/)
      const label = match?.[1]?.trim() || t('home.public.footer.serviceLabel')
      const value = (match?.[2] || entry).trim()
      return { label, value, icon: iconFor(label) }
    })
})

function iconFor(label: string): ContactIcon {
  if (/群|group|community/i.test(label)) return 'users'
  if (/邮箱|邮件|email|mail/i.test(label)) return 'mail'
  if (/微信|wechat/i.test(label)) return 'chat'
  return 'chatBubble'
}

async function copyContact(item: ContactItem): Promise<void> {
  try {
    await navigator.clipboard.writeText(item.value)
    copiedValue.value = item.value
    window.setTimeout(() => {
      if (copiedValue.value === item.value) copiedValue.value = ''
    }, 1600)
  } catch {
    // Clipboard access is optional on public pages.
  }
}
</script>

<style>
.public-home-footer { border-top: 1px solid rgb(255 255 255 / .1); background: #070c11; color: #cbd5e1; }
.public-home-footer-inner { display: grid; max-width: 1280px; margin: 0 auto; grid-template-columns: minmax(220px, 1fr) minmax(280px, 1.2fr) auto; gap: 40px; align-items: start; padding: 42px 40px 36px; }
.public-footer-brand { display: flex; min-width: 0; align-items: center; gap: 12px; }
.public-footer-brand-mark { display: grid; width: 40px; height: 40px; flex: 0 0 auto; place-items: center; background: transparent; }
.public-footer-brand-icon { width: 32px; height: 32px; }
.public-footer-brand strong { display: block; color: #f8fafc; font-size: 15px; font-weight: 650; line-height: 22px; }
.public-footer-brand p { margin-top: 3px; color: #94a3b8; font-size: 12px; line-height: 18px; }
.public-footer-heading h2 { color: #f8fafc; font-size: 14px; font-weight: 650; line-height: 20px; }
.public-footer-heading p { margin-top: 3px; color: #64748b; font-size: 12px; line-height: 18px; }
.public-contact-list { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0 24px; margin-top: 14px; }
.public-contact-item { display: flex; min-width: 0; align-items: center; gap: 9px; padding: 10px 0; border: 0; border-top: 1px solid rgb(148 163 184 / .16); border-radius: 0; background: transparent; }
.public-contact-icon { display: grid; width: 24px; height: 24px; flex: 0 0 auto; place-items: center; border-radius: 6px; background: transparent; color: #5eead4; }
.public-contact-copy { display: flex; min-width: 0; flex: 1; flex-direction: column; gap: 1px; }
.public-contact-label { overflow: hidden; color: #94a3b8; font-size: 10px; line-height: 15px; text-overflow: ellipsis; white-space: nowrap; }
.public-contact-value { overflow: hidden; color: #e2e8f0; font-size: 12px; line-height: 18px; text-overflow: ellipsis; white-space: nowrap; }
.public-contact-copy-button { display: grid; width: 24px; height: 24px; flex: 0 0 auto; place-items: center; border: 0; border-radius: 6px; background: transparent; color: #64748b; cursor: pointer; }
.public-contact-copy-button:hover { background: rgb(45 212 191 / .12); color: #99f6e4; }
.public-contact-copy-button:focus-visible { outline: 2px solid #5eead4; outline-offset: 2px; }
.public-footer-links { display: flex; min-width: 120px; flex-direction: column; gap: 11px; padding-top: 2px; }
.public-footer-links a { display: inline-flex; align-items: center; justify-content: space-between; gap: 12px; color: #94a3b8; font-size: 12px; line-height: 20px; }
.public-footer-links a:hover { color: #5eead4; }
.public-footer-bottom { display: flex; max-width: 1280px; margin: 0 auto; align-items: center; justify-content: space-between; gap: 20px; padding: 15px 40px 20px; border-top: 1px solid rgb(255 255 255 / .07); color: #64748b; font-size: 11px; line-height: 18px; }
.public-footer-bottom span:last-child { color: #5eead4; }
.public-home-light .public-home-footer { border-color: #dbe4e6; background: rgb(255 255 255 / .72); color: #475569; }
.public-home-light .public-footer-brand strong, .public-home-light .public-footer-heading h2 { color: #0f172a; }
.public-home-light .public-footer-brand p, .public-home-light .public-footer-heading p, .public-home-light .public-contact-label { color: #64748b; }
.public-home-light .public-contact-item { border-color: #d7e2e2; background: transparent; }
.public-home-light .public-contact-icon { background: transparent; color: #047857; }
.public-home-light .public-contact-value { color: #0f172a; }
.public-home-light .public-contact-copy-button { color: #64748b; }
.public-home-light .public-contact-copy-button:hover { background: #ecfdf5; color: #047857; }
.public-home-light .public-footer-links a { color: #475569; }
.public-home-light .public-footer-links a:hover { color: #047857; }
.public-home-light .public-footer-bottom { border-color: #dbe4e6; color: #64748b; }
.public-home-light .public-footer-bottom span:last-child { color: #0f766e; }
@media (max-width: 900px) { .public-home-footer-inner { grid-template-columns: minmax(180px, .8fr) minmax(280px, 1.2fr); } .public-footer-links { grid-column: 1 / -1; flex-direction: row; gap: 24px; padding-top: 0; } }
@media (max-width: 639px) { .public-home-footer-inner { grid-template-columns: 1fr; gap: 26px; padding: 32px 20px 28px; } .public-contact-list { grid-template-columns: 1fr; } .public-footer-links { grid-column: auto; flex-direction: row; flex-wrap: wrap; } .public-footer-bottom { flex-direction: column; align-items: flex-start; padding: 14px 20px 18px; } }
</style>
