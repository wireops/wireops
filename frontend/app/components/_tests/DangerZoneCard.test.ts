import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import DangerZoneCard from '../DangerZoneCard.vue'

type Slots = Record<string, (() => unknown) | undefined>

const stubs = {
  AccordionCard: {
    props: ['open', 'title', 'icon', 'iconClass', 'titleClass', 'chevronClass'],
    emits: ['update:open'],
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', [slots.default?.()])
    },
  },
  UButton: {
    props: ['label', 'color', 'variant', 'size', 'icon'],
    emits: ['click'],
    setup(props: { label?: string }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { onClick: () => emit('click') }, props.label)
    },
  },
}

function mountCard(actions: InstanceType<typeof DangerZoneCard>['$props']['actions']) {
  return mount(DangerZoneCard, {
    props: { actions },
    global: { stubs },
  })
}

describe('DangerZoneCard', () => {
  it('invokes the onClick callback for an action button', async () => {
    const onClick = vi.fn()
    const wrapper = mountCard([
      {
        key: 'restart',
        label: 'Restart stack',
        description: 'Restart all containers in this stack.',
        buttonLabel: 'Restart',
        onClick,
      },
    ])

    await wrapper.get('button').trigger('click')

    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('renders a separator between multiple actions but not before the first', () => {
    const wrapper = mountCard([
      {
        key: 'restart',
        label: 'Restart stack',
        description: 'Restart all containers in this stack.',
        buttonLabel: 'Restart',
        onClick: vi.fn(),
      },
      {
        key: 'delete',
        label: 'Delete stack',
        description: 'Permanently delete this stack.',
        buttonLabel: 'Delete',
        color: 'error',
        onClick: vi.fn(),
      },
    ])

    expect(wrapper.findAll('button')).toHaveLength(2)
    expect(wrapper.findAll('hr')).toHaveLength(1)
  })
})
