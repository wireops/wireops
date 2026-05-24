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

defineExpose({ refresh })

watch(() => props.stackId, refreshResources, { immediate: true })
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex justify-between items-center">
        <h3 class="font-semibold">Services & Resources</h3>
        <UTooltip text="Refresh services and resources">
          <UButton icon="i-lucide-refresh-cw" variant="ghost" size="xs" @click="refresh" />
        </UTooltip>
      </div>
    </template>

    <div class="space-y-6">
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

        <div v-if="serviceTree.length" class="space-y-4">
          <div v-for="svc in serviceTree" :key="svc.name">
            <div class="flex items-center gap-2 py-1">
              <ContainerIcon
                :name="svc.name"
                :slug="svc.containers[0] ? getContainerSlug(svc.containers[0]) : undefined"
                wrapper-class="w-7 h-7 flex flex-shrink-0 items-center justify-center rounded bg-gray-100 dark:bg-gray-800 border border-gray-200 dark:border-gray-700 overflow-hidden"
                icon-class="w-4 h-4 object-contain"
              />
              <span class="font-semibold text-sm">{{ svc.name }}</span>
            </div>
            <div class="ml-[14px] border-l border-gray-200 dark:border-gray-700 pl-[22px] space-y-2">
              <div
                v-for="container in svc.containers"
                :key="container.container_id"
                class="py-2 px-2 rounded-md transition-colors hover:bg-gray-100 dark:hover:bg-gray-800 group"
              >
                <div class="flex items-center justify-between">
                  <div class="flex items-center gap-2 min-w-0">
                    <BadgeStatus :status="container.status" />
                    <span class="text-sm font-mono truncate">{{ container.container_id.slice(0, 12) }}</span>
                    <button
                      class="text-xs text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800 p-1 rounded transition-colors cursor-pointer shrink-0"
                      :title="`Copy ${container.container_id}`"
                      @click="emit('copy-container-id', container.container_id)"
                    >
                      <UIcon name="i-lucide-copy" class="w-3.5 h-3.5" />
                    </button>
                  </div>
                  <div class="flex items-center gap-1 shrink-0">
                    <UButton
                      v-if="container.status === 'running'"
                      icon="i-lucide-square"
                      variant="ghost"
                      color="warning"
                      size="xs"
                      title="Stop"
                      @click="emit('container-action', { containerId: container.container_id, containerName: container.container_name || container.container_id, action: 'stop' })"
                    />
                    <UButton
                      icon="i-lucide-rotate-cw"
                      variant="ghost"
                      color="info"
                      size="xs"
                      title="Restart"
                      @click="emit('container-action', { containerId: container.container_id, containerName: container.container_name || container.container_id, action: 'restart' })"
                    />
                  </div>
                </div>

                <div v-if="containerStats[container.container_id]" class="flex flex-wrap items-center gap-x-4 gap-y-1 mt-1 text-xs text-gray-400">
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-cpu" class="w-3 h-3" />
                    {{ containerStats[container.container_id].cpu_percent != null ? containerStats[container.container_id].cpu_percent.toFixed(2) : '-' }}%
                  </span>
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-memory-stick" class="w-3 h-3" />
                    {{ formatBytes(containerStats[container.container_id].mem_usage) }} / {{ formatBytes(containerStats[container.container_id].mem_limit) }}
                    <span v-if="containerStats[container.container_id].mem_usage != null && containerStats[container.container_id].mem_limit" class="text-xs text-gray-500 dark:text-gray-400 font-medium">
                      ({{ formatMemPercent(containerStats[container.container_id].mem_usage, containerStats[container.container_id].mem_limit) }})
                    </span>
                  </span>
                  <span class="flex items-center gap-1">
                    <UIcon name="i-lucide-clock" class="w-3 h-3" />
                    {{ formatUptime(containerStats[container.container_id].started_at) }}
                  </span>
                  <ContainerIntegrationActions
                    :actions="integrationActions[container.container_id] || []"
                    :container-id="container.container_id"
                    :container-name="container.container_name || container.container_id"
                    @show-logs="(containerId, containerName) => emit('show-logs', containerId, containerName)"
                  />
                </div>
              </div>
            </div>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500 py-2 text-center">No services found. Run a sync first.</p>
      </section>

      <hr class="border-gray-200 dark:border-carbon-800 my-4" />

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

      <hr class="border-gray-200 dark:border-carbon-800 my-4" />

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
