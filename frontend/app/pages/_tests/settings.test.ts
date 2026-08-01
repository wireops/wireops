import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h, reactive } from 'vue'
import SettingsPage from '../settings.vue'

const stubs = {
  UIcon: {
    props: ['name'],
    setup(props: { name?: string }) {
      return () => h('span', { 'data-icon': props.name })
    },
  },
  NuxtPage: { setup: () => () => h('div', { class: 'nuxt-page-stub' }) },
}

function mountSettings(path: string) {
  ;(globalThis as any).useRoute = () => reactive({ path })
  ;(globalThis as any).navigateTo = vi.fn()
  return mount(SettingsPage, { global: { stubs } })
}

describe('settings.vue activeSection', () => {
  it('selects the Integrations section on /settings/integrations', () => {
    const wrapper = mountSettings('/settings/integrations')

    expect(wrapper.text()).toContain('Integrations')
    expect(wrapper.text()).toContain('Connect wireops to proxies, log viewers, notification channels and secret backends.')
    expect(wrapper.find('[data-icon="i-lucide-puzzle"]').exists()).toBe(true)
  })

  it('falls back to the General section for an unknown /settings path', () => {
    const wrapper = mountSettings('/settings/does-not-exist')

    expect(wrapper.text()).toContain('General')
    expect(wrapper.text()).toContain('Global timezone, SSO, and retention settings for the wireops instance.')
    expect(wrapper.find('[data-icon="i-lucide-settings-2"]').exists()).toBe(true)
  })
})
