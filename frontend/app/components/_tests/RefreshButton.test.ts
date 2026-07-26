import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import RefreshButton from '../RefreshButton.vue'

const stubs = {
  UTooltip: {
    setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) {
      return () => h('div', [slots.default?.()])
    },
  },
  UButton: {
    props: ['label', 'icon', 'disabled', 'ariaLabel'],
    emits: ['click'],
    setup(props: { label?: string; disabled?: boolean; ariaLabel?: string }, { emit }: { emit: (e: string, ev: MouseEvent) => void }) {
      return () => h('button', {
        disabled: props.disabled,
        'aria-label': props.ariaLabel,
        onClick: (ev: MouseEvent) => emit('click', ev),
      }, props.label)
    },
  },
}

describe('RefreshButton', () => {
  it('awaits an async handler and shows a spinning state while pending', async () => {
    vi.useFakeTimers()
    let resolveHandler: () => void = () => {}
    const handler = vi.fn(() => new Promise<void>((resolve) => { resolveHandler = resolve }))

    const wrapper = mount(RefreshButton, {
      attrs: { onClick: handler },
      global: { stubs },
    })

    await wrapper.get('button').trigger('click')
    expect(handler).toHaveBeenCalledTimes(1)
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()

    resolveHandler()
    await vi.advanceTimersByTimeAsync(400)

    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    vi.useRealTimers()
  })

  it('stops spinning after a rejected handler', async () => {
    vi.useFakeTimers()
    const handler = vi.fn(() => Promise.reject(new Error('boom')))

    const wrapper = mount(RefreshButton, {
      attrs: { onClick: handler },
      global: { stubs, config: { errorHandler: () => {} } },
    })

    const clickPromise = wrapper.get('button').trigger('click').catch(() => {})
    await vi.advanceTimersByTimeAsync(400)
    await clickPromise

    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()
    vi.useRealTimers()
  })
})
