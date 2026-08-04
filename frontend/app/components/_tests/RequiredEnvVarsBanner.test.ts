import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h } from 'vue'
import RequiredEnvVarsBanner from '../RequiredEnvVarsBanner.vue'

const stubs = {
  UIcon: { setup: () => () => h('span') },
  UButton: {
    props: ['label'],
    emits: ['click'],
    setup(props: { label?: string }, { emit }: { emit: (e: string) => void }) {
      return () => h('button', { onClick: () => emit('click') }, props.label)
    },
  },
}

describe('RequiredEnvVarsBanner', () => {
  const copyFn = vi.fn()

  beforeEach(() => {
    copyFn.mockClear()
    ;(globalThis as any).useCopy = () => ({ copy: copyFn })
  })

  it('renders the missing variables when the lint report has an unresolved-variable finding', async () => {
    const lintCompose = vi.fn().mockResolvedValue({
      report: {
        findings: [
          { rule: 'compose/unresolved-variable', title: 'x', severity: 'warning', message: 'x', vars: ['FOO', 'BAR'] },
        ],
        errors: 0,
        warnings: 1,
        infos: 0,
      },
    })
    ;(globalThis as any).useApi = () => ({ lintCompose })

    const wrapper = mount(RequiredEnvVarsBanner, {
      props: { stackId: 's1', repository: 'r1', composePath: '', composeFile: '', envKeys: [] },
      global: { stubs },
    })
    await flushPromises()

    expect(lintCompose).toHaveBeenCalledWith({ repository: 'r1', compose_path: '', compose_file: '', stack: 's1' })
    expect(wrapper.text()).toContain('BAR')
    expect(wrapper.text()).toContain('FOO')
  })

  it('renders nothing when there are no unresolved-variable findings', async () => {
    const lintCompose = vi.fn().mockResolvedValue({ report: { findings: [], errors: 0, warnings: 0, infos: 0 } })
    ;(globalThis as any).useApi = () => ({ lintCompose })

    const wrapper = mount(RequiredEnvVarsBanner, {
      props: { stackId: 's1', repository: 'r1', composePath: '', composeFile: '', envKeys: [] },
      global: { stubs },
    })
    await flushPromises()

    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('stays silent when the lint request fails', async () => {
    const lintCompose = vi.fn().mockRejectedValue(new Error('boom'))
    ;(globalThis as any).useApi = () => ({ lintCompose })

    const wrapper = mount(RequiredEnvVarsBanner, {
      props: { stackId: 's1', repository: 'r1', composePath: '', composeFile: '', envKeys: [] },
      global: { stubs },
    })
    await flushPromises()

    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it('copies missing keys as KEY= lines when the copy button is clicked', async () => {
    const lintCompose = vi.fn().mockResolvedValue({
      report: {
        findings: [{ rule: 'compose/unresolved-variable', title: 'x', severity: 'warning', message: 'x', vars: ['FOO'] }],
        errors: 0,
        warnings: 1,
        infos: 0,
      },
    })
    ;(globalThis as any).useApi = () => ({ lintCompose })

    const wrapper = mount(RequiredEnvVarsBanner, {
      props: { stackId: 's1', repository: 'r1', composePath: '', composeFile: '', envKeys: [] },
      global: { stubs },
    })
    await flushPromises()

    await wrapper.find('button').trigger('click')
    expect(copyFn).toHaveBeenCalledWith('FOO=', 'Missing variables')
  })
})
