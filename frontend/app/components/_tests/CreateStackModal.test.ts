import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, reactive, ref } from 'vue'
import CreateStackModal from '../CreateStackModal.vue'

function setupGlobals() {
  const queryState = reactive<{ query: Record<string, any> }>({ query: {} })
  const push = vi.fn(({ query }: any) => { queryState.query = { ...query } })
  const replace = vi.fn(({ query }: any) => { queryState.query = { ...query } })

  ;(globalThis as any).useRoute = () => queryState
  ;(globalThis as any).useRouter = () => ({ push, replace })

  const createStack = vi.fn().mockResolvedValue({ id: 'stack-1' })
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => {
        if (name === 'stacks') return { create: createStack }
        if (name === 'repositories') return { getFullList: vi.fn().mockResolvedValue([{ id: 'repo-1', name: 'repo', git_url: 'git@x' }]) }
        return { getFullList: vi.fn().mockResolvedValue([]) }
      },
    },
  })

  const getWireopsFiles = vi.fn().mockResolvedValue(['wireops.yaml'])
  const getWireopsDefinitionFromFile = vi.fn()
  const getStackFiles = vi.fn().mockResolvedValue(['docker-compose.yml'])
  const createStackFromWireops = vi.fn().mockResolvedValue({ id: 'stack-1', name: 'api', status: 'pending' })
  // Worker tags come from the live /api/custom/workers route (reported by
  // the worker agent via WORKER_TAGS), not a raw PocketBase collection field.
  const getWorkers = vi.fn().mockResolvedValue([
    { id: 'worker-1', hostname: 'worker-a', status: 'ACTIVE', tags: ['prod', 'amd64'] },
    { id: 'worker-2', hostname: 'worker-b', status: 'ACTIVE', tags: [] },
  ])
  const lintCompose = vi.fn().mockResolvedValue({
    report: { findings: [], errors: 0, warnings: 0, infos: 0 },
  })
  ;(globalThis as any).useApi = () => ({
    getStackFiles,
    getWireopsFiles,
    getWireopsDefinitionFromFile,
    createStackFromWireops,
    getWorkers,
    lintCompose,
  })
  ;(globalThis as any).useValidation = () => ({
    validateComposePath: vi.fn().mockReturnValue(''),
    validateComposeFile: vi.fn().mockReturnValue(''),
  })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).useAsyncData = (_key: string, fn: () => Promise<any>) => {
    const data = ref<any[]>([])
    const refresh = async () => { data.value = await fn() }
    return { data, refresh }
  }

  return { createStack, getWireopsFiles, getWireopsDefinitionFromFile, getStackFiles, getWorkers, createStackFromWireops, lintCompose }
}

const stubs = {
  UModal: { template: '<div><slot name="content" /></div>' },
  UCard: { template: '<div><slot name="header" /><slot /><slot name="footer" /></div>' },
  UStepper: { props: ['modelValue', 'items'], template: '<div />' },
  UFormField: { props: ['label', 'error', 'required'], template: '<div><label>{{ label }}</label><slot /><div class="field-error">{{ error }}</div></div>' },
  AppTextInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string }, { emit }: { emit: (event: 'update:modelValue', value: string) => void }) {
      return () => h('input', {
        value: props.modelValue,
        onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
      })
    },
  },
  AppSelectInput: {
    props: ['modelValue', 'items', 'disabled'],
    emits: ['update:modelValue'],
    template: `<select :value="modelValue" :disabled="disabled" @change="$emit('update:modelValue', $event.target.value)">
      <option v-for="i in items" :key="i.value" :value="i.value">{{ i.label }}</option>
    </select>`,
  },
  UButton: {
    props: ['label', 'disabled', 'loading'],
    template: '<button :disabled="disabled" v-bind="$attrs"><slot>{{ label }}</slot></button>',
  },
  UAlert: { props: ['title', 'description', 'color'], template: '<div class="alert"><div>{{ title }}</div><div><slot name="description">{{ description }}</slot></div></div>' },
  UBadge: { props: ['label'], template: '<span class="badge">{{ label }}</span>' },
  UIcon: { template: '<span />' },
  // Rendered via h() rather than a template string — Codacy flags inline
  // HTML-string stubs as XSS even in tests.
  LintFindings: {
    props: ['report', 'loading', 'configError'],
    setup(props: { report?: { findings: { message: string }[] } | null; loading?: boolean; configError?: string }) {
      return () => h('div', { class: 'lint-findings' }, [
        props.loading ? h('span', { class: 'lint-loading' }, 'loading') : null,
        props.configError ? h('span', { class: 'lint-config-error' }, props.configError) : null,
        ...(props.report?.findings || []).map(f => h('li', { class: 'lint-finding' }, f.message)),
      ])
    },
  },
  ComposePreview: {
    props: ['content', 'filename', 'findings'],
    setup(props: { content?: string; filename?: string; findings?: { line?: number }[] }) {
      return () => h('div', { class: 'compose-preview', 'data-filename': props.filename }, [
        h('pre', { class: 'preview-content' }, props.content),
        h('span', { class: 'preview-marked-lines' },
          (props.findings || []).map(f => f.line).filter(Boolean).join(',')),
      ])
    },
  },
}

