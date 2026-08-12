import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { h } from 'vue'
import ContainerLogsSlideover from '../ContainerLogsSlideover.vue'

const getContainerLogs = vi.fn()
const toastAdd = vi.fn()

function mountViewer(props: Partial<{ containerId: string, containerName: string }> = {}) {
  return mount(ContainerLogsSlideover, {
    props: {
      open: true,
      stackId: 'stack-1',
      containerId: 'container-1',
      containerName: 'api/one',
      ...props,
    },
    global: {
      stubs: {
        USlideover: {
          setup(_: unknown, { slots }: any) {
            return () => h('section', slots.content?.())
          },
        },
        UTooltip: {
          setup(_: unknown, { slots }: any) {
            return () => h('span', slots.default?.())
          },
        },
        UButton: {
          inheritAttrs: false,
          props: ['icon', 'label', 'disabled', 'loading'],
          emits: ['click'],
          setup(props: any, { attrs, slots, emit }: any) {
            return () => h('button', {
              ...attrs,
              disabled: props.disabled,
              onClick: () => emit('click'),
            }, [props.label, slots.default?.()])
          },
        },
        UIcon: true,
        CloseButton: true,
        AppTextInput: true,
        AppSelectInput: true,
      },
    },
  })
}

describe('ContainerLogsSlideover', () => {
  beforeEach(() => {
    vi.stubGlobal('useApi', () => ({ getContainerLogs }))
    vi.stubGlobal('useToast', () => ({ add: toastAdd }))
    getContainerLogs.mockResolvedValue({
      logs: '2026-08-12T10:00:00Z INFO ready\n2026-08-12T10:00:01Z ERROR failed\n',
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads the selected container and renders syntax-highlighted lines', async () => {
    const wrapper = mountViewer()
    await flushPromises()

    expect(getContainerLogs).toHaveBeenCalledWith('stack-1', 'container-1', 200)
    expect(wrapper.text()).toContain('INFO ready')
    expect(wrapper.text()).toContain('ERROR failed')
    expect(wrapper.find('.text-red-400').text()).toBe('ERROR')
  })

  it('filters case-insensitively while preserving the original line number', async () => {
    const wrapper = mountViewer()
    await flushPromises()

    ;(wrapper.vm as any).query = 'error'
    await flushPromises()

    expect(wrapper.text()).not.toContain('INFO ready')
    expect(wrapper.text()).toContain('ERROR failed')
    expect(wrapper.text()).toContain('1 of 2 lines')
    expect(wrapper.find('[role="log"]').text()).toMatch(/^2/)
    expect(wrapper.find('mark').text()).toBe('ERROR')
  })

  it('downloads the unfiltered logs using a safe filename', async () => {
    vi.useFakeTimers()
    const wrapper = mountViewer()
    await flushPromises()

    const createObjectURL = vi.fn().mockReturnValue('blob:logs')
    const revokeObjectURL = vi.fn()
    vi.stubGlobal('URL', { createObjectURL, revokeObjectURL })
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    ;(wrapper.vm as any).query = 'error'
    ;(wrapper.vm as any).downloadLogs()

    const blob = createObjectURL.mock.calls[0]?.[0] as Blob
    expect(await blob.text()).toContain('INFO ready')
    expect(click).toHaveBeenCalledOnce()
    expect(toastAdd).toHaveBeenCalledWith({ title: 'Logs downloaded', color: 'success' })
    expect(revokeObjectURL).not.toHaveBeenCalled()

    vi.runAllTimers()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:logs')
    vi.useRealTimers()
  })

  it('shows an error message when the logs request fails', async () => {
    getContainerLogs.mockReset()
    getContainerLogs.mockRejectedValue(new Error('worker offline'))

    const wrapper = mountViewer()
    await flushPromises()

    expect(wrapper.text()).toContain('worker offline')
    expect(wrapper.find('[role="log"]').exists()).toBe(false)
  })

  it('shows an empty-log message when the container has no logs', async () => {
    getContainerLogs.mockReset()
    getContainerLogs.mockResolvedValue({ logs: '' })

    const wrapper = mountViewer()
    await flushPromises()

    expect(wrapper.text()).toContain('This container has no logs yet.')
    expect(wrapper.find('[role="log"]').exists()).toBe(false)
  })

  it('clears stale content when switching to a different container', async () => {
    const wrapper = mountViewer()
    await flushPromises()
    expect(wrapper.text()).toContain('INFO ready')

    let resolveNext: (value: { logs: string }) => void = () => {}
    getContainerLogs.mockReset()
    getContainerLogs.mockReturnValue(new Promise((resolve) => { resolveNext = resolve }))

    await wrapper.setProps({ containerId: 'container-2' })

    expect(wrapper.text()).not.toContain('INFO ready')

    resolveNext({ logs: '2026-08-12T10:00:02Z INFO other\n' })
    await flushPromises()
    expect(wrapper.text()).toContain('INFO other')
  })
})

