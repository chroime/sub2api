import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import { describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import type { GroupIdentity } from '@/types'
import GroupIdentityEditor from '../GroupIdentityEditor.vue'
import zh from '@/i18n/locales/zh'

function editor(value: GroupIdentity = {}) {
  const Host = defineComponent({ components: { GroupIdentityEditor }, setup() { const identity = ref(value); return { identity } }, template: '<GroupIdentityEditor id="identity" v-model="identity" name="Grok Ultra" platform="grok" />' })
  return mount(Host, { global: { plugins: [createPinia(), createI18n({ legacy: false, locale: 'zh', messages: { zh } })] } })
}

describe('GroupIdentityEditor', () => {
  it('keeps older groups unlabelled until a preset is deliberately chosen and can clear it', async () => {
    const w = editor()
    try {
      expect(w.find('[data-group-identity]').exists()).toBe(false)
      await w.get('[data-identity-preset="self"]').trigger('click'); await flushPromises()
      expect(w.get('[data-group-identity]').text()).toBe('自营')
      expect(w.vm.identity).toEqual({ label: '自营', color: 'teal' })
      await w.get('[data-identity-preset="none"]').trigger('click')
      expect(w.vm.identity).toEqual({})
      expect(w.find('[data-group-identity]').exists()).toBe(false)
    } finally { w.unmount() }
  })

  it('supports custom text, palette and arbitrary hex color with an immediate escaped preview', async () => {
    const w = editor({ label: '合作', color: 'teal' })
    try {
      await w.get('#identity-label').setValue('<b>定制</b>')
      await w.get('[data-identity-color="blue"]').trigger('click')
      expect(w.vm.identity).toEqual({ label: '<b>定制</b>', color: 'blue' })
      expect(w.get('[data-group-identity]').find('b').exists()).toBe(false)
      await w.get('#identity-color').setValue('#ffc800')
      expect(w.vm.identity).toEqual({ label: '<b>定制</b>', color: '#ffc800' })
      expect(w.get('[data-group-identity]').attributes('style')).toContain('background-color: rgb(255, 200, 0)')
    } finally { w.unmount() }
  })

  it('counts Unicode characters and rejects oversized labels without silently truncating them', async () => {
    const w = editor()
    try {
      await w.get('#identity-label').setValue('😀'.repeat(12))
      expect(w.findComponent(GroupIdentityEditor).vm.validate()).toBe(true)
      await w.get('#identity-label').setValue('身份标识'.repeat(4))
      expect(w.get('#identity-label').attributes('aria-invalid')).toBe('true')
      expect(w.findComponent(GroupIdentityEditor).vm.validate()).toBe(false)
    } finally { w.unmount() }
  })
})
