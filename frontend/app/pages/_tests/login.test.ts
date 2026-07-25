import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import * as vue from 'vue'
import { h } from 'vue'
import LoginPage from '../login.vue'

type Slots = Record<string, (() => unknown) | undefined>

const stubs = {
  UAlert: { setup: () => () => h('div') },
  UIcon: { setup: () => () => h('span') },
  UFormField: {
    props: ['label'],
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', slots.default?.())
    },
  },
  UButton: {
    props: ['icon', 'label', 'variant', 'color', 'size', 'loading', 'type', 'block', 'ariaLabel'],
    emits: ['click'],
    setup(props: { type?: string, label?: string, ariaLabel?: string }, { emit, slots, attrs }: { emit: (e: string, ...a: unknown[]) => void, slots: Slots, attrs: Record<string, unknown> }) {
      return () => h('button', {
        type: props.type || 'button',
        'aria-label': props.ariaLabel ?? attrs['aria-label'],
        onClick: () => emit('click'),
      }, props.label ?? slots.default?.())
    },
  },
  AppTextInput: {
    props: ['modelValue', 'type', 'placeholder', 'icon', 'ariaLabel'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string, type?: string, ariaLabel?: string }, { emit, slots }: { emit: (e: string, v: string) => void, slots: Slots }) {
      return () => h('div', [
        h('input', {
          type: props.type || 'text',
          'aria-label': props.ariaLabel,
          value: props.modelValue,
          onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value),
        }),
        slots.trailing?.(),
      ])
    },
  },
  NuxtLink: {
    props: ['to'],
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('a', slots.default?.())
    },
  },
}

describe('login.vue password visibility toggle', () => {
  beforeEach(() => {
    for (const key of ['ref', 'computed', 'watch', 'onMounted', 'nextTick']) {
      (globalThis as any)[key] = (vue as any)[key]
    }
    ;(globalThis as any).definePageMeta = vi.fn()
    ;(globalThis as any).useHead = vi.fn()
    ;(globalThis as any).navigateTo = vi.fn()
    ;(globalThis as any).useAuth = () => ({
      login: vi.fn().mockResolvedValue({}),
      getSSOProviders: vi.fn().mockResolvedValue([]),
      loginWithSSO: vi.fn().mockResolvedValue({}),
    })
    ;(globalThis as any).useA11yAnnouncer = () => ({ announce: vi.fn() })
  })

  function mountLogin() {
    return mount(LoginPage, { global: { stubs } })
  }

  function passwordInput(wrapper: ReturnType<typeof mountLogin>) {
    return wrapper.find('input[aria-label="Password"]')
  }

  function toggleButton(wrapper: ReturnType<typeof mountLogin>) {
    return wrapper.findAll('button').filter(b => b.attributes('aria-label')?.includes('password'))[0]!
  }

  it('starts masked with a "Show password" toggle', async () => {
    const wrapper = mountLogin()
    await vue.nextTick()

    expect(passwordInput(wrapper).attributes('type')).toBe('password')
    expect(toggleButton(wrapper).attributes('aria-label')).toBe('Show password')
  })

  it('reveals the password on toggle and updates the aria-label', async () => {
    const wrapper = mountLogin()
    await vue.nextTick()

    await toggleButton(wrapper).trigger('click')

    expect(passwordInput(wrapper).attributes('type')).toBe('text')
    expect(toggleButton(wrapper).attributes('aria-label')).toBe('Hide password')
  })

  it('returns to masked state when toggled a second time', async () => {
    const wrapper = mountLogin()
    await vue.nextTick()

    await toggleButton(wrapper).trigger('click')
    await toggleButton(wrapper).trigger('click')

    expect(passwordInput(wrapper).attributes('type')).toBe('password')
    expect(toggleButton(wrapper).attributes('aria-label')).toBe('Show password')
  })
})
