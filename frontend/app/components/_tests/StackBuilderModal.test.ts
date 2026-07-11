import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import StackBuilderModal from '../StackBuilderModal.vue'

vi.stubGlobal('useToast', () => ({
  add: vi.fn(),
}))

const stubs = {
  UModal: {
    props: ['open'],
    template: '<div v-if="open"><slot name="content" /></div>',
  },
  UCard: {
    template: '<section><slot name="header" /><slot /><slot name="footer" /></section>',
  },
  UFormField: {
    props: ['label'],
    template: '<label><slot name="label">{{ label }}</slot><slot /></label>',
  },
  UInput: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
  },
  USelect: {
    props: ['modelValue', 'items'],
    emits: ['update:modelValue'],
    template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="item in items" :key="item.value" :value="item.value">{{ item.label }}</option></select>',
  },
  USwitch: {
    props: ['modelValue'],
    emits: ['update:modelValue'],
    template: '<input type="checkbox" :checked="modelValue" @change="$emit(\'update:modelValue\', $event.target.checked)" />',
  },
  UButton: {
    props: ['label'],
    emits: ['click'],
    template: '<button type="button" v-bind="$attrs" @click="$emit(\'click\')">{{ label }}<slot /></button>',
  },
  UTooltip: { template: '<span><slot /><slot name="content" /></span>' },
  UIcon: { template: '<span />' },
  YamlHighlighter: {
    props: ['code'],
    template: '<pre data-testid="yaml-preview">{{ code }}</pre>',
  },
}

describe('StackBuilderModal', () => {
  it('renders YAML preview generated from the default builder state', () => {
    const wrapper = mount(StackBuilderModal, {
      props: { open: true },
      global: { stubs },
    })

    const preview = wrapper.get('[data-testid="yaml-preview"]').text()

    expect(preview).toContain('version: "wireops.v1"')
    expect(preview).toContain('name: "my-stack"')
    expect(preview).toContain('compose:')
    expect(preview).toContain('remove_orphans: true')
    expect(preview).toContain('force_pull: false')
    expect(preview).toContain('jobs:')
    expect(preview).toContain('wait_running: false')
  })

  it('lists active workers with their tags and adds a tag on click', async () => {
    const wrapper = mount(StackBuilderModal, {
      props: {
        open: true,
        workers: [
          { id: '1', hostname: 'worker-gpu', status: 'ACTIVE', tags: ['gpu', 'us-east'] },
          { id: '2', hostname: 'worker-offline', status: 'OFFLINE', tags: ['legacy'] },
        ],
      },
      global: { stubs },
    })

    expect(wrapper.text()).toContain('worker-gpu')
    expect(wrapper.text()).not.toContain('worker-offline')

    const tagButton = wrapper.findAll('button').find(b => b.text() === 'gpu')
    expect(tagButton).toBeTruthy()
    await tagButton!.trigger('click')

    const preview = wrapper.get('[data-testid="yaml-preview"]').text()
    expect(preview).toContain('worker:')
    expect(preview).toContain('- "gpu"')
  })
})
