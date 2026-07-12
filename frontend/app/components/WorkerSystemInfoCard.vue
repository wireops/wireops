<script setup lang="ts">
import { computed } from 'vue'
import { WORKER_STATUS, workerHasReportedInfo, usageColor, roundUsage } from '../utils/worker'
import WorkerEnvBadges from './WorkerEnvBadges.vue'

const props = defineProps<{
  worker: {
    status?: string
    version?: string
    os?: string
    arch?: string
    docker_version?: string
    compose_version?: string
    docker_online?: boolean
    cpu_usage?: number
    memory_usage?: number
    disk_usage?: number
  }
}>()

const hasInfo = computed(() => workerHasReportedInfo(props.worker))

const showDockerOfflineWarning = computed(() =>
  props.worker.status === WORKER_STATUS.ACTIVE && hasInfo.value && props.worker.docker_online === false
)
const showComposeMissingWarning = computed(() => hasInfo.value && !props.worker.compose_version)
const showVersionMissingWarning = computed(() => !props.worker.version)

const allTelemetryZero = computed(() =>
  hasInfo.value
  && !(props.worker.cpu_usage)
  && !(props.worker.memory_usage)
  && !(props.worker.disk_usage)
)

const usageRows = computed(() => [
  { label: 'CPU', value: roundUsage(props.worker.cpu_usage ?? 0) },
  { label: 'Memory', value: roundUsage(props.worker.memory_usage ?? 0) },
  { label: 'Disk', value: roundUsage(props.worker.disk_usage ?? 0) },
])
</script>

<template>
  <UCard>
    <template #header>
      <h3 class="font-semibold">System</h3>
    </template>

    <WorkerEnvBadges :worker="worker" size="sm" :show-version="false" class="mb-4" />

    <div class="space-y-3 mb-4">
      <UAlert
        v-if="showDockerOfflineWarning"
        color="error"
        variant="subtle"
        icon="i-lucide-container"
        title="Docker offline"
        description="The Docker daemon is offline on this worker."
      />
      <UAlert
        v-if="showComposeMissingWarning"
        color="warning"
        variant="subtle"
        icon="i-lucide-triangle-alert"
        title="Compose not found"
        description="Docker Compose was not detected — stack deployments will fail."
      />
      <UAlert
        v-if="showVersionMissingWarning"
        color="warning"
        variant="subtle"
        icon="i-lucide-package-x"
        title="Outdated agent"
        description="Worker did not report its version — likely an outdated agent. Consider upgrading."
      />
    </div>

    <div class="pt-4 border-t border-gray-200 dark:border-carbon-700">
      <span class="text-xs text-gray-500 dark:text-wire-200/50 uppercase tracking-wide font-semibold block mb-3">Resource Usage</span>

      <p v-if="!hasInfo" class="text-sm text-gray-400 dark:text-wire-200/40">No telemetry reported yet.</p>
      <div v-else class="space-y-3">
        <div v-for="row in usageRows" :key="row.label" class="flex items-center gap-3">
          <span class="w-16 shrink-0 text-xs text-gray-500 dark:text-wire-200/50">{{ row.label }}</span>
          <UProgress :model-value="row.value" size="sm" :color="usageColor(row.value)" class="flex-1" />
          <span class="w-10 shrink-0 text-right text-xs font-medium text-gray-900 dark:text-wire-200">{{ row.value }}%</span>
        </div>
        <p v-if="allTelemetryZero" class="text-xs text-gray-400 dark:text-wire-200/40">
          Telemetry may report 0% on non-Linux hosts.
        </p>
      </div>
    </div>
  </UCard>
</template>
