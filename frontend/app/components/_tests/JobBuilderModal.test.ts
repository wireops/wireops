import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import JobBuilderModal from '../JobBuilderModal.vue'

vi.stubGlobal('useToast', () => ({
  add: vi.fn(),
}))

vi.stubGlobal('translateCron', (cron: string) => `cron: ${cron}`)

vi.stubGlobal('parseJobYaml', vi.fn())

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

describe('JobBuilderModal', () => {
  it('renders YAML preview generated from the default builder state', () => {
    const wrapper = mount(JobBuilderModal, {
      props: { open: true },
      global: { stubs },
    })

    const preview = wrapper.get('[data-testid="yaml-preview"]').text()

    expect(preview).toContain('name: "my-scheduled-job"')
    expect(preview).toContain('description: "A brief description of what this job does"')
    expect(preview).toContain('cron: "*/5 * * * *"')
    expect(preview).toContain('  - "production"')
    expect(preview).toContain('  - "cleanup"')
    expect(preview).toContain('image: "ubuntu:latest"')
    expect(preview).toContain('command: "echo \\"hello from wireops\\""')
    expect(preview).toContain('  - "/var/log:/app/logs"')
    expect(preview).toContain('  cpu: "0.5"')
    expect(preview).toContain('  memory: "512m"')
    expect(preview).toContain('  timeout: "5m"')
  })
})
