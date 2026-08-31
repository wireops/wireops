import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h, reactive } from 'vue'
import JobCreateModal from '../JobCreateModal.vue'

function setupGlobals() {
  const queryState = reactive<{ query: Record<string, any> }>({ query: {} })
  const push = vi.fn(({ query }: any) => { queryState.query = { ...query } })
  const replace = vi.fn(({ query }: any) => { queryState.query = { ...query } })

  ;(globalThis as any).useRoute = () => queryState
  ;(globalThis as any).useRouter = () => ({ push, replace })

  const createJob = vi.fn().mockResolvedValue({ id: 'job-1' })
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: (name: string) => {
        if (name === 'scheduled_jobs') return { create: createJob }
        return {}
      },
    },
  })

  const getJobFiles = vi.fn().mockResolvedValue(['job.yaml'])
  const getJobDefinitionFromFile = vi.fn().mockResolvedValue({ name: 'nightly-backup', description: 'Backs up the db' })
  const customPost = vi.fn().mockResolvedValue({})
  ;(globalThis as any).useApi = () => ({
    getJobFiles,
    getJobDefinitionFromFile,
    customPost,
  })

  const toastAdd = vi.fn()
  ;(globalThis as any).useToast = () => ({ add: toastAdd })

  return { createJob, getJobFiles, getJobDefinitionFromFile, customPost, toastAdd }
}

const stubs = {
  UModal: { template: '<div><slot name="content" /></div>' },
  AppPanelCard: { template: '<div><slot name="header" /><slot /><slot name="footer" /></div>' },
  UFormField: { props: ['label', 'error', 'required'], template: '<div><label>{{ label }}</label><slot /><div class="field-error">{{ error }}</div></div>' },
  AppSelectInput: {
    props: ['modelValue', 'items', 'disabled'],
    emits: ['update:modelValue'],
    template: `<select :value="modelValue" :disabled="disabled" @change="$emit('update:modelValue', $event.target.value)">
      <option v-for="i in items" :key="i.value" :value="i.value">{{ i.label }}</option>
    </select>`,
  },
  USwitch: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: boolean }, { emit }: { emit: (e: 'update:modelValue', value: boolean) => void }) {
      return () => h('input', {
        type: 'checkbox',
        checked: props.modelValue,
        onChange: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).checked),
      })
    },
  },
  UButton: {
    props: ['label', 'disabled', 'loading'],
    template: '<button :disabled="disabled" v-bind="$attrs"><slot>{{ label }}</slot></button>',
  },
  UIcon: { template: '<span />' },
  CancelButton: {
    emits: ['click'],
    setup(_props: unknown, { emit }: { emit: (e: 'click') => void }) {
      return () => h('button', { type: 'button', onClick: () => emit('click') }, 'Cancel')
    },
  },
  // The pending env vars editor is exercised by its own component tests —
  // here it's just a v-model passthrough so tests can push rows into it.
  EnvironmentVariablesPendingEditor: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: unknown[] }, { emit, expose }: { emit: (event: 'update:modelValue', value: unknown[]) => void, expose: (exposed: Record<string, unknown>) => void }) {
      // Mimics the real component's separation between a typed draft row
      // and committed rows: `type-draft-env-var` only fills the draft, and
      // `commitDraft` (exposed, called by the parent before Next/Submit)
      // is what actually pushes it into the model.
      const draft = { key: '', value: '' }
      function commitDraft() {
        if (!draft.key || !draft.value) return
        emit('update:modelValue', [...(props.modelValue || []), { key: draft.key, value: draft.value, secret: false, secret_provider: '' }])
        draft.key = ''
        draft.value = ''
      }
      expose({ commitDraft })
      return () => h('div', { class: 'env-vars-pending-editor' }, [
        h('button', {
          type: 'button',
          class: 'add-fake-env-var',
          onClick: () => emit('update:modelValue', [...(props.modelValue || []), { key: 'FOO', value: 'bar', secret: false, secret_provider: '' }]),
        }, 'Add fake env var'),
        h('button', {
          type: 'button',
          class: 'type-draft-env-var',
          onClick: () => { draft.key = 'DRAFT'; draft.value = 'uncommitted' },
        }, 'Type draft env var'),
      ])
    },
  },
}

