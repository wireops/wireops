import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, ref } from 'vue'
import MigrateStackModal from '../MigrateStackModal.vue'

type Slots = Record<string, (() => unknown) | undefined>

const stubs = {
  UCard: {
    setup(_props: unknown, { slots }: { slots: Slots }) {
      return () => h('div', [slots.header?.(), slots.default?.(), slots.footer?.()])
    },
  },
  UButton: {
    props: ['label', 'loading', 'disabled'],
    emits: ['click'],
    setup(props: { label?: string, loading?: boolean, disabled?: boolean }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', {
        class: 'u-button',
        disabled: props.disabled,
        'data-loading': props.loading ? 'true' : 'false',
        onClick: () => emit('click'),
      }, props.label)
    },
  },
  CancelButton: {
    emits: ['click'],
    setup(_props: unknown, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { class: 'cancel-button', onClick: () => emit('click') }, 'Cancel')
    },
  },
  UFormField: {
    props: ['label'],
    setup(props: { label?: string }, { slots }: { slots: Slots }) {
      return () => h('div', { class: 'form-field' }, [h('label', props.label), slots.default?.()])
    },
  },
  AppSelectInput: {
    props: ['modelValue', 'items', 'disabled', 'placeholder'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string, items?: { label: string, value: string }[], disabled?: boolean }, { emit }: { emit: (e: string, v: string) => void }) {
      return () => h('select', {
        class: 'app-select',
        value: props.modelValue,
        disabled: props.disabled,
        onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLSelectElement).value),
      }, (props.items || []).map(item => h('option', { value: item.value }, item.label)))
    },
  },
  UCheckbox: {
    props: ['modelValue', 'label'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: boolean, label?: string }, { emit }: { emit: (e: string, v: boolean) => void }) {
      return () => h('label', { class: 'u-checkbox' }, [
        h('input', {
          type: 'checkbox',
          checked: props.modelValue,
          onChange: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).checked),
        }),
        props.label,
      ])
    },
  },
  CommandLineLabel: {
    props: ['command'],
    setup(props: { command?: string }) {
      return () => h('code', { class: 'copy-command' }, props.command)
    },
  },
  UBadge: {
    props: ['label', 'color'],
    setup(props: { label?: string, color?: string }) {
      return () => h('span', { class: 'u-badge', 'data-color': props.color }, props.label)
    },
  },
  UIcon: { setup: () => () => h('span') },
}

function baseRepos() {
  return [
    { id: 'repo-source', name: 'source-repo', git_url: 'git@x:source.git' },
    { id: 'repo-target', name: 'target-repo', git_url: 'git@x:target.git' },
  ]
}

function setupGlobals(opts: {
  previewMigrateStack?: ReturnType<typeof vi.fn>
  migrateStack?: ReturnType<typeof vi.fn>
  getWireopsFiles?: ReturnType<typeof vi.fn>
  getStackFiles?: ReturnType<typeof vi.fn>
  repos?: any[]
} = {}) {
  const previewMigrateStack = opts.previewMigrateStack ?? vi.fn()
  const migrateStack = opts.migrateStack ?? vi.fn().mockResolvedValue({ status: 'migration_started' })
  const getWireopsFiles = opts.getWireopsFiles ?? vi.fn().mockResolvedValue(['wireops.yaml'])
  const getStackFiles = opts.getStackFiles ?? vi.fn().mockResolvedValue(['docker-compose.yml'])

  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: () => ({
        getFullList: vi.fn().mockResolvedValue(opts.repos ?? baseRepos()),
      }),
    },
  })
  ;(globalThis as any).useApi = () => ({
    getWireopsFiles,
    getStackFiles,
    previewMigrateStack,
    migrateStack,
  })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any[]>([])
    const refresh = async () => { data.value = await fn() }
    return { data, refresh }
  }

  return { previewMigrateStack, migrateStack, getWireopsFiles, getStackFiles }
}

function manualStack() {
  return { id: 'stack-1', name: 'my-stack', repository: 'repo-source', config_source: 'manual' }
}