// Advances from the Configuration step to the Review step, where the lint
// runs.
async function goToReviewStep(wrapper: any) {
  const nextButton = wrapper.findAll('button').find((b: any) => b.text() === 'Next')
  await nextButton!.trigger('click')
  await flushPromises()
}

async function openInWireopsMode() {
  const wrapper = mount(CreateStackModal, {
    props: { open: false },
    global: { stubs },
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

describe('CreateStackModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('defaults to wireops_file mode and shows the file picker without a Name field', async () => {
    setupGlobals()
    const wrapper = await openInWireopsMode()

    expect(wrapper.text()).toContain('Manual')
    expect(wrapper.text()).toContain('From wireops.yaml')
    expect(wrapper.text()).toContain('wireops.yaml file')
    // no Name input in wireops_file mode — name comes from the file
    expect(wrapper.findAll('label').some(l => l.text() === 'Name')).toBe(false)
  })

  it('previews a valid wireops.yaml definition without letting the name be edited', async () => {
    const { getWireopsDefinitionFromFile } = setupGlobals()
    getWireopsDefinitionFromFile.mockResolvedValue({
      version: 'wireops.v1',
      name: 'api',
      deploy_timeout_seconds: 300,
      compose: { remove_orphans: true, force_pull: false },
      jobs: { wait_running: true },
      worker: { tags: ['prod'] },
      resolved_compose_path: '.',
      resolved_compose_file: 'docker-compose.yml',
    })

    const wrapper = await openInWireopsMode()

    const repoSelect = wrapper.findAll('select')[0]!
    await repoSelect.setValue('repo-1')
    await flushPromises()

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'wireops.yaml'))
    expect(fileSelect).toBeTruthy()
    await fileSelect!.setValue('wireops.yaml')
    await flushPromises()

    expect(getWireopsDefinitionFromFile).toHaveBeenCalledWith('repo-1', 'wireops.yaml')
    expect(wrapper.findAll('label').some(l => l.text() === 'Name')).toBe(false)
    expect(wrapper.text()).toContain('api')
    expect(wrapper.text()).toContain('not editable here')
    expect(wrapper.text()).toContain('remove_orphans: true')
    expect(wrapper.text()).toContain('force_pull: false')
  })

  it('blocks Next when the wireops.yaml file is invalid', async () => {
    const { getWireopsDefinitionFromFile } = setupGlobals()
    const err: any = new Error('invalid wireops.yaml')
    err.data = { errors: ['name is required'] }
    getWireopsDefinitionFromFile.mockRejectedValue(err)

    const wrapper = await openInWireopsMode()

    const repoSelect = wrapper.findAll('select')[0]!
    await repoSelect.setValue('repo-1')
    await flushPromises()

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'wireops.yaml'))
    await fileSelect!.setValue('wireops.yaml')
    await flushPromises()

    expect(wrapper.text()).toContain('name is required')

    const nextButton = wrapper.findAll('button').find(b => b.text() === 'Next')
    expect(nextButton?.attributes('disabled')).toBeDefined()
  })

  it('submits only repository/worker/wireops_file — never client-computed config', async () => {
    const { getWireopsDefinitionFromFile, createStackFromWireops } = setupGlobals()
    getWireopsDefinitionFromFile.mockResolvedValue({
      version: 'wireops.v1',
      name: 'api',
      deploy_timeout_seconds: 300,
      compose: { remove_orphans: false, force_pull: true },
      jobs: { wait_running: true },
      worker: { tags: ['prod'] },
      resolved_compose_path: '.',
      resolved_compose_file: 'docker-compose.yml',
    })

    const wrapper = await openInWireopsMode()

    const repoSelect = wrapper.findAll('select')[0]!
    await repoSelect.setValue('repo-1')
    await flushPromises()

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'wireops.yaml'))
    await fileSelect!.setValue('wireops.yaml')
    await flushPromises()

    const nextButton = wrapper.findAll('button').find(b => b.text() === 'Next')
    await nextButton!.trigger('click')
    await flushPromises()

    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    expect(workerSelect).toBeTruthy()
    // worker.tags: [prod] should filter out worker-b (no tags)
    expect(workerSelect!.findAll('option').some(o => o.text() === 'worker-b')).toBe(false)
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(createStackFromWireops).toHaveBeenCalledWith({
      repository: 'repo-1',
      worker: 'worker-1',
      wireops_file: 'wireops.yaml',
    })
  })

  it('falls back to every active worker and warns when worker.tags matches none', async () => {
    const { getWireopsDefinitionFromFile, createStackFromWireops, getWorkers } = setupGlobals()
    getWorkers.mockResolvedValue([
      { id: 'worker-1', hostname: 'worker-a', status: 'ACTIVE', tags: ['staging'] },
      { id: 'worker-2', hostname: 'worker-b', status: 'ACTIVE', tags: [] },
    ])
    getWireopsDefinitionFromFile.mockResolvedValue({
      version: 'wireops.v1',
      name: 'api',
      deploy_timeout_seconds: 300,
      compose: { remove_orphans: true, force_pull: false },
      jobs: { wait_running: false },
      worker: { tags: ['prod'] },
      resolved_compose_path: '.',
      resolved_compose_file: 'docker-compose.yml',
    })

    const wrapper = await openInWireopsMode()

    const repoSelect = wrapper.findAll('select')[0]!
    await repoSelect.setValue('repo-1')
    await flushPromises()

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'wireops.yaml'))
    await fileSelect!.setValue('wireops.yaml')
    await flushPromises()

    const nextButton = wrapper.findAll('button').find(b => b.text() === 'Next')
    await nextButton!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('No worker matches the required tags')

    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    expect(workerSelect).toBeTruthy()
    // fallback to every active worker — worker-b (no tags) must still be selectable
    expect(workerSelect!.findAll('option').some(o => o.text() === 'worker-b')).toBe(true)
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(createStackFromWireops).toHaveBeenCalledWith({
      repository: 'repo-1',
      worker: 'worker-1',
      wireops_file: 'wireops.yaml',
    })
  })

  it('manual mode still shows an editable Name and creates via the raw stacks collection', async () => {
    const { createStack } = setupGlobals()
    const wrapper = await openInWireopsMode()

    const manualButton = wrapper.findAll('button').find(b => b.text() === 'Manual')
    await manualButton!.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('label').some(l => l.text() === 'Name')).toBe(true)

    await wrapper.find('input').setValue('my-stack')
    const repoSelect = wrapper.findAll('select')[0]!
    await repoSelect.setValue('repo-1')
    await flushPromises()

    const nextButton = wrapper.findAll('button').find(b => b.text() === 'Next')
    await nextButton!.trigger('click')
    await flushPromises()

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'docker-compose.yml'))
    await fileSelect!.setValue('docker-compose.yml')

    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(createStack).toHaveBeenCalledWith(expect.objectContaining({
      name: 'my-stack',
      repository: 'repo-1',
      worker: 'worker-1',
      compose_path: '.',
      compose_file: 'docker-compose.yml',
      config_source: 'manual',
    }))
  })

  it('lints the selected compose file on reaching the Review step', async () => {
    const { lintCompose } = setupGlobals()
    lintCompose.mockResolvedValue({
      report: {
        findings: [
          { rule: 'compose/latest-tag', severity: 'warning', service: 'web', message: 'service "web" uses image "nginx", which is not pinned' },
        ],
        errors: 0,
        warnings: 1,
        infos: 0,
      },
    })

    const wrapper = await openInWireopsMode()

    const manualButton = wrapper.findAll('button').find(b => b.text() === 'Manual')
    await manualButton!.trigger('click')
    await flushPromises()

    await wrapper.find('input').setValue('my-stack')
    await wrapper.findAll('select')[0]!.setValue('repo-1')
    await flushPromises()

    await goToReviewStep(wrapper)

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'docker-compose.yml'))
    await fileSelect!.setValue('docker-compose.yml')
    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)

    expect(lintCompose).toHaveBeenCalledWith({
      repository: 'repo-1',
      compose_path: '.',
      compose_file: 'docker-compose.yml',
      worker: 'worker-1',
    })
    expect(wrapper.find('.lint-finding').text()).toContain('not pinned')
  })

  it('still allows creating the stack when the lint reports errors', async () => {
    const { lintCompose, createStack } = setupGlobals()
    lintCompose.mockResolvedValue({
      report: {
        findings: [
          { rule: 'policy/images', severity: 'error', service: 'web', message: 'image policy violation' },
        ],
        errors: 1,
        warnings: 0,
        infos: 0,
      },
    })

    const wrapper = await openInWireopsMode()

    const manualButton = wrapper.findAll('button').find(b => b.text() === 'Manual')
    await manualButton!.trigger('click')
    await flushPromises()

    await wrapper.find('input').setValue('my-stack')
    await wrapper.findAll('select')[0]!.setValue('repo-1')
    await flushPromises()

    await goToReviewStep(wrapper)

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'docker-compose.yml'))
    await fileSelect!.setValue('docker-compose.yml')
    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)
    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    // Lint is advisory: an error-severity finding must not gate creation.
    expect(createStack).toHaveBeenCalled()
  })

  it('surfaces a compose config failure instead of failing the request', async () => {
    const { lintCompose } = setupGlobals()
    lintCompose.mockResolvedValue({
      report: { findings: [], errors: 0, warnings: 0, infos: 0 },
      config_error: 'failed to resolve compose config: yaml: line 3: mapping values are not allowed',
    })

    const wrapper = await openInWireopsMode()

    const manualButton = wrapper.findAll('button').find(b => b.text() === 'Manual')
    await manualButton!.trigger('click')
    await flushPromises()

    await wrapper.find('input').setValue('my-stack')
    await wrapper.findAll('select')[0]!.setValue('repo-1')
    await flushPromises()

    await goToReviewStep(wrapper)

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'docker-compose.yml'))
    await fileSelect!.setValue('docker-compose.yml')
    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)

    expect(wrapper.find('.lint-config-error').text()).toContain('mapping values are not allowed')
  })

  it('previews the compose file with its findings on the Review step', async () => {
    const { lintCompose } = setupGlobals()
    lintCompose.mockResolvedValue({
      report: {
        findings: [
          { rule: 'compose/latest-tag', severity: 'warning', line: 3, message: 'unpinned image' },
          { rule: 'policy/privileged', severity: 'error', line: 5, message: 'privileged blocked' },
        ],
        errors: 1,
        warnings: 1,
        infos: 0,
      },
      content: 'services:\n  web:\n    image: nginx\n    privileged: true\n',
      filename: 'docker-compose.yml',
    })

    const wrapper = await openInWireopsMode()

    const manualButton = wrapper.findAll('button').find(b => b.text() === 'Manual')
    await manualButton!.trigger('click')
    await flushPromises()

    await wrapper.find('input').setValue('my-stack')
    await wrapper.findAll('select')[0]!.setValue('repo-1')
    await flushPromises()

    await goToReviewStep(wrapper)

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'docker-compose.yml'))
    await fileSelect!.setValue('docker-compose.yml')
    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)

    const preview = wrapper.find('.compose-preview')
    expect(preview.exists()).toBe(true)
    expect(preview.attributes('data-filename')).toBe('docker-compose.yml')
    expect(wrapper.find('.preview-content').text()).toContain('image: nginx')
    // The findings reach the preview so it can mark their lines.
    expect(wrapper.find('.preview-marked-lines').text()).toBe('3,5')
  })

  it('omits the preview when the server returned no file content', async () => {
    const { lintCompose } = setupGlobals()
    lintCompose.mockResolvedValue({
      report: { findings: [], errors: 0, warnings: 0, infos: 0 },
    })

    const wrapper = await openInWireopsMode()

    const manualButton = wrapper.findAll('button').find(b => b.text() === 'Manual')
    await manualButton!.trigger('click')
    await flushPromises()

    await wrapper.find('input').setValue('my-stack')
    await wrapper.findAll('select')[0]!.setValue('repo-1')
    await flushPromises()

    await goToReviewStep(wrapper)

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'docker-compose.yml'))
    await fileSelect!.setValue('docker-compose.yml')
    const workerSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'worker-a'))
    await workerSelect!.setValue('worker-1')

    await goToReviewStep(wrapper)

    expect(wrapper.find('.compose-preview').exists()).toBe(false)
    // The report itself must still render — a missing preview is not a failure.
    expect(wrapper.find('.lint-findings').exists()).toBe(true)
  })

  it('blocks Next into Review until a worker is picked', async () => {
    setupGlobals()
    const wrapper = await openInWireopsMode()

    const manualButton = wrapper.findAll('button').find(b => b.text() === 'Manual')
    await manualButton!.trigger('click')
    await flushPromises()

    await wrapper.find('input').setValue('my-stack')
    await wrapper.findAll('select')[0]!.setValue('repo-1')
    await flushPromises()

    await goToReviewStep(wrapper)

    const fileSelect = wrapper.findAll('select').find(s => s.findAll('option').some(o => o.text() === 'docker-compose.yml'))
    await fileSelect!.setValue('docker-compose.yml')
    await flushPromises()

    const nextButton = wrapper.findAll('button').find(b => b.text() === 'Next')
    expect(nextButton?.attributes('disabled')).toBeDefined()
  })
})
