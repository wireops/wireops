<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import ContainerIcon from './ContainerIcon.vue'
import type { IntegrationAction } from '~/composables/useIntegrations'

type PortInfo = { container_port: number, protocol: string, host_ip?: string, host_port?: number }

interface ServiceContainer {
  service_name: string
  container_id: string
  container_name?: string
  status: string
  ports?: PortInfo[]
}

interface ContainerStats {
  cpu_percent?: number
  mem_usage?: number
  mem_limit?: number
  started_at?: string
}

type VolumeInfo = {
  name: string; docker_name: string; driver: string; mountpoint: string; scope: string
  created_at?: string; size_bytes?: number; options?: Record<string, string>
}
type NetworkIPAMConfig = {
  subnet?: string; gateway?: string; ip_range?: string; aux_addresses?: Record<string, string>
}
type NetworkInfo = {
  name: string; docker_name: string; id?: string; driver: string; scope: string; created_at?: string
  subnet?: string; gateway?: string; ipam_configs?: NetworkIPAMConfig[]
  enable_ipv4: boolean; enable_ipv6: boolean; internal: boolean; attachable: boolean; ingress: boolean; config_only: boolean
  options?: Record<string, string>
}

interface ContainerInfo {
  name: string
  is_fallback: boolean
  slug?: string
}

type BulkContainerAction = 'stop' | 'restart'
type BulkContainerTarget = { containerId: string, containerName: string }

const props = withDefaults(defineProps<{
  stackId: string
  services: ServiceContainer[]
  containerStats: Record<string, ContainerStats>
  integrationActions: Record<string, IntegrationAction[]>
  containersList?: ContainerInfo[]
  canOperate?: boolean
  actionsDisabled?: boolean
}>(), {
  containersList: () => [],
  canOperate: true,
  actionsDisabled: false,
})

const emit = defineEmits<{
  (e: 'refresh'): void
  (e: 'copy-container-id', containerId: string): void
  (e: 'show-logs', containerId: string, containerName: string): void
  (e: 'container-action', payload: { containerId: string, containerName: string, action: 'stop' | 'restart' }): void
  (e: 'bulk-container-action', payload: { containers: BulkContainerTarget[], action: BulkContainerAction, subject: string }): void
  (e: 'open-terminal', payload: { containerId: string, containerName: string }): void
}>()

const { getStackResources } = useApi()