async function openModal() {
  const wrapper = mount(JobCreateModal, {
    props: { repos: [{ id: 'repo-1', name: 'repo', git_url: 'git@x' }], open: false },
    global: { stubs },
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

async function clickNext(wrapper: any) {
  const nextButton = wrapper.findAll('button').find((b: any) => b.text() === 'Next')
  await nextButton!.trigger('click')
  await flushPromises()
}

// Selects a repository (step 1) and a job file (step 2), landing on the
// Environment Variables step with a valid, submittable form.
async function fillJobThroughConfiguration(wrapper: any) {
  await wrapper.find('select').setValue('repo-1')
  await flushPromises()
  await clickNext(wrapper)

  const fileSelect = wrapper.findAll('select').find((s: any) => s.findAll('option').some((o: any) => o.text() === 'job.yaml'))
  await fileSelect!.setValue('job.yaml')
  await flushPromises()

  await clickNext(wrapper)
}

describe('JobCreateModal', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('autofills name and description from the parsed job.yaml', async () => {
    const { getJobDefinitionFromFile } = setupGlobals()
    const wrapper = await openModal()

    await wrapper.find('select').setValue('repo-1')
    await flushPromises()
    await clickNext(wrapper)

    const fileSelect = wrapper.findAll('select').find((s: any) => s.findAll('option').some((o: any) => o.text() === 'job.yaml'))
    await fileSelect!.setValue('job.yaml')
    await flushPromises()

    expect(getJobDefinitionFromFile).toHaveBeenCalledWith('repo-1', 'job.yaml')
    expect(wrapper.text()).toContain('nightly-backup')
    expect(wrapper.text()).toContain('Backs up the db')
  })

  it('blocks Next on the Configuration step until a job file is selected', async () => {
    setupGlobals()
    const wrapper = await openModal()

    await wrapper.find('select').setValue('repo-1')
    await flushPromises()
    await clickNext(wrapper)

    const nextButton = wrapper.findAll('button').find((b: any) => b.text() === 'Next')
    expect(nextButton?.attributes('disabled')).toBeDefined()
  })

  it('creates a job through all three steps and never touches the env-vars endpoint when none were added', async () => {
    const { createJob, customPost } = setupGlobals()
    const wrapper = await openModal()

    await fillJobThroughConfiguration(wrapper)

    expect(wrapper.find('.env-vars-pending-editor').exists()).toBe(true)

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(createJob).toHaveBeenCalledWith(expect.objectContaining({
      repository: 'repo-1',
      job_file: 'job.yaml',
      name: 'nightly-backup',
      status: 'active',
    }))
    expect(customPost).not.toHaveBeenCalled()
  })

  it('saves pending env vars via the bulk endpoint after creating the job', async () => {
    const { createJob, customPost } = setupGlobals()
    const wrapper = await openModal()

    await fillJobThroughConfiguration(wrapper)
    await wrapper.find('.add-fake-env-var').trigger('click')
    await flushPromises()

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(createJob).toHaveBeenCalled()
    expect(customPost).toHaveBeenCalledWith('/api/custom/jobs/job-1/env-vars/bulk', {
      mode: 'replace',
      vars: [{ key: 'FOO', value: 'bar', secret: false, secret_provider: '' }],
    })
  })

  it('persists a typed-but-not-added env var row when Create is clicked without pressing the row add button', async () => {
    const { createJob, customPost } = setupGlobals()
    const wrapper = await openModal()

    await fillJobThroughConfiguration(wrapper)
    await wrapper.find('.type-draft-env-var').trigger('click')

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(createJob).toHaveBeenCalled()
    expect(customPost).toHaveBeenCalledWith('/api/custom/jobs/job-1/env-vars/bulk', {
      mode: 'replace',
      vars: [{ key: 'DRAFT', value: 'uncommitted', secret: false, secret_provider: '' }],
    })
  })

  it('still creates the job and warns instead of failing when the env-vars save fails', async () => {
    const { createJob, customPost, toastAdd } = setupGlobals()
    customPost.mockRejectedValueOnce(new Error('boom'))
    const wrapper = await openModal()

    await fillJobThroughConfiguration(wrapper)
    await wrapper.find('.add-fake-env-var').trigger('click')
    await flushPromises()

    await wrapper.find('form').trigger('submit.prevent')
    await flushPromises()

    expect(createJob).toHaveBeenCalled()
    expect(toastAdd).toHaveBeenCalledWith(expect.objectContaining({ color: 'warning' }))
    expect(wrapper.emitted('created')).toBeTruthy()
  })
})
