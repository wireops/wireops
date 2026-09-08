import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, ref } from 'vue'
import RepositoryKeysPanel from '../RepositoryKeysPanel.vue'

const stubs = {
  AppPanelCard: { setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('div', [slots.header?.(), slots.default?.()]) } },
  AppTextInput: { setup() { return () => h('div', [h('input')]) } },
  UButton: {
    props: ['label', 'icon', 'disabled', 'loading'],
    setup(props: { label?: string, disabled?: boolean, loading?: boolean }, { attrs, emit }: { attrs: Record<string, unknown>, emit: (e: string) => void }) {
      return () => h('button', { ...attrs, disabled: props.disabled || props.loading, onClick: () => emit('click') }, props.label)
    },
    emits: ['click'],
  },
  UBadge: { setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('span', { class: 'ubadge' }, slots.default?.()) } },
  UTooltip: { setup(_props: unknown, { slots }: { slots: Record<string, (() => unknown) | undefined> }) { return () => h('div', slots.default?.()) } },
  UIcon: { setup() { return () => h('span') } },
  GithubIcon: { setup() { return () => h('span', { class: 'github-icon' }) } },
  GitlabIcon: { setup() { return () => h('span', { class: 'gitlab-icon' }) } },
  RepositoryKeyModal: true,
  ConfirmModal: true,
}

function gitlabKey(overrides: Record<string, any> = {}) {
  return {
    id: 'key-1',
    name: 'GitLab (@octocat)',
    auth_type: 'oauth_token',
    oauth_provider: 'gitlab',
    oauth_account_login: 'octocat',
    oauth_refresh_error: 'invalid_grant',
    ...overrides,
  }
}

function setupGlobals({ keys = [gitlabKey()], connect = vi.fn() }: { keys?: any[], connect?: ReturnType<typeof vi.fn> } = {}) {
  const toastAdd = vi.fn()
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => ({
        getFullList: vi.fn().mockResolvedValue(name === 'repository_keys' ? keys : []),
        delete: vi.fn(),
      }),
    },
  })
  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any>()
    const refresh = async () => { data.value = await fn() }
    return { data, refresh }
  }
  ;(globalThis as any).usePermissions = () => ({ canManageRepos: ref(true) })
  ;(globalThis as any).useRealtime = () => ({ subscribe: vi.fn() })
  ;(globalThis as any).useGitProviderOAuth = () => ({ connect })
  ;(globalThis as any).useToast = () => ({ add: toastAdd })
  return { toastAdd, connect }
}

async function mountPanel() {
  const wrapper = mount(RepositoryKeysPanel, { global: { stubs } })
  await flushPromises()
  return wrapper
}

describe('RepositoryKeysPanel reconnect flow', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows a reconnect badge and button for a key with a stored refresh error', async () => {
    setupGlobals()
    const wrapper = await mountPanel()

    expect(wrapper.text()).toContain('Reconnect required')
    expect(wrapper.findAll('button').some(b => b.text() === 'Reconnect')).toBe(true)
  })

  it('does not show a reconnect badge for a healthy oauth key', async () => {
    setupGlobals({ keys: [gitlabKey({ oauth_refresh_error: '' })] })
    const wrapper = await mountPanel()

    expect(wrapper.text()).not.toContain('Reconnect required')
  })

  it('reconnects successfully: refreshes the list and clears the loading state', async () => {
    const connect = vi.fn().mockResolvedValue({ keyId: 'key-1', login: 'octocat' })
    const { toastAdd } = setupGlobals({ connect })
    const wrapper = await mountPanel()

    const reconnectButton = wrapper.findAll('button').find(b => b.text() === 'Reconnect')!
    await reconnectButton.trigger('click')
    await flushPromises()

    expect(connect).toHaveBeenCalledWith('gitlab')
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ color: 'success' }))
    // The button must not be left stuck in a loading/disabled state.
    expect((wrapper.findAll('button').find(b => b.text() === 'Reconnect')!.element as HTMLButtonElement).disabled).toBe(false)
  })

  it('handles a cancelled/failed reconnect without leaving the button stuck loading', async () => {
    const connect = vi.fn().mockResolvedValue(null)
    const { toastAdd } = setupGlobals({ connect })
    const wrapper = await mountPanel()

    const reconnectButton = wrapper.findAll('button').find(b => b.text() === 'Reconnect')!
    await reconnectButton.trigger('click')
    await flushPromises()

    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ color: 'warning' }))
    expect((wrapper.findAll('button').find(b => b.text() === 'Reconnect')!.element as HTMLButtonElement).disabled).toBe(false)
  })
})
