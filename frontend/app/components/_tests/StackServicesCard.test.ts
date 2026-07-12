import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import StackServicesCard from '../StackServicesCard.vue'

const getStackResources = vi.fn()
vi.stubGlobal('useApi', () => ({ getStackResources }))

describe('StackServicesCard', () => {
  const stubs = {
    UCard: { template: '<div><slot /><slot name="header" /></div>' },
    UTooltip: { template: '<div><slot /></div>' },
    UButton: {
      template: '<button v-bind="$attrs" @click="$emit(\'click\')"><slot /><slot name="leading" /></button>',
      props: ['icon', 'variant', 'size', 'color'],
      emits: ['click'],
    },
    UIcon: { template: '<span />' },
    BadgeStatus: { template: '<span class="badge-status">{{ status }}</span>', props: ['status'] },
    ContainerIntegrationActions: {
      template: '<button class="show-logs" @click="$emit(\'show-logs\', containerId, containerName)">logs</button>',
      props: ['actions', 'containerId', 'containerName'],
      emits: ['show-logs'],
    },
    UBadge: { template: '<span>{{ label }}</span>', props: ['label'] },
  }

  it('groups containers by service name and emits actions', async () => {
    getStackResources.mockResolvedValue({
      volumes: [{ name: 'data', driver: 'local', mountpoint: '/var/lib/docker/volumes/data', scope: 'local' }],
      networks: [{ name: 'app-net', driver: 'bridge', scope: 'local', subnet: '172.20.0.0/16', gateway: '172.20.0.1' }],
    })

    const wrapper = mount(StackServicesCard, {
      props: {
        stackId: 'stack-1',
        services: [
          { service_name: 'api', container_id: 'abcdef1234567890', container_name: 'api-1', status: 'running' },
          { service_name: 'api', container_id: 'fedcba0987654321', container_name: 'api-2', status: 'exited' },
          { service_name: 'worker', container_id: '1122334455667788', container_name: 'worker-1', status: 'running' },
        ],
        containerStats: {
          abcdef1234567890: { cpu_percent: 12.34, mem_usage: 1024, mem_limit: 2048, started_at: new Date(Date.now() - 60000).toISOString() },
        },
        integrationActions: {},
      },
      global: { stubs },
    })

    await Promise.resolve()
    await Promise.resolve()

    expect(wrapper.text()).toContain('api')
    expect(wrapper.text()).toContain('worker')
    expect(wrapper.text()).toContain('abcdef123456')
    expect(wrapper.text()).toContain('fedcba098765')
    expect(wrapper.text()).toContain('Volumes')
    expect(wrapper.text()).toContain('Networks')
    expect(wrapper.text()).toContain('data')
    expect(wrapper.text()).toContain('app-net')
    expect(getStackResources).toHaveBeenCalledWith('stack-1')

    await wrapper.find('button[title="Stop"]').trigger('click')
    expect(wrapper.emitted('container-action')?.[0]).toEqual([
      { containerId: 'abcdef1234567890', containerName: 'api-1', action: 'stop' },
    ])

    await wrapper.find('.show-logs').trigger('click')
    expect(wrapper.emitted('show-logs')?.[0]).toEqual(['abcdef1234567890', 'api-1'])

    await wrapper.find('button[title="Copy container ID"]').trigger('click')
    expect(wrapper.emitted('copy-container-id')?.[0]).toEqual(['abcdef1234567890'])
  })

  it('formats and displays published ports once the container row is expanded', async () => {
    getStackResources.mockResolvedValue({ volumes: [], networks: [] })

    const wrapper = mount(StackServicesCard, {
      props: {
        stackId: 'stack-1',
        services: [
          {
            service_name: 'web',
            container_id: 'abcdef1234567890',
            container_name: 'web-1',
            status: 'running',
            ports: [
              { container_port: 80, protocol: 'tcp', host_ip: '127.0.0.1', host_port: 8080 },
              { container_port: 80, protocol: 'tcp', host_ip: '::1', host_port: 8443 },
              { container_port: 53, protocol: 'udp', host_ip: '0.0.0.0', host_port: 53 },
              { container_port: 443, protocol: 'tcp' },
            ],
          },
        ],
        containerStats: {},
        integrationActions: {},
      },
      global: { stubs },
    })

    await Promise.resolve()
    await Promise.resolve()
    await wrapper.findAll('button')[0].trigger('click')
    await Promise.resolve()

    const text = wrapper.text()
    expect(text).toContain('127.0.0.1:8080')
    expect(text).toContain('[::1]:8443')
    expect(text).toContain('53/udp')
    expect(text).not.toContain('0.0.0.0')
    expect(text).toContain('443/tcp')
    expect(text).toContain('80/tcp')
    // unpublished port (443/tcp) renders a host badge showing the '-' placeholder
    const hostBadges = wrapper.findAll('span').filter(span => span.text() === '-')
    expect(hostBadges.length).toBe(1)
  })

  it('renders no port badges for a container with no ports', async () => {
    getStackResources.mockResolvedValue({ volumes: [], networks: [] })

    const wrapper = mount(StackServicesCard, {
      props: {
        stackId: 'stack-1',
        services: [
          { service_name: 'web', container_id: 'abcdef1234567890', container_name: 'web-1', status: 'running' },
        ],
        containerStats: {},
        integrationActions: {},
      },
      global: { stubs },
    })

    await Promise.resolve()
    await Promise.resolve()
    await wrapper.findAll('button')[0].trigger('click')
    await Promise.resolve()

    expect(wrapper.text()).not.toContain('->')
    expect(wrapper.text()).not.toContain('/tcp')
    expect(wrapper.text()).not.toContain('/udp')
  })
})
