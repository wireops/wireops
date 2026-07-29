import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import EmptyState from '../EmptyState.vue'

describe('EmptyState', () => {
  const stubs = {
    UIcon: {
      props: ['name'],
      setup(props: any) {
        return () => h('span', { 'data-icon': props.name })
      },
    },
    ActionButton: {
      props: ['label', 'icon'],
      emits: ['click'],
      setup(props: any, { emit }: any) {
        return () => h('button', { onClick: () => emit('click') }, props.label)
      },
    },
  }

  it('renders title and description without a CTA when ctaLabel is unset', () => {
    const wrapper = mount(EmptyState, {
      props: {
        icon: 'i-lucide-inbox',
        title: 'No repositories configured yet',
        description: 'Ask an admin to add a repository to get started.',
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('No repositories configured yet')
    expect(wrapper.text()).toContain('Ask an admin to add a repository to get started.')
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('renders a CTA button and emits cta on click', async () => {
    const wrapper = mount(EmptyState, {
      props: {
        icon: 'i-lucide-inbox',
        title: 'No repositories configured yet',
        description: 'Add a repository to start tracking your compose stacks.',
        ctaLabel: 'Add Repository',
      },
      global: { stubs },
    })

    const button = wrapper.find('button')
    expect(button.exists()).toBe(true)
    expect(button.text()).toBe('Add Repository')

    await button.trigger('click')
    expect(wrapper.emitted('cta')).toHaveLength(1)
  })
})
