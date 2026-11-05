import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ModelIcon from '../ModelIcon.vue'

describe('ModelIcon 深色模式', () => {
  it.each(['gpt-6-sol', 'grok-4.7', 'kimi-k3.1-pro'])('接近黑色的 %s 图标标记为单色，深色模式下换成浅色', (model) => {
    const svg = mount(ModelIcon, { props: { model } }).get('svg')
    expect(svg.classes()).toContain('model-icon-mono')
  })

  it.each(['claude-sonnet-5-5', 'gemini-3.8-flash', 'deepseek-v4-pro'])('有品牌色的 %s 图标保持原色', (model) => {
    const svg = mount(ModelIcon, { props: { model } }).get('svg')
    expect(svg.classes()).not.toContain('model-icon-mono')
  })
})
