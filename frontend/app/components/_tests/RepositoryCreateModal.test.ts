import { describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h } from 'vue'
import RepositoryCreateModal from '../RepositoryCreateModal.vue'
import GithubIcon from '../GithubIcon.vue'
import GenericIcon from '../GenericIcon.vue'

const UStepperStub = {
  props: ['modelValue', 'items'],
  emits: ['update:modelValue'],
  template: '<div class="stepper" />',
}

const stubs = {
  UModal: { template: '<div><slot name="content" /></div>' },
  UCard: { template: '<div><slot name="header" /><slot /><slot name="footer" /></div>' },
  UStepper: UStepperStub,
  UFormField: { props: ['label', 'error', 'required'], template: '<div><label>{{ label }}</label><slot /></div>' },
  AppTextInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    setup(props: { modelValue?: string }, { emit }: { emit: (e: 'update:modelValue', v: string) => void }) {
      return () => h('input', {
        value: props.modelValue,
        onInput: (event: Event) => emit('update:modelValue', (event.target as HTMLInputElement).value),
      })
    },
  },
  AppSelectInput: {
    props: ['modelValue', 'items', 'disabled', 'loading'],
    emits: ['update:modelValue'],
    template: `<select :value="modelValue" :disabled="disabled" @change="$emit('update:modelValue', $event.target.value)">
      <option v-for="i in items" :key="i.value" :value="i.value">{{ i.label }}</option>
    </select>`,
  },
  USelectMenu: { props: ['modelValue', 'items'], template: '<select />' },
  URadioGroup: { props: ['modelValue', 'items'], template: '<div />' },
  UCheckbox: { props: ['modelValue', 'label'], template: '<div />' },
  UButton: {
    props: ['label', 'disabled', 'loading'],
    template: '<button :disabled="disabled" v-bind="$attrs"><slot>{{ label }}</slot></button>',
  },
  UBadge: { template: '<span class="badge"><slot /></span>' },
  USkeleton: { template: '<div class="skeleton" />' },
  UIcon: { template: '<span />' },
  CancelButton: { template: '<button>Cancel</button>' },
  RepositoryKeyModal: true,
  GithubIcon,
  GenericIcon,
}

function setupGlobals(gitProviders: { slug: string, name: string, configured: boolean }[], repositoryKeys: any[] = []) {
  const listGitProviders = vi.fn().mockResolvedValue(gitProviders)
  const listGitProviderOrgs = vi.fn().mockResolvedValue([])
  const listGitProviderRepos = vi.fn().mockResolvedValue([])
  const listGitProviderBranches = vi.fn().mockResolvedValue([])
  const testCredentials = vi.fn()
  ;(globalThis as any).useApi = () => ({
    testCredentials,
    listGitProviders,
    listGitProviderOrgs,
    listGitProviderRepos,
    listGitProviderBranches,
  })
  ;(globalThis as any).useNuxtApp = () => ({
    $pb: {
      collection: () => ({
        getFullList: vi.fn().mockResolvedValue(repositoryKeys),
      }),
    },
  })
  ;(globalThis as any).useGitProviderOAuth = () => ({ connect: vi.fn() })
  ;(globalThis as any).useRepositoryPlatform = () => ({
    PLATFORM_OPTIONS: [{ label: 'GitHub', value: 'github' }],
    platformIconUrl: () => null,
  })
  ;(globalThis as any).useToast = () => ({ add: vi.fn() })
  ;(globalThis as any).useA11yAnnouncer = () => ({ announce: vi.fn() })

  return { listGitProviders, listGitProviderOrgs, listGitProviderRepos, listGitProviderBranches }
}

async function openModal() {
  const wrapper = mount(RepositoryCreateModal, {
    props: { open: false },
    global: { stubs },
  })
  await wrapper.setProps({ open: true })
  await flushPromises()
  return wrapper
}

describe('RepositoryCreateModal wizard', () => {
  it('filters the Source step cards to configured providers only', async () => {
    setupGlobals([
      { slug: 'github', name: 'GitHub', configured: true },
      { slug: 'gitea', name: 'Gitea', configured: false },
    ])

    const wrapper = await openModal()

    expect(wrapper.text()).toContain('GitHub')
    expect(wrapper.text()).not.toContain('Gitea')
    expect(wrapper.text()).toContain('Generic')
  })

  it('exposes Source/Details stepper items with Details disabled until a source is picked', async () => {
    setupGlobals([{ slug: 'github', name: 'GitHub', configured: true }])

    const wrapper = await openModal()
    const stepper = wrapper.findComponent(UStepperStub)

    expect(stepper.props('modelValue')).toBe(0)
    expect(stepper.props('items').map((i: any) => i.title)).toEqual(['Source', 'Details'])
    expect(stepper.props('items')[1].disabled).toBe(true)
  })

  it('moves to the Details step and enables it once a configured provider is selected', async () => {
    setupGlobals(
      [{ slug: 'github', name: 'GitHub', configured: true }],
      [{ id: 'key-1', auth_type: 'oauth_token', oauth_provider: 'github' }],
    )

    const wrapper = await openModal()

    // The Source and Details panels both stay mounted (v-show toggles a
    // display:none on the panel wrapper rather than v-if unmounting it),
    // so the panel's own inline style — not text presence — is what tells
    // them apart. happy-dom's getComputedStyle doesn't resolve inline
    // styles reliably here, so isVisible() can't be used; read the style
    // attribute directly instead.
    const sourcePrompt = wrapper.find('p')
    expect(sourcePrompt.text()).toBe('Where is this repository hosted?')
    const sourcePanel = sourcePrompt.element.parentElement as HTMLElement
    expect(sourcePanel.style.display).not.toBe('none')

    const githubCard = wrapper.findAll('button').find(b => b.text().includes('GitHub'))
    await githubCard!.trigger('click')
    await flushPromises()

    expect(sourcePanel.style.display).toBe('none')
    expect(wrapper.text()).toContain('GitHub')

    const stepper = wrapper.findComponent(UStepperStub)
    expect(stepper.props('modelValue')).toBe(1)
    expect(stepper.props('items')[1].disabled).toBe(false)
  })

  it('skips straight to the Details step when no SCM integration is configured', async () => {
    setupGlobals([])

    const wrapper = await openModal()

    const sourcePanel = wrapper.find('p').element.parentElement as HTMLElement
    expect(sourcePanel.style.display).toBe('none')
    expect(wrapper.findAll('label').some(l => l.text() === 'Name')).toBe(true)
  })
})
