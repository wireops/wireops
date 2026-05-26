<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import ContainerIcon from './ContainerIcon.vue'
import type { IntegrationAction } from '~/composables/useIntegrations'

interface ServiceContainer {
  service_name: string
  container_id: string
  container_name?: string
  status: string
}

interface ContainerStats {
  cpu_percent?: number
  mem_usage?: number
  mem_limit?: number
  started_at?: string
}

type VolumeInfo = { name: string; driver: string; mountpoint: string; scope: string }
type NetworkInfo = { name: string; driver: string; scope: string; subnet?: string; gateway?: string }

interface ContainerInfo {
  name: string
  is_fallback: boolean
  slug?: string
}

const props = defineProps<{
  stackId: string
  services: ServiceContainer[]
  containerStats: Record<string, ContainerStats>
  integrationActions: Record<string, IntegrationAction[]>
  containersList?: ContainerInfo[]
}>()

const emit = defineEmits<{
  (e: 'refresh'): void
  (e: 'copy-container-id', containerId: string): void
  (e: 'show-logs', containerId: string, containerName: string): void
  (e: 'container-action', payload: { containerId: string, containerName: string, action: 'stop' | 'restart' }): void
  (e: 'bulk-container-action', payload: { containers: { containerId: string, containerName: string }[], action: 'stop' | 'restart' }): void
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

const getContainerSlug = (container: ServiceContainer) => {
  if (!props.containersList) return undefined
  const match = props.containersList.find(
    c => c.name === container.service_name || c.name === container.container_name
  )
  return match?.slug
}

const serviceTree = computed(() => {
  const map: Record<string, ServiceContainer[]> = {}
  for (const service of props.services || []) {
    if (!map[service.service_name]) map[service.service_name] = []
    map[service.service_name]?.push(service)
  }
  return Object.entries(map).map(([name, containers]) => ({ name, containers }))
})

// Track open state per container
const openContainers = ref<Record<string, boolean>>({})

function toggleContainer(id: string) {
  openContainers.value[id] = !openContainers.value[id]
}

const volumes = ref<VolumeInfo[]>([])
const networks = ref<NetworkInfo[]>([])

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

function emitBulkAction(action: 'stop' | 'restart') {
  const targets = (props.services || [])
    .filter(svc => action !== 'stop' || svc.status === 'running')
    .map(svc => ({ containerId: svc.container_id, containerName: svc.container_name || svc.container_id }))
  if (targets.length === 0) return
  emit('bulk-container-action', { containers: targets, action })
}

defineExpose({ refresh })

watch(() => props.stackId, refreshResources, { immediate: true })
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex justify-between items-center">
        <h3 class="font-semibold">Services & Resources</h3>
        <div class="flex items-center gap-2">
          <template v-if="serviceTree.length">
            <UTooltip text="Restart All">
              <UButton icon="i-lucide-rotate-cw" variant="ghost" size="xs" color="info" @click="emitBulkAction('restart')" />
            </UTooltip>
            <UTooltip text="Stop All">
              <UButton icon="i-lucide-square" variant="ghost" size="xs" color="warning" @click="emitBulkAction('stop')" />
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

        <div v-if="serviceTree.length" class="flex flex-col gap-1.5">
          <template v-for="svc in serviceTree" :key="svc.name">
            <div
              v-for="container in svc.containers"
              :key="container.container_id"
              class="rounded-lg border border-gray-200 dark:border-gray-700/60 overflow-hidden"
            >
              <!-- Accordion Header -->
              <button
                type="button"
                class="flex w-full items-center gap-2 px-3 py-2.5 transition-colors hover:bg-gray-50 dark:hover:bg-gray-800/60 text-left"
                :class="openContainers[container.container_id] ? 'bg-gray-50 dark:bg-gray-800/50' : 'bg-transparent'"
                :aria-expanded="openContainers[container.container_id]"
                @click="toggleContainer(container.container_id)"
              >
                <!-- Container Icon -->
                <ContainerIcon
                  :name="container.service_name"
                  :slug="getContainerSlug(container)"
                  wrapper-class="w-6 h-6 flex shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden"
                  icon-class="w-4 h-4 object-contain"
                />

                <!-- Container Name -->
                <span class="font-medium text-sm text-gray-900 dark:text-white truncate flex-1 min-w-0">
                  {{ container.container_name || container.service_name }}
                </span>

                <!-- Status badge -->
                <BadgeStatus :status="container.status" class="shrink-0" />

                <!-- Container ID as code -->
                <code class="hidden sm:inline-flex text-xs font-mono text-gray-400 dark:text-gray-500 bg-gray-100 dark:bg-gray-800 px-1.5 py-0.5 rounded shrink-0">
                  {{ container.container_id.slice(0, 12) }}
                </code>

                <!-- Action buttons — @click.stop prevents accordion toggle -->
                <div class="flex items-center gap-0.5 shrink-0 ml-1" @click.stop>
                  <ContainerIntegrationActions
                    :actions="integrationActions[container.container_id] || []"
                    :container-id="container.container_id"
                    :container-name="container.container_name || container.container_id"
                    @show-logs="(cid, cname) => emit('show-logs', cid, cname)"
                  />
                  <UTooltip text="Stop container">
                    <UButton
                      v-if="container.status === 'running'"
                      icon="i-lucide-square"
                      variant="ghost"
                      color="warning"
                      size="xs"
                      @click="emit('container-action', { containerId: container.container_id, containerName: container.container_name || container.container_id, action: 'stop' })"
                    />
                  </UTooltip>
                  <UTooltip text="Restart container">
                    <UButton
                      icon="i-lucide-rotate-cw"
                      variant="ghost"
                      color="info"
                      size="xs"
                      @click="emit('container-action', { containerId: container.container_id, containerName: container.container_name || container.container_id, action: 'restart' })"
                    />
                  </UTooltip>
                </div>

                <!-- Chevron indicator -->
                <UIcon
                  name="i-lucide-chevron-down"
                  class="w-4 h-4 shrink-0 text-gray-400 transition-transform duration-200"
                  :class="openContainers[container.container_id] ? 'rotate-180' : ''"
                />
              </button>

              <!-- Accordion Body -->
              <div
                v-if="openContainers[container.container_id]"
                class="px-3 pb-3 pt-2.5 border-t border-gray-200 dark:border-gray-700/60 bg-gray-50/50 dark:bg-gray-800/20"
              >
                <div
                  v-if="containerStats[container.container_id]"
                  class="grid grid-cols-2 sm:grid-cols-3 gap-2"
                >
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

                  <!-- Uptime stat -->
                  <div class="flex flex-col gap-1 bg-white dark:bg-gray-900/60 rounded-lg px-3 py-2 border border-gray-100 dark:border-gray-700/40 col-span-2 sm:col-span-1">
                    <div class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                      <UIcon name="i-lucide-clock" class="w-3.5 h-3.5" />
                      <span>Uptime</span>
                    </div>
                    <span class="text-sm font-semibold text-gray-900 dark:text-white tabular-nums">
                      {{ formatUptime(containerStats[container.container_id].started_at) }}
                    </span>
                  </div>
                </div>

                <!-- No stats fallback -->
                <p v-else class="text-xs text-gray-400 italic py-1">
                  No runtime stats available for this container.
                </p>
              </div>
            </div>
          </template>
        </div>
        <p v-else class="text-sm text-gray-500 py-2 text-center">No services found. Run a sync first.</p>
      </section>

      <hr class="border-gray-200 dark:border-carbon-800 my-4">

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

        <div v-if="volumes.length" class="space-y-4">
          <div v-for="vol in volumes" :key="vol.name">
            <div class="flex items-center gap-2 py-1">
              <div class="w-7 h-7 flex flex-shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden">
                <UIcon name="i-lucide-database" class="w-4 h-4 text-gray-500 dark:text-gray-400" />
              </div>
              <span class="font-semibold text-sm">{{ vol.name }}</span>
              <UBadge :label="vol.driver" variant="subtle" size="xs" />
              <UBadge :label="vol.scope" variant="outline" size="xs" color="neutral" />
            </div>
            <div v-if="vol.mountpoint" class="ml-[14px] border-l border-gray-200 dark:border-gray-700 pl-[22px] py-1">
              <p class="text-xs text-gray-400 font-mono truncate" :title="vol.mountpoint">
                {{ vol.mountpoint }}
              </p>
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-2 text-center">No volumes found. Run a sync first.</p>
      </section>

      <hr class="border-gray-200 dark:border-carbon-800 my-4">

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

        <div v-if="networks.length" class="space-y-4">
          <div v-for="net in networks" :key="net.name">
            <div class="flex items-center gap-2 py-1">
              <div class="w-7 h-7 flex flex-shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden">
                <UIcon name="i-lucide-waypoints" class="w-4 h-4 text-gray-500 dark:text-gray-400" />
              </div>
              <span class="font-semibold text-sm">{{ net.name }}</span>
              <UBadge :label="net.driver" variant="subtle" size="xs" />
              <UBadge :label="net.scope" variant="outline" size="xs" color="neutral" />
            </div>
            <div v-if="net.subnet || net.gateway" class="ml-[14px] border-l border-gray-200 dark:border-gray-700 pl-[22px] py-1">
              <p class="text-xs text-gray-400 font-mono">
                <span v-if="net.subnet">{{ net.subnet }}</span>
                <span v-if="net.subnet && net.gateway"> · </span>
                <span v-if="net.gateway">gw {{ net.gateway }}</span>
              </p>
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-2 text-center">No networks found. Run a sync first.</p>
      </section>
    </div>
  </UCard>
</template>
