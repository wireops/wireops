import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ContainerLogsSlideover from '../ContainerLogsSlideover.vue'

const getContainerLogs = vi.fn()
const toastAdd = vi.fn()

function mountViewer() {
  return mount(ContainerLogsSlideover, {
    props: {
      open: true,
      stackId: 'stack-1',
      containerId: 'container-1',
      containerName: 'api/one',
    },
    global: {
      stubs: {
        USlideover: { template: '<section><slot name="content" /></section>' },
        UTooltip: { template: '<span><slot /></span>' },
        UButton: { template: '<button v-bind="$attrs" @click="$emit(\'click\')"><slot /></button>' },
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
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:logs')
  })
})