function formatUptime(startedAt?: string): string {
  if (!startedAt) return '-'
  const start = new Date(startedAt).getTime()
  const now = Date.now()
  const diff = Math.floor((now - start) / 1000)
  if (diff < 0) return '-'
  const days = Math.floor(diff / 86400)
  const hours = Math.floor((diff % 86400) / 3600)
  const mins = Math.floor((diff % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

function formatContainerPort(port: PortInfo): string {
  const proto = (port.protocol || 'tcp').toLowerCase()
  return `${port.container_port}/${proto}`
}

function formatHostPort(port: PortInfo): string {
  if (!port.host_port) return '-'
  const wildcard = !port.host_ip || port.host_ip === '0.0.0.0' || port.host_ip === '::'
  const ip = wildcard ? '' : (port.host_ip!.includes(':') ? `[${port.host_ip}]` : port.host_ip)
  return ip ? `${ip}:${port.host_port}` : `${port.host_port}`
}

function formatBytes(bytes?: number): string {
  if (bytes == null || isNaN(bytes)) return '-'
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
}

function formatMemPercent(usage?: number, limit?: number): string {
  if (usage == null || limit == null || limit === 0) return '-'
  return `${(usage / limit * 100).toFixed(2)}%`
}

function formatCreatedAt(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function recordEntries(value?: Record<string, string>): [string, string][] {
  return Object.entries(value ?? {}).sort(([a], [b]) => a.localeCompare(b))
}

function hasVolumeDetails(volume: VolumeInfo): boolean {
  return Boolean(
    volume.created_at
    || (volume.docker_name && volume.docker_name !== volume.name)
    || recordEntries(volume.options).length
  )
}

function hasNetworkDetails(network: NetworkInfo): boolean {
  return Boolean(
    network.id
    || network.created_at
    || (network.docker_name && network.docker_name !== network.name)
    || network.ipam_configs?.length
    || recordEntries(network.options).length
  )
}

const getContainerSlug = (container: ServiceContainer) => {
  if (!props.containersList) return undefined
  const match = props.containersList.find(
    c => c.name === container.service_name || c.name === container.container_name
  )
  return match?.slug
}

function getServiceSlug(containers: ServiceContainer[]) {
  return containers[0] ? getContainerSlug(containers[0]) : undefined
}

const serviceTree = computed(() => {
  const map: Record<string, ServiceContainer[]> = {}
  for (const service of props.services || []) {
    if (!map[service.service_name]) map[service.service_name] = []
    map[service.service_name]?.push(service)
  }
  return Object.entries(map).map(([name, containers]) => ({ name, containers }))
})

const selectedContainerIds = ref<Set<string>>(new Set())

function actionTargets(containers: ServiceContainer[], action: BulkContainerAction): BulkContainerTarget[] {
  const seen = new Set<string>()
  return containers
    .filter(container => container.container_id && (action !== 'stop' || container.status === 'running'))
    .filter((container) => {
      if (seen.has(container.container_id)) return false
      seen.add(container.container_id)
      return true
    })
    .map(container => ({
      containerId: container.container_id,
      containerName: container.container_name || container.container_id,
    }))
}

const selectedContainers = computed(() =>
  (props.services || []).filter(container => selectedContainerIds.value.has(container.container_id))
)
const selectedCount = computed(() => selectedContainers.value.length)
const selectedRunningCount = computed(() =>
  selectedContainers.value.filter(container => container.status === 'running').length
)

function isContainerSelected(containerId: string): boolean {
  return selectedContainerIds.value.has(containerId)
}

function setContainerSelected(containerId: string, selected: boolean) {
  const next = new Set(selectedContainerIds.value)
  if (selected) next.add(containerId)
  else next.delete(containerId)
  selectedContainerIds.value = next
}

function handleContainerSelection(containerId: string, event: Event) {
  setContainerSelected(containerId, (event.target as HTMLInputElement).checked)
}

function isServiceSelected(containers: ServiceContainer[]): boolean {
  return containers.length > 0 && containers.every(container => selectedContainerIds.value.has(container.container_id))
}

function setServiceSelected(containers: ServiceContainer[], selected: boolean) {
  const next = new Set(selectedContainerIds.value)
  for (const container of containers) {
    if (selected) next.add(container.container_id)
    else next.delete(container.container_id)
  }
  selectedContainerIds.value = next
}

function handleServiceSelection(containers: ServiceContainer[], event: Event) {
  setServiceSelected(containers, (event.target as HTMLInputElement).checked)
}

function clearSelection() {
  selectedContainerIds.value = new Set()
}

// Track open state per container
const openContainers = ref<Record<string, boolean>>({})

function toggleContainer(id: string) {
  openContainers.value[id] = !openContainers.value[id]
}

const volumes = ref<VolumeInfo[]>([])
const networks = ref<NetworkInfo[]>([])
const openVolumeDetails = ref<Record<string, boolean>>({})
const openNetworkDetails = ref<Record<string, boolean>>({})

function toggleVolumeDetails(name: string) {
  openVolumeDetails.value[name] = !openVolumeDetails.value[name]
}

function toggleNetworkDetails(name: string) {
  openNetworkDetails.value[name] = !openNetworkDetails.value[name]
}

async function refreshResources() {
  try {
    const res = await getStackResources(props.stackId)
    volumes.value = res.volumes ?? []
    networks.value = res.networks ?? []
  } catch {
    volumes.value = []
    networks.value = []
  }
}

function refresh() {
  emit('refresh')
  refreshResources()
}

function emitContainersAction(containers: ServiceContainer[], action: BulkContainerAction, subject: string) {
  if (!props.canOperate || props.actionsDisabled) return
  const targets = actionTargets(containers, action)
  if (targets.length === 0) return
  emit('bulk-container-action', { containers: targets, action, subject })
}

function emitBulkAction(action: BulkContainerAction) {
  emitContainersAction(props.services || [], action, 'all containers')
}

function emitServiceAction(serviceName: string, containers: ServiceContainer[], action: BulkContainerAction) {
  emitContainersAction(containers, action, `all containers in ${serviceName}`)
}

function emitSelectedAction(action: BulkContainerAction) {
  emitContainersAction(selectedContainers.value, action, 'selected containers')
}

defineExpose({ refresh, clearSelection })

watch(() => props.stackId, refreshResources, { immediate: true })
watch(() => props.services, (services) => {
  const available = new Set((services || []).map(container => container.container_id))
  selectedContainerIds.value = new Set(
    [...selectedContainerIds.value].filter(containerId => available.has(containerId))
  )
}, { deep: true })
</script>

<template>
  <AppPanelCard>
    <template #header>
      <div class="flex justify-between items-center">
        <h3 class="font-semibold">Services & Resources</h3>
        <div class="flex items-center gap-2">
          <template v-if="canOperate && serviceTree.length">
            <UTooltip text="Restart All">
              <UButton
                icon="i-lucide-rotate-cw"
                variant="ghost"
                size="xs"
                color="info"
                title="Restart all containers in stack"
                :disabled="actionsDisabled"
                @click="emitBulkAction('restart')"
              />
            </UTooltip>
            <UTooltip text="Stop All">
              <UButton
                icon="i-lucide-square"
                variant="ghost"
                size="xs"
                color="warning"
                title="Stop all running containers in stack"
                :disabled="actionsDisabled"
                @click="emitBulkAction('stop')"
              />
            </UTooltip>
          </template>
          <UTooltip text="Refresh services and resources">
            <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click="refresh" />
          </UTooltip>
        </div>
      </div>
    </template>

    <div class="space-y-6">
      <!-- Containers section -->
      <section class="space-y-4">
        <div class="flex flex-col gap-0.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-box" class="w-4 h-4 text-yellow-400" />
            </div>
            <h4 class="text-base font-bold text-gray-900 dark:text-white">Containers</h4>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 pl-[42px]">Active services and runtime status</p>
        </div>

        <div
          v-if="canOperate && selectedCount"
          class="flex flex-col gap-3 rounded-lg border border-blue-200 bg-blue-50/70 px-3 py-2.5 dark:border-blue-900/70 dark:bg-blue-950/20 sm:flex-row sm:items-center sm:justify-between"
          data-testid="container-selection-toolbar"
        >
          <span class="text-sm font-medium text-blue-900 dark:text-blue-200">
            {{ selectedCount }} container{{ selectedCount === 1 ? '' : 's' }} selected
          </span>
          <div class="flex flex-wrap items-center gap-2">
            <UButton
              label="Restart selected"
              icon="i-lucide-rotate-cw"
              color="info"
              variant="soft"
              size="xs"
              data-testid="restart-selected"
              :disabled="actionsDisabled"
              @click="emitSelectedAction('restart')"
            />
            <UButton
              label="Stop selected"
              icon="i-lucide-square"
              color="warning"
              variant="soft"
              size="xs"
              data-testid="stop-selected"
              :disabled="actionsDisabled || selectedRunningCount === 0"
              @click="emitSelectedAction('stop')"
            />
            <UButton label="Clear" color="neutral" variant="ghost" size="xs" @click="clearSelection" />
          </div>
        </div>

        <div v-if="serviceTree.length" class="flex flex-col gap-3">
          <section
            v-for="svc in serviceTree"
            :key="svc.name"
            class="space-y-1.5"
            :data-service-name="svc.name"
          >
            <div class="flex flex-wrap items-center gap-2 rounded-lg bg-gray-100 px-3 py-2 dark:bg-gray-800/70">
              <label v-if="canOperate" class="flex shrink-0 cursor-pointer items-center" @click.stop>
                <input
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                  :aria-label="`Select all containers in ${svc.name}`"
                  :checked="isServiceSelected(svc.containers)"
                  :disabled="actionsDisabled"
                  @change="handleServiceSelection(svc.containers, $event)"
                >
              </label>
              <ContainerIcon
                :name="svc.name"
                :slug="getServiceSlug(svc.containers)"
                wrapper-class="w-6 h-6 flex shrink-0 items-center justify-center rounded bg-white dark:bg-gray-900 border border-gray-300 dark:border-gray-700 overflow-hidden"
                icon-class="w-4 h-4 object-contain"
              />
              <span class="min-w-0 flex-1 truncate text-sm font-semibold text-gray-900 dark:text-white">{{ svc.name }}</span>
              <UBadge :label="`${svc.containers.length} container${svc.containers.length === 1 ? '' : 's'}`" variant="subtle" size="xs" color="neutral" />
              <div v-if="canOperate" class="flex items-center gap-1">
                <UButton
                  label="Restart All"
                  icon="i-lucide-rotate-cw"
                  color="info"
                  variant="ghost"
                  size="xs"
                  :title="`Restart all containers in ${svc.name}`"
                  :data-testid="`restart-service-${svc.name}`"
                  :disabled="actionsDisabled"
                  @click="emitServiceAction(svc.name, svc.containers, 'restart')"
                />
                <UButton
                  label="Stop All"
                  icon="i-lucide-square"
                  color="warning"
                  variant="ghost"
                  size="xs"
                  :title="`Stop all running containers in ${svc.name}`"
                  :data-testid="`stop-service-${svc.name}`"
                  :disabled="actionsDisabled || !svc.containers.some(container => container.status === 'running')"
                  @click="emitServiceAction(svc.name, svc.containers, 'stop')"
                />
              </div>
            </div>

            <div class="ml-[14px] flex flex-col gap-1.5 border-l border-gray-300 pl-[22px] dark:border-gray-700">
              <div
                v-for="container in svc.containers"
                :key="container.container_id"
                class="rounded-lg border border-gray-300 dark:border-gray-700/60 overflow-hidden"
              >
              <!-- Accordion Header -->
              <div
                class="flex w-full flex-wrap items-center gap-2 px-3 py-2.5 transition-colors hover:bg-gray-50 dark:hover:bg-gray-800/60 text-left"
                :class="openContainers[container.container_id] ? 'bg-gray-50 dark:bg-gray-800/50' : 'bg-transparent'"
              >
                <label v-if="canOperate" class="flex shrink-0 cursor-pointer items-center">
                  <input
                    type="checkbox"
                    class="h-4 w-4 rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                    :aria-label="`Select ${container.container_name || container.service_name}`"
                    :data-testid="`select-container-${container.container_id}`"
                    :checked="isContainerSelected(container.container_id)"
                    :disabled="actionsDisabled"
                    @change="handleContainerSelection(container.container_id, $event)"
                  >
                </label>

                <!-- Clickable/toggle region: icon, name, badge, id, chevron only — no nested interactive controls -->
                <div
                  role="button"
                  tabindex="0"
                  class="flex flex-1 min-w-0 items-center gap-2"
                  :aria-expanded="!!openContainers[container.container_id]"
                  @click="toggleContainer(container.container_id)"
                  @keydown.enter.prevent="toggleContainer(container.container_id)"
                  @keydown.space.prevent="toggleContainer(container.container_id)"
                >
                  <!-- Container Icon -->
                  <ContainerIcon
                    :name="container.service_name"
                    :slug="getContainerSlug(container)"
                    wrapper-class="w-6 h-6 flex shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-300 dark:border-gray-700 overflow-hidden"
                    icon-class="w-4 h-4 object-contain"
                  />

                  <!-- Container Name -->
                  <span
                    class="font-medium text-sm text-gray-900 dark:text-white truncate flex-1 min-w-0"
                    :title="container.container_name || container.service_name"
                  >
                    {{ container.container_name || container.service_name }}
                  </span>

                  <!-- Status badge -->
                  <BadgeStatus :status="container.status" mobile-icon-only class="shrink-0" />

                  <!-- Container ID as code -->
                  <code class="hidden sm:inline-flex text-xs font-mono text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded shrink-0">
                    {{ container.container_id.slice(0, 12) }}
                  </code>

                  <!-- Chevron indicator -->
                  <UIcon
                    name="i-lucide-chevron-down"
                    class="w-4 h-4 shrink-0 text-gray-400 transition-transform duration-200"
                    :class="openContainers[container.container_id] ? 'rotate-180' : ''"
                  />
                </div>

                <!-- Copy container ID -->
                <div class="hidden sm:inline-flex shrink-0">
                  <UTooltip text="Copy container ID">
                    <UButton
                      icon="i-lucide-copy"
                      variant="ghost"
                      size="xs"
                      color="neutral"
                      title="Copy container ID"
                      @click="emit('copy-container-id', container.container_id)"
                    />
                  </UTooltip>
                </div>

                <!-- Action buttons -->
                <div class="flex items-center gap-0.5 shrink-0">
                  <ContainerIntegrationActions
                    :actions="integrationActions[container.container_id] || []"
                    :container-id="container.container_id"
                    :container-name="container.container_name || container.container_id"
                    @show-logs="(cid, cname) => emit('show-logs', cid, cname)"
                  />
                  <UTooltip text="Open terminal">
                    <UButton
                      v-if="canOperate && container.status === 'running'"
                      icon="i-lucide-terminal"
                      variant="ghost"
                      color="neutral"
                      size="xs"
                      title="Terminal"
                      :disabled="actionsDisabled"
                      @click="emit('open-terminal', { containerId: container.container_id, containerName: container.container_name || container.container_id })"
                    />
                  </UTooltip>
                  <UTooltip text="Stop container">
                    <UButton
                      v-if="canOperate && container.status === 'running'"
                      icon="i-lucide-square"
                      variant="ghost"
                      color="warning"
                      size="xs"
                      title="Stop"
                      :disabled="actionsDisabled"
                      @click="emit('container-action', { containerId: container.container_id, containerName: container.container_name || container.container_id, action: 'stop' })"
                    />
                  </UTooltip>
                  <UTooltip text="Restart container">
                    <UButton
                      v-if="canOperate"
                      icon="i-lucide-rotate-cw"
                      variant="ghost"
                      color="info"
                      size="xs"
                      title="Restart"
                      :disabled="actionsDisabled"
                      @click="emit('container-action', { containerId: container.container_id, containerName: container.container_name || container.container_id, action: 'restart' })"
                    />
                  </UTooltip>
                </div>
              </div>

              <!-- Accordion Body -->
              <div
                v-if="openContainers[container.container_id]"
                class="px-3 pb-3 pt-2.5 border-t border-gray-300 dark:border-gray-700/60 bg-gray-50/50 dark:bg-gray-800/20"
              >
                <div class="grid grid-cols-2 sm:grid-cols-4 gap-2">
                  <template v-if="containerStats[container.container_id]">
                    <!-- CPU stat -->
                    <div class="flex flex-col gap-1 bg-white dark:bg-gray-900/60 rounded-lg px-3 py-2 border border-gray-100 dark:border-gray-700/40">
                      <div class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                        <UIcon name="i-lucide-cpu" class="w-3.5 h-3.5" />
                        <span>CPU</span>
                      </div>
                      <span class="text-sm font-semibold text-gray-900 dark:text-white tabular-nums">
                        {{ containerStats[container.container_id].cpu_percent != null
                          ? containerStats[container.container_id].cpu_percent!.toFixed(2) + '%'
                          : '-' }}
                      </span>
                    </div>

                    <!-- Memory stat -->
                    <div class="flex flex-col gap-1 bg-white dark:bg-gray-900/60 rounded-lg px-3 py-2 border border-gray-100 dark:border-gray-700/40">
                      <div class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                        <UIcon name="i-lucide-memory-stick" class="w-3.5 h-3.5" />
                        <span>Memory</span>
                      </div>
                      <span class="text-sm font-semibold text-gray-900 dark:text-white tabular-nums">
                        {{ formatBytes(containerStats[container.container_id].mem_usage) }}
                        <span class="text-xs font-normal text-gray-400">/ {{ formatBytes(containerStats[container.container_id].mem_limit) }}</span>
                      </span>
                      <span
                        v-if="containerStats[container.container_id].mem_usage != null && containerStats[container.container_id].mem_limit"
                        class="text-xs text-gray-500 dark:text-gray-400"
                      >
                        {{ formatMemPercent(containerStats[container.container_id].mem_usage, containerStats[container.container_id].mem_limit) }}
                      </span>
                    </div>

                  </template>

                  <!-- No stats fallback -->
                  <p v-else class="col-span-2 sm:col-span-4 text-xs text-gray-400 italic py-1">
                    No runtime stats available for this container.
                  </p>

                  <!-- Published ports (sits alongside the other stat cards) -->
                  <div
                    v-if="container.ports && container.ports.length"
                    class="flex flex-col gap-1 bg-white dark:bg-gray-900/60 rounded-lg px-3 py-2 border border-gray-100 dark:border-gray-700/40"
                    :class="containerStats[container.container_id] ? '' : 'col-span-2 sm:col-span-4'"
                  >
                    <div class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                      <UIcon name="i-lucide-ethernet-port" class="w-3.5 h-3.5" />
                      <span>Ports</span>
                    </div>
                    <div class="flex flex-col gap-1.5">
                      <div
                        v-for="(port, idx) in container.ports"
                        :key="idx"
                        class="flex items-center gap-1.5"
                      >
                        <UBadge :label="formatHostPort(port)" variant="subtle" size="xs" color="neutral" class="font-mono" />
                        <UIcon name="i-lucide-arrow-right" class="w-3 h-3 text-gray-400 shrink-0" />
                        <UBadge :label="formatContainerPort(port)" variant="subtle" size="xs" color="neutral" class="font-mono" />
                      </div>
                    </div>
                  </div>

                  <!-- Uptime stat (always the last card) -->
                  <div
                    v-if="containerStats[container.container_id]"
                    class="flex flex-col gap-1 bg-white dark:bg-gray-900/60 rounded-lg px-3 py-2 border border-gray-100 dark:border-gray-700/40"
                    :class="container.ports && container.ports.length ? '' : 'col-span-2 sm:col-span-1'"
                  >
                    <div class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                      <UIcon name="i-lucide-clock" class="w-3.5 h-3.5" />
                      <span>Uptime</span>
                    </div>
                    <span class="text-sm font-semibold text-gray-900 dark:text-white tabular-nums">
                      {{ formatUptime(containerStats[container.container_id].started_at) }}
                    </span>
                  </div>
                </div>
              </div>
              </div>
            </div>
          </section>
        </div>
        <p v-else class="text-sm text-gray-500 py-2 text-center">No services found. Run a sync first.</p>
      </section>

      <hr class="border-gray-300 dark:border-carbon-800 my-4">

      <!-- Volumes section -->
      <section class="space-y-3">
        <div class="flex flex-col gap-0.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-hard-drive" class="w-4 h-4 text-yellow-400" />
            </div>
            <h4 class="text-base font-bold text-gray-900 dark:text-white">Volumes</h4>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 pl-[42px]">Persistent storage volumes for data</p>
        </div>

        <div v-if="volumes.length" class="space-y-3">
          <div
            v-for="vol in volumes"
            :key="vol.name"
            class="rounded-lg border border-gray-200 p-3 transition-colors dark:border-gray-700/60"
            :class="hasVolumeDetails(vol) ? 'cursor-pointer hover:border-gray-300 hover:bg-gray-50/50 dark:hover:border-gray-600 dark:hover:bg-gray-800/20' : ''"
            :role="hasVolumeDetails(vol) ? 'button' : undefined"
            :tabindex="hasVolumeDetails(vol) ? 0 : undefined"
            :aria-expanded="hasVolumeDetails(vol) ? !!openVolumeDetails[vol.name] : undefined"
            :data-testid="`volume-card-${vol.name}`"
            @click="hasVolumeDetails(vol) && toggleVolumeDetails(vol.name)"
            @keydown.enter.prevent="hasVolumeDetails(vol) && toggleVolumeDetails(vol.name)"
            @keydown.space.prevent="hasVolumeDetails(vol) && toggleVolumeDetails(vol.name)"
          >
            <div class="flex flex-wrap items-center gap-2">
              <div class="w-7 h-7 flex flex-shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-300 dark:border-gray-700 overflow-hidden">
                <UIcon name="i-lucide-database" class="w-4 h-4 text-gray-500 dark:text-gray-400" />
              </div>
              <span class="min-w-0 flex-1 truncate font-semibold text-sm">{{ vol.name }}</span>
              <UBadge :label="vol.driver" variant="subtle" size="xs" />
              <UBadge :label="vol.scope" variant="outline" size="xs" color="neutral" />
              <UIcon
                v-if="hasVolumeDetails(vol)"
                name="i-lucide-chevron-down"
                class="h-4 w-4 shrink-0 text-gray-400 transition-transform"
                :class="openVolumeDetails[vol.name] ? 'rotate-180' : ''"
              />
            </div>
            <div v-if="vol.mountpoint || vol.size_bytes != null" class="mt-2 border-t border-gray-100 pt-2 dark:border-gray-800">
              <div class="flex flex-wrap items-start justify-between gap-2">
                <div v-if="vol.mountpoint" class="min-w-0 flex-1">
                  <p class="text-xs text-gray-500 dark:text-gray-400">Mount point</p>
                  <code class="block truncate text-xs text-gray-500 dark:text-gray-400" :title="vol.mountpoint">{{ vol.mountpoint }}</code>
                </div>
                <div v-if="vol.size_bytes != null" class="shrink-0 text-right">
                  <p class="text-xs text-gray-500 dark:text-gray-400">Size</p>
                  <p class="text-xs font-medium tabular-nums text-gray-800 dark:text-wire-200">{{ formatBytes(vol.size_bytes) }}</p>
                </div>
              </div>
            </div>
            <div v-if="openVolumeDetails[vol.name]" class="mt-3 space-y-3 border-t border-gray-100 pt-3 dark:border-gray-800">
              <dl class="grid grid-cols-1 gap-3 text-xs sm:grid-cols-2">
                <div v-if="vol.docker_name && vol.docker_name !== vol.name">
                  <dt class="text-gray-500 dark:text-gray-400">Docker name</dt>
                  <dd><code class="break-all text-gray-800 dark:text-wire-200">{{ vol.docker_name }}</code></dd>
                </div>
                <div v-if="vol.created_at">
                  <dt class="text-gray-500 dark:text-gray-400">Created</dt>
                  <dd class="text-gray-800 dark:text-wire-200">{{ formatCreatedAt(vol.created_at) }}</dd>
                </div>
              </dl>
              <div v-if="recordEntries(vol.options).length">
                <p class="mb-1.5 text-xs font-medium text-gray-700 dark:text-gray-300">Driver options</p>
                <dl class="space-y-1 rounded bg-gray-50 p-2 text-xs dark:bg-gray-800/50">
                  <div v-for="[key, value] in recordEntries(vol.options)" :key="key" class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-2">
                    <dt class="truncate text-gray-500 dark:text-gray-400" :title="key">{{ key }}</dt>
                    <dd class="break-all font-mono text-gray-800 dark:text-wire-200">{{ value }}</dd>
                  </div>
                </dl>
              </div>
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-2 text-center">No volumes found. Run a sync first.</p>
      </section>

      <hr class="border-gray-300 dark:border-carbon-800 my-4">

      <!-- Networks section -->
      <section class="space-y-3">
        <div class="flex flex-col gap-0.5">
          <div class="flex items-center gap-2.5">
            <div class="flex items-center justify-center w-8 h-8 rounded-lg bg-yellow-400/10 shrink-0">
              <UIcon name="i-lucide-network" class="w-4 h-4 text-yellow-400" />
            </div>
            <h4 class="text-base font-bold text-gray-900 dark:text-white">Networks</h4>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400 pl-[42px]">Virtual networks connecting stack services</p>
        </div>

        <div v-if="networks.length" class="space-y-3">
          <div
            v-for="net in networks"
            :key="net.name"
            class="rounded-lg border border-gray-200 p-3 transition-colors dark:border-gray-700/60"
            :class="hasNetworkDetails(net) ? 'cursor-pointer hover:border-gray-300 hover:bg-gray-50/50 dark:hover:border-gray-600 dark:hover:bg-gray-800/20' : ''"
            :role="hasNetworkDetails(net) ? 'button' : undefined"
            :tabindex="hasNetworkDetails(net) ? 0 : undefined"
            :aria-expanded="hasNetworkDetails(net) ? !!openNetworkDetails[net.name] : undefined"
            :data-testid="`network-card-${net.name}`"
            @click="hasNetworkDetails(net) && toggleNetworkDetails(net.name)"
            @keydown.enter.prevent="hasNetworkDetails(net) && toggleNetworkDetails(net.name)"
            @keydown.space.prevent="hasNetworkDetails(net) && toggleNetworkDetails(net.name)"
          >
            <div class="flex flex-wrap items-center gap-2">
              <div class="w-7 h-7 flex flex-shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-300 dark:border-gray-700 overflow-hidden">
                <UIcon name="i-lucide-waypoints" class="w-4 h-4 text-gray-500 dark:text-gray-400" />
              </div>
              <span class="min-w-0 flex-1 truncate font-semibold text-sm">{{ net.name }}</span>
              <UBadge :label="net.driver" variant="subtle" size="xs" />
              <UBadge :label="net.scope" variant="outline" size="xs" color="neutral" />
              <UIcon
                v-if="hasNetworkDetails(net)"
                name="i-lucide-chevron-down"
                class="h-4 w-4 shrink-0 text-gray-400 transition-transform"
                :class="openNetworkDetails[net.name] ? 'rotate-180' : ''"
              />
            </div>
            <div v-if="net.subnet || net.gateway" class="mt-2 border-t border-gray-100 pt-2 dark:border-gray-800">
              <p class="text-xs font-mono text-gray-500 dark:text-gray-400">
                <span v-if="net.subnet">{{ net.subnet }}</span>
                <span v-if="net.subnet && net.gateway"> · </span>
                <span v-if="net.gateway">gw {{ net.gateway }}</span>
              </p>
            </div>
            <div v-if="openNetworkDetails[net.name]" class="mt-3 space-y-3 border-t border-gray-100 pt-3 dark:border-gray-800">
              <dl class="grid grid-cols-1 gap-3 text-xs sm:grid-cols-2">
                <div v-if="net.docker_name && net.docker_name !== net.name">
                  <dt class="text-gray-500 dark:text-gray-400">Docker name</dt>
                  <dd><code class="break-all text-gray-800 dark:text-wire-200">{{ net.docker_name }}</code></dd>
                </div>
                <div v-if="net.id">
                  <dt class="text-gray-500 dark:text-gray-400">Network ID</dt>
                  <dd><code class="break-all text-gray-800 dark:text-wire-200">{{ net.id }}</code></dd>
                </div>
                <div v-if="net.created_at">
                  <dt class="text-gray-500 dark:text-gray-400">Created</dt>
                  <dd class="text-gray-800 dark:text-wire-200">{{ formatCreatedAt(net.created_at) }}</dd>
                </div>
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">Addressing</dt>
                  <dd class="text-gray-800 dark:text-wire-200">IPv4 {{ net.enable_ipv4 ? 'enabled' : 'disabled' }} · IPv6 {{ net.enable_ipv6 ? 'enabled' : 'disabled' }}</dd>
                </div>
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">Network flags</dt>
                  <dd class="text-gray-800 dark:text-wire-200">{{ net.internal ? 'Internal' : 'External' }} · {{ net.attachable ? 'Attachable' : 'Not attachable' }}</dd>
                </div>
                <div v-if="net.ingress || net.config_only">
                  <dt class="text-gray-500 dark:text-gray-400">Special role</dt>
                  <dd class="text-gray-800 dark:text-wire-200">{{ net.ingress ? 'Ingress' : '' }}{{ net.ingress && net.config_only ? ' · ' : '' }}{{ net.config_only ? 'Config only' : '' }}</dd>
                </div>
              </dl>
              <div v-if="net.ipam_configs?.length">
                <p class="mb-1.5 text-xs font-medium text-gray-700 dark:text-gray-300">IPAM pools</p>
                <div class="space-y-2">
                  <div v-for="(config, index) in net.ipam_configs" :key="index" class="rounded bg-gray-50 p-2 text-xs dark:bg-gray-800/50">
                    <p class="font-mono text-gray-800 dark:text-wire-200">
                      {{ config.subnet || 'No subnet' }}<span v-if="config.gateway"> · gw {{ config.gateway }}</span><span v-if="config.ip_range"> · range {{ config.ip_range }}</span>
                    </p>
                    <dl v-if="recordEntries(config.aux_addresses).length" class="mt-2 space-y-1">
                      <div v-for="[key, value] in recordEntries(config.aux_addresses)" :key="key" class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-2">
                        <dt class="truncate text-gray-500 dark:text-gray-400" :title="key">{{ key }}</dt>
                        <dd class="break-all font-mono text-gray-800 dark:text-wire-200">{{ value }}</dd>
                      </div>
                    </dl>
                  </div>
                </div>
              </div>
              <div v-if="recordEntries(net.options).length">
                <p class="mb-1.5 text-xs font-medium text-gray-700 dark:text-gray-300">Driver options</p>
                <dl class="space-y-1 rounded bg-gray-50 p-2 text-xs dark:bg-gray-800/50">
                  <div v-for="[key, value] in recordEntries(net.options)" :key="key" class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-2">
                    <dt class="truncate text-gray-500 dark:text-gray-400" :title="key">{{ key }}</dt>
                    <dd class="break-all font-mono text-gray-800 dark:text-wire-200">{{ value }}</dd>
                  </div>
                </dl>
              </div>
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-2 text-center">No networks found. Run a sync first.</p>
      </section>
    </div>
  </AppPanelCard>
</template>
