import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import SecretRevealField from '../SecretRevealField.vue'

type Slots = Record<string, (() => unknown) | undefined>

const stubs = {
  AppTextInput: {
    props: ['modelValue', 'type', 'disabled', 'icon', 'title'],
    setup(props: { modelValue?: string, type?: string }, { slots }: { slots: Slots }) {
      return () => h('div', [
        h('input', { value: props.modelValue, type: props.type, disabled: true }),
        slots.trailing?.(),
      ])
    },
  },
  AppButtonInput: {
    props: ['icon', 'ariaLabel', 'disabled'],
    emits: ['click'],
    setup(props: { ariaLabel?: string, disabled?: boolean }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', {
        type: 'button',
        'aria-label': props.ariaLabel,
        disabled: props.disabled,
        onClick: () => emit('click'),
      })
    },
  },
}

describe('SecretRevealField', () => {
  const revealEnvVar = vi.fn()

  beforeEach(() => {
    revealEnvVar.mockReset()
    ;(globalThis as any).useApi = () => ({ revealEnvVar })
  })

  it('shows the masked placeholder until revealed', () => {
    const wrapper = mount(SecretRevealField, {
      props: { collection: 'stack_env_vars', envVarId: 'env-1' },
      global: { stubs },
    })
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('••••••••')
    expect(revealEnvVar).not.toHaveBeenCalled()
  })

  it('fetches and displays the plaintext value on reveal, then re-masks on hide', async () => {
    revealEnvVar.mockResolvedValue({ value: 's3cr3t' })
    const wrapper = mount(SecretRevealField, {
      props: { collection: 'stack_env_vars', envVarId: 'env-1' },
      global: { stubs },
    })

    await wrapper.find('button').trigger('click')
    await Promise.resolve()
    await Promise.resolve()

    expect(revealEnvVar).toHaveBeenCalledWith('stack_env_vars', 'env-1')
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('s3cr3t')

    await wrapper.find('button').trigger('click')
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('••••••••')
    // Hiding clears the cached plaintext rather than just toggling display —
    // a second reveal must re-fetch, not replay stale state.
    await wrapper.find('button').trigger('click')
    await Promise.resolve()
    await Promise.resolve()
    expect(revealEnvVar).toHaveBeenCalledTimes(2)
  })

  it('shows an inline error when the reveal request fails', async () => {
    revealEnvVar.mockRejectedValue({ data: { error: 'value is not an internal secret' } })
    const wrapper = mount(SecretRevealField, {
      props: { collection: 'stack_env_vars', envVarId: 'env-1' },
      global: { stubs },
    })

    await wrapper.find('button').trigger('click')
    await Promise.resolve()
    await Promise.resolve()

    expect(wrapper.text()).toContain('value is not an internal secret')
    expect((wrapper.find('input').element as HTMLInputElement).value).toBe('••••••••')
  })

  it('reveals a multiline secret for inspection and clears the expanded view on hide', async () => {
    revealEnvVar.mockResolvedValue({ value: 'first-secret\nsecond-secret\n' })
    const wrapper = mount(SecretRevealField, {
      props: { collection: 'global_env_vars', envVarId: 'env-1' },
      global: { stubs: { ...stubs, UIcon: true } },
    })
    await wrapper.get('[aria-label="Reveal value"]').trigger('click')
    await Promise.resolve()
    await Promise.resolve()
    await wrapper.get('[aria-label="Expand value"]').trigger('click')
    expect((wrapper.get('textarea').element as HTMLTextAreaElement).value).toBe('first-secret\nsecond-secret\n')
    await wrapper.get('[aria-label="Hide value"]').trigger('click')
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('first-secret')
  })
})
