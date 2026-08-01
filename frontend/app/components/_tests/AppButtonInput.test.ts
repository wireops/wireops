import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import AppButtonInput from '../AppButtonInput.vue'

const stubs = {
  UIcon: {
    props: ['name'],
    setup(props: { name?: string }) {
      return () => h('span', { 'data-icon': props.name })
    },
  },
}

describe('AppButtonInput', () => {
  it('renders the label', () => {
    const wrapper = mount(AppButtonInput, {
      props: { label: 'Add Worker' },
      global: { stubs },
    })
    expect(wrapper.text()).toContain('Add Worker')
  })

  it('does not render a label when none is given', () => {
    const wrapper = mount(AppButtonInput, { global: { stubs } })
    expect(wrapper.text()).toBe('')
  })

  it('renders the icon when given', () => {
    const wrapper = mount(AppButtonInput, {
      props: { icon: 'i-lucide-plus' },
      global: { stubs },
    })
    expect(wrapper.find('[data-icon="i-lucide-plus"]').exists()).toBe(true)
  })

  it('does not render an icon when none is given', () => {
    const wrapper = mount(AppButtonInput, { global: { stubs } })
    expect(wrapper.find('[data-icon]').exists()).toBe(false)
  })

  it('renders the trailing slot', () => {
    const wrapper = mount(AppButtonInput, {
      global: {
        stubs,
      },
      slots: {
        trailing: () => h('span', { class: 'trailing-marker' }, 'kbd'),
      },
    })
    expect(wrapper.find('.trailing-marker').text()).toBe('kbd')
  })

  it('emits click with the mouse event', async () => {
    const wrapper = mount(AppButtonInput, { global: { stubs } })
    await wrapper.get('button').trigger('click')
    expect(wrapper.emitted('click')).toHaveLength(1)
    expect(wrapper.emitted('click')![0][0]).toBeInstanceOf(MouseEvent)
  })

  it('sets the disabled attribute and blocks click emission', async () => {
    const wrapper = mount(AppButtonInput, {
      props: { disabled: true },
      global: { stubs },
    })
    const button = wrapper.get('button')
    expect(button.attributes('disabled')).toBeDefined()

    await button.trigger('click')
    expect(wrapper.emitted('click')).toBeUndefined()
  })

  it('exposes an accessible name via ariaLabel independent of the visible label', () => {
    const wrapper = mount(AppButtonInput, {
      props: { ariaLabel: 'Search', icon: 'i-lucide-search' },
      global: { stubs },
    })
    const button = wrapper.get('button')
    expect(button.attributes('aria-label')).toBe('Search')
    expect(wrapper.text()).toBe('')
  })

  it('has no aria-label when none is given', () => {
    const wrapper = mount(AppButtonInput, {
      props: { label: 'Search' },
      global: { stubs },
    })
    expect(wrapper.get('button').attributes('aria-label')).toBeUndefined()
  })
})
