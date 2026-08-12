import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import StackServicesCard from '../StackServicesCard.vue'

const getStackResources = vi.fn()
vi.stubGlobal('useApi', () => ({ getStackResources }))

describe('StackServicesCard', () => {
  const stubs = {
    AppPanelCard: {
      setup(_: unknown, { slots }: any) {
        return () => h('div', [slots.header?.(), slots.default?.()])
      },
    },
    UCard: {
      setup(_: unknown, { slots }: any) {
        return () => h('div', [slots.header?.(), slots.default?.()])
      },
    },
    UTooltip: {
      setup(_: unknown, { slots }: any) {
        return () => h('div', slots.default?.())
      },
    },
    UButton: {
      inheritAttrs: false,
      props: ['icon', 'variant', 'size', 'color', 'label', 'disabled'],
      emits: ['click'],
      setup(props: any, { attrs, slots, emit }: any) {
        return () => h('button', {
          ...attrs,
          disabled: props.disabled,
          onClick: () => emit('click'),
        }, [props.label, slots.default?.(), slots.leading?.()])
      },
    },
    UIcon: {
      setup() {
        return () => h('span')
      },
    },
    BadgeStatus: {
      props: ['status'],
      setup(props: any) {
        return () => h('span', { class: 'badge-status' }, props.status)
      },
    },
    ContainerIntegrationActions: {
      props: ['actions', 'containerId', 'containerName'],
      emits: ['show-logs'],
      setup(props: any, { emit }: any) {
        return () => h('button', {
          class: 'show-logs',
          onClick: () => emit('show-logs', props.containerId, props.containerName),
        }, 'logs')
      },
    },
    UBadge: {
      props: ['label'],
      setup(props: any) {
        return () => h('span', props.label)
      },
    },
  }

  it('groups containers by service name and emits actions', async () => {
    getStackResources.mockResolvedValue({
      volumes: [{
        name: 'data', docker_name: 'stack_data', driver: 'local', mountpoint: '/var/lib/docker/volumes/data', scope: 'local',
        created_at: '2026-08-12T12:00:00Z', size_bytes: 1536, options: { type: 'none' },
      }],
      networks: [{
        name: 'app-net', docker_name: 'stack_app-net', id: 'network-id', driver: 'bridge', scope: 'local', created_at: '2026-08-12T12:00:00Z',
        subnet: '172.20.0.0/16', gateway: '172.20.0.1', enable_ipv4: true, enable_ipv6: false, internal: false, attachable: true, ingress: false, config_only: false,
        ipam_configs: [{ subnet: '172.20.0.0/16', gateway: '172.20.0.1', ip_range: '172.20.0.0/24' }], options: { 'com.docker.network.bridge.enable_icc': 'true' },
      }],
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
    expect(wrapper.text()).toContain('1.5 KB')
    expect(wrapper.text()).toContain('app-net')
    expect(getStackResources).toHaveBeenCalledWith('stack-1')

    await wrapper.get('[data-testid="volume-card-data"]').trigger('click')
    expect(wrapper.text()).toContain('Docker name')

    await wrapper.get('[data-testid="network-card-app-net"]').trigger('click')
    expect(wrapper.text()).toContain('Network ID')
    expect(wrapper.text()).toContain('IPAM pools')

    await wrapper.find('button[title="Stop"]').trigger('click')
    expect(wrapper.emitted('container-action')?.[0]).toEqual([
      { containerId: 'abcdef1234567890', containerName: 'api-1', action: 'stop' },
    ])

    await wrapper.find('.show-logs').trigger('click')
    expect(wrapper.emitted('show-logs')?.[0]).toEqual(['abcdef1234567890', 'api-1'])

    await wrapper.find('button[title="Copy container ID"]').trigger('click')
    expect(wrapper.emitted('copy-container-id')?.[0]).toEqual(['abcdef1234567890'])

    await wrapper.get('[data-testid="restart-service-api"]').trigger('click')
    expect(wrapper.emitted('bulk-container-action')?.[0]).toEqual([{
      action: 'restart',
      subject: 'all containers in api',
      containers: [
        { containerId: 'abcdef1234567890', containerName: 'api-1' },
        { containerId: 'fedcba0987654321', containerName: 'api-2' },
      ],
    }])

    await wrapper.get('[data-testid="stop-service-api"]').trigger('click')
    expect(wrapper.emitted('bulk-container-action')?.[1]).toEqual([{
      action: 'stop',
      subject: 'all containers in api',
      containers: [
        { containerId: 'abcdef1234567890', containerName: 'api-1' },
      ],
    }])
  })

  it('runs bulk actions for an arbitrary container selection', async () => {
    getStackResources.mockResolvedValue({ volumes: [], networks: [] })

    const wrapper = mount(StackServicesCard, {
      props: {
        stackId: 'stack-1',
        services: [
          { service_name: 'api', container_id: 'api-running', container_name: 'api-1', status: 'running' },
          { service_name: 'api', container_id: 'api-exited', container_name: 'api-2', status: 'exited' },
          { service_name: 'worker', container_id: 'worker-running', container_name: 'worker-1', status: 'running' },
        ],
        containerStats: {},
        integrationActions: {},
      },
      global: { stubs },
    })

    await wrapper.get('[data-testid="select-container-api-running"]').setValue(true)
    await wrapper.get('[data-testid="select-container-api-exited"]').setValue(true)

    expect(wrapper.get('[data-testid="container-selection-toolbar"]').text()).toContain('2 containers selected')

    await wrapper.get('[data-testid="restart-selected"]').trigger('click')
    expect(wrapper.emitted('bulk-container-action')?.[0]).toEqual([{
      action: 'restart',
      subject: 'selected containers',
      containers: [
        { containerId: 'api-running', containerName: 'api-1' },
        { containerId: 'api-exited', containerName: 'api-2' },
      ],
    }])

    await wrapper.get('[data-testid="stop-selected"]').trigger('click')
    expect(wrapper.emitted('bulk-container-action')?.[1]).toEqual([{
      action: 'stop',
      subject: 'selected containers',
      containers: [
        { containerId: 'api-running', containerName: 'api-1' },
      ],
    }])
  })

  it('hides operational controls from users without operate permission', async () => {
    getStackResources.mockResolvedValue({ volumes: [], networks: [] })

    const wrapper = mount(StackServicesCard, {
      props: {
        stackId: 'stack-1',
        services: [
          { service_name: 'api', container_id: 'api-running', container_name: 'api-1', status: 'running' },
        ],
        containerStats: {},
        integrationActions: {},
        canOperate: false,
      },
      global: { stubs },
    })

    expect(wrapper.find('[data-testid="restart-service-api"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="select-container-api-running"]').exists()).toBe(false)
    expect(wrapper.find('button[title="Stop"]').exists()).toBe(false)
    expect(wrapper.find('button[title="Restart"]').exists()).toBe(false)
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
    await wrapper.get('[role="button"][aria-expanded]').trigger('click')
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
    await wrapper.get('[role="button"][aria-expanded]').trigger('click')
    await Promise.resolve()

    expect(wrapper.text()).not.toContain('->')
    expect(wrapper.text()).not.toContain('/tcp')
    expect(wrapper.text()).not.toContain('/udp')
  })
})