function wireopsStack() {
  return { id: 'stack-1', name: 'my-stack', repository: 'repo-source', config_source: 'wireops_file' }
}

function baselinePreview(overrides: Partial<Record<string, any>> = {}) {
  return {
    source_repository: 'repo-source',
    target_repository: 'repo-target',
    services: { added: [], removed: [], common: ['web'] },
    volumes: { added: [], removed: [], common: [] },
    networks: { added: [], removed: [], common: [] },
    project_name: { source: 'myapp', target: 'myapp', same: true },
    sops: { status: 'none' },
    warnings: [],
    ...overrides,
  }
}

describe('MigrateStackModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('excludes the current repository from the target list and shows the compose-file picker for a manual stack', async () => {
    const { getStackFiles } = setupGlobals()
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    const selects = wrapper.findAll('select.app-select')
    const repoOptions = selects[0]!.findAll('option').map(o => o.attributes('value'))
    expect(repoOptions).toEqual(['repo-target'])

    await selects[0]!.setValue('repo-target')
    await flushPromises()

    expect(getStackFiles).toHaveBeenCalledWith('repo-target')
    // Second select (compose file) should now be present.
    expect(wrapper.findAll('select.app-select').length).toBe(2)
  })

  it('shows the wireops.yaml picker for a wireops-managed stack instead of a compose-file picker', async () => {
    const { getWireopsFiles, getStackFiles } = setupGlobals()
    const wrapper = mount(MigrateStackModal, { props: { stack: wireopsStack() }, global: { stubs } })
    await flushPromises()

    const selects = wrapper.findAll('select.app-select')
    await selects[0]!.setValue('repo-target')
    await flushPromises()

    expect(getWireopsFiles).toHaveBeenCalledWith('repo-target')
    expect(getStackFiles).not.toHaveBeenCalled()
  })

  it('disables the Migrate button until a preview has been fetched', async () => {
    const previewMigrateStack = vi.fn().mockResolvedValue(baselinePreview())
    setupGlobals({ previewMigrateStack })
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    const migrateButton = () => wrapper.findAll('button.u-button').find(b => b.text() === 'Migrate Stack')!
    expect(migrateButton().attributes('disabled')).toBeDefined()

    const selects = wrapper.findAll('select.app-select')
    await selects[0]!.setValue('repo-target')
    await flushPromises()
    await wrapper.findAll('select.app-select')[1]!.setValue('docker-compose.yml')
    await flushPromises()

    const previewButton = wrapper.findAll('button.u-button').find(b => b.text() === 'Preview Migration')!
    await previewButton.trigger('click')
    await flushPromises()

    expect(previewMigrateStack).toHaveBeenCalledWith('stack-1', {
      repository: 'repo-target',
      compose_path: '.',
      compose_file: 'docker-compose.yml',
    })
    expect(migrateButton().attributes('disabled')).toBeUndefined()
  })

  it('renders a removed volume as a critical (error-colored) badge', async () => {
    const previewMigrateStack = vi.fn().mockResolvedValue(baselinePreview({
      volumes: { added: [], removed: ['data'], common: [] },
      warnings: [{ severity: 'critical', code: 'volume_removed', message: "named volume \"data\" does not exist on the target" }],
    }))
    setupGlobals({ previewMigrateStack })
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    const selects = wrapper.findAll('select.app-select')
    await selects[0]!.setValue('repo-target')
    await flushPromises()
    await wrapper.findAll('select.app-select')[1]!.setValue('docker-compose.yml')
    await flushPromises()
    await wrapper.findAll('button.u-button').find(b => b.text() === 'Preview Migration')!.trigger('click')
    await flushPromises()

    const criticalBadge = wrapper.find('.u-badge[data-color="error"]')
    expect(criticalBadge.exists()).toBe(true)
    expect(criticalBadge.text()).toBe('data')
    expect(wrapper.text()).toContain('does not exist on the target')
  })

  it('shows a copyable target age public key when SOPS is undecryptable', async () => {
    const previewMigrateStack = vi.fn().mockResolvedValue(baselinePreview({
      sops: { status: 'undecryptable', target_age_public_key: 'age1abcdef' },
      warnings: [{ severity: 'warn', code: 'sops_undecryptable', message: 'target repository has a secrets.yaml that does not decrypt (target age public key: age1abcdef)' }],
    }))
    setupGlobals({ previewMigrateStack })
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    const selects = wrapper.findAll('select.app-select')
    await selects[0]!.setValue('repo-target')
    await flushPromises()
    await wrapper.findAll('select.app-select')[1]!.setValue('docker-compose.yml')
    await flushPromises()
    await wrapper.findAll('button.u-button').find(b => b.text() === 'Preview Migration')!.trigger('click')
    await flushPromises()

    const copyBlock = wrapper.find('.copy-command')
    expect(copyBlock.exists()).toBe(true)
    expect(copyBlock.text()).toBe('age1abcdef')
  })

  it('only shows the teardown checkbox when the project name differs', async () => {
    const previewMigrateStack = vi.fn().mockResolvedValue(baselinePreview({
      project_name: { source: 'old-name', target: 'new-name', same: false },
    }))
    setupGlobals({ previewMigrateStack })
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    expect(wrapper.find('.u-checkbox').exists()).toBe(false)

    const selects = wrapper.findAll('select.app-select')
    await selects[0]!.setValue('repo-target')
    await flushPromises()
    await wrapper.findAll('select.app-select')[1]!.setValue('docker-compose.yml')
    await flushPromises()
    await wrapper.findAll('button.u-button').find(b => b.text() === 'Preview Migration')!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.u-checkbox').exists()).toBe(true)
  })

  it('surfaces a preview API error instead of throwing', async () => {
    const previewMigrateStack = vi.fn().mockRejectedValue(new Error('target repository is the same'))
    setupGlobals({ previewMigrateStack })
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    const selects = wrapper.findAll('select.app-select')
    await selects[0]!.setValue('repo-target')
    await flushPromises()
    await wrapper.findAll('select.app-select')[1]!.setValue('docker-compose.yml')
    await flushPromises()
    await wrapper.findAll('button.u-button').find(b => b.text() === 'Preview Migration')!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('target repository is the same')
    expect(wrapper.findAll('button.u-button').find(b => b.text() === 'Migrate Stack')!.attributes('disabled')).toBeDefined()
  })

  it('confirms the migration with confirm=true and the teardown flag, then emits migrated', async () => {
    const previewMigrateStack = vi.fn().mockResolvedValue(baselinePreview({
      project_name: { source: 'old-name', target: 'new-name', same: false },
    }))
    const migrateStack = vi.fn().mockResolvedValue({ status: 'migration_started' })
    setupGlobals({ previewMigrateStack, migrateStack })
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    const selects = wrapper.findAll('select.app-select')
    await selects[0]!.setValue('repo-target')
    await flushPromises()
    await wrapper.findAll('select.app-select')[1]!.setValue('docker-compose.yml')
    await flushPromises()
    await wrapper.findAll('button.u-button').find(b => b.text() === 'Preview Migration')!.trigger('click')
    await flushPromises()

    await wrapper.find('.u-checkbox input').setValue(true)
    await wrapper.findAll('button.u-button').find(b => b.text() === 'Migrate Stack')!.trigger('click')
    await flushPromises()

    expect(migrateStack).toHaveBeenCalledWith('stack-1', {
      repository: 'repo-target',
      compose_path: '.',
      compose_file: 'docker-compose.yml',
      confirm: true,
      teardown_old_project: true,
    })
    expect(wrapper.emitted('migrated')).toBeTruthy()
  })

  it('emits cancel when the cancel button is clicked', async () => {
    setupGlobals()
    const wrapper = mount(MigrateStackModal, { props: { stack: manualStack() }, global: { stubs } })
    await flushPromises()

    await wrapper.find('.cancel-button').trigger('click')
    expect(wrapper.emitted('cancel')).toBeTruthy()
  })
})
